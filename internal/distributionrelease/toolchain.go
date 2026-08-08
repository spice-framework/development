package distributionrelease

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/spice-framework/development/internal/catalog"
)

const maximumGoToolOutput = 32 << 20

var exactGoVersionPattern = regexp.MustCompile(
	`\Ago version (go[0-9]+\.[0-9]+\.[0-9]+) ([a-z0-9]+)/([a-z0-9]+)\r?\n?\z`,
)

func resolveGoExecutable() (string, error) {
	located, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("locate distribution Go executable: %w", err)
	}
	absolute, err := filepath.Abs(located)
	if err != nil {
		return "", fmt.Errorf("resolve distribution Go executable: %w", err)
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve real distribution Go executable: %w", err)
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return "", fmt.Errorf("resolve absolute distribution Go executable: %w", err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("inspect distribution Go executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("distribution Go executable %q is not a regular file", real)
	}
	return filepath.Clean(real), nil
}

func requireGoRuntime(
	ctx context.Context,
	root string,
	executable string,
	scratchRoot string,
) error {
	stdout, stderr, err := runGoCommand(
		ctx,
		root,
		executable,
		scratchRoot,
		catalogHostTarget(),
		maximumBuildOutput,
		"version",
	)
	if err != nil {
		return fmt.Errorf("inspect distribution Go runtime: %w", err)
	}
	if stderr != "" {
		return fmt.Errorf("distribution Go version wrote unexpected stderr %q", stderr)
	}
	return validateGoVersionOutput(stdout)
}

func validateGoVersionOutput(output string) error {
	matches := exactGoVersionPattern.FindStringSubmatch(output)
	if matches == nil || matches[1] != "go1.26.5" || matches[2] != runtime.GOOS || matches[3] != runtime.GOARCH {
		return fmt.Errorf("distribution requires exact Go 1.26.5 host runtime, got %q", output)
	}
	return nil
}

func runGoCommand(
	ctx context.Context,
	root string,
	executable string,
	scratchRoot string,
	target catalog.ReleaseTarget,
	maximum int,
	arguments ...string,
) (string, string, error) {
	// #nosec G204 -- the executable is resolved once to an absolute regular
	// file, arguments are fixed or catalog validated, and no shell is used.
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = root
	command.Env = distributionBuildEnvironment(os.Environ(), scratchRoot, target)
	var stdout boundedBuffer
	stdout.maximum = maximum
	var stderr boundedBuffer
	stderr.maximum = maximum
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if stdout.truncated || stderr.truncated {
		return stdout.String(), stderr.String(), fmt.Errorf("Go command output exceeds %d bytes", maximum)
	}
	if err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf(
			"go %s: %w: %s",
			strings.Join(arguments, " "),
			err,
			strings.TrimSpace(stderr.String()),
		)
	}
	return stdout.String(), stderr.String(), nil
}

func distributionBuildEnvironment(
	environment []string,
	scratchRoot string,
	target catalog.ReleaseTarget,
) []string {
	ambient := make(map[string]string)
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if found {
			ambient[strings.ToUpper(name)] = value
		}
	}
	result := make([]string, 0, 28)
	for _, name := range []string{"SYSTEMROOT", "WINDIR", "COMSPEC"} {
		if value := ambient[name]; value != "" {
			result = append(result, name+"="+value)
		}
	}
	result = append(
		result,
		"CGO_ENABLED=0",
		"GO111MODULE=on",
		"GOARCH="+target.GOARCH,
		"GOCACHE="+filepath.Join(scratchRoot, "gocache"),
		"GOENV=off",
		"GOEXPERIMENT=",
		"GOFLAGS=",
		"GOMODCACHE="+filepath.Join(scratchRoot, "gomodcache"),
		"GONOPROXY=",
		"GONOSUMDB=",
		"GOOS="+target.GOOS,
		"GOPATH="+filepath.Join(scratchRoot, "gopath"),
		"GOPRIVATE=",
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOTELEMETRY=off",
		"GOTOOLCHAIN=local",
		"GOTMPDIR="+filepath.Join(scratchRoot, "gotmp"),
		"GOVCS=off",
		"GOWORK=off",
	)
	switch target.GOARCH {
	case "amd64":
		result = append(result, "GOAMD64=v1")
	case "arm64":
		result = append(result, "GOARM64=v8.0")
	}
	return result
}

func catalogHostTarget() catalog.ReleaseTarget {
	return catalog.ReleaseTarget{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
}

func verifyExecutableIdentity(
	ctx context.Context,
	prepared preparedRelease,
	target catalog.ReleaseTarget,
	binary catalog.ReleaseBinary,
	output string,
) error {
	identity := prepared.repository.Release.BuildIdentity
	stdout, stderr, err := runGoCommand(
		ctx,
		prepared.sourceRoot,
		prepared.goExecutable,
		prepared.scratchRoot,
		target,
		maximumGoToolOutput,
		"tool",
		"nm",
		output,
	)
	if err != nil {
		return fmt.Errorf("inspect %s build identity symbols: %w", binary.Name, err)
	}
	if stderr != "" {
		return fmt.Errorf("inspect %s build identity symbols wrote stderr %q", binary.Name, stderr)
	}
	wanted := map[string]int{identity.VersionSymbol: 0, identity.CommitSymbol: 0}
	for line := range strings.SplitSeq(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			if _, found := wanted[fields[len(fields)-1]]; found {
				if fields[len(fields)-2] != "D" {
					return fmt.Errorf(
						"built binary %q identity symbol %s has unexpected nm type %s",
						binary.Name,
						fields[len(fields)-1],
						fields[len(fields)-2],
					)
				}
				wanted[fields[len(fields)-1]]++
			}
		}
	}
	for _, symbol := range []string{identity.VersionSymbol, identity.CommitSymbol} {
		if wanted[symbol] != 1 {
			return fmt.Errorf(
				"built binary %q has %d exact build identity symbols named %s; require one",
				binary.Name,
				wanted[symbol],
				symbol,
			)
		}
	}
	if target != catalogHostTarget() {
		return nil
	}
	// #nosec G204 -- output is a renderer-created absolute path and the fixed
	// argument invokes the repository's required identity-only command path.
	command := exec.CommandContext(ctx, output, "--version")
	command.Dir = prepared.sourceRoot
	command.Env = distributionBuildEnvironment(os.Environ(), prepared.scratchRoot, target)
	var versionOutput boundedBuffer
	versionOutput.maximum = maximumBuildOutput
	var versionError boundedBuffer
	versionError.maximum = maximumBuildOutput
	command.Stdout = &versionOutput
	command.Stderr = &versionError
	if err := command.Run(); err != nil {
		return fmt.Errorf("execute %s --version: %w: %s", binary.Name, err, strings.TrimSpace(versionError.String()))
	}
	if versionOutput.truncated || versionError.truncated {
		return fmt.Errorf("execute %s --version output exceeds %d bytes", binary.Name, maximumBuildOutput)
	}
	version := strings.TrimPrefix(prepared.repository.Release.Version, "v")
	want := binary.Name + " " + version + " (" + prepared.commit + ")\n"
	if versionOutput.String() != want || versionError.String() != "" {
		return fmt.Errorf(
			"execute %s --version returned stdout %q and stderr %q; require %q",
			binary.Name,
			versionOutput.String(),
			versionError.String(),
			want,
		)
	}
	return nil
}
