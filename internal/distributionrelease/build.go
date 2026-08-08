package distributionrelease

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/spice-framework/development/internal/catalog"
)

const maximumBuildOutput = 32 << 10

type modMetadata struct {
	Module struct {
		Path string
	}
	Go        string
	Toolchain string
	Require   []struct {
		Path    string
		Version string
	}
	Replace []json.RawMessage
}

func requireModuleGraph(
	ctx context.Context,
	root string,
	repository catalog.Repository,
	goExecutable string,
	scratchRoot string,
) ([]module, error) {
	output, stderr, err := runGoCommand(
		ctx,
		root,
		goExecutable,
		scratchRoot,
		catalogHostTarget(),
		maximumGoToolOutput,
		"mod",
		"edit",
		"-json",
	)
	if err != nil {
		return nil, fmt.Errorf("inspect distribution module: %w", err)
	}
	if stderr != "" {
		return nil, fmt.Errorf("inspect distribution module wrote unexpected stderr %q", stderr)
	}
	var metadata modMetadata
	if err := json.Unmarshal([]byte(output), &metadata); err != nil {
		return nil, fmt.Errorf("parse distribution module: %w", err)
	}
	if metadata.Module.Path != repository.Module {
		return nil, fmt.Errorf("distribution module %q does not match catalog module %q", metadata.Module.Path, repository.Module)
	}
	if metadata.Go != "1.26.0" || metadata.Toolchain != "go1.26.5" {
		return nil, fmt.Errorf("distribution requires go 1.26.0 and toolchain go1.26.5, got go %s and toolchain %s", metadata.Go, metadata.Toolchain)
	}
	if len(metadata.Replace) != 0 {
		return nil, errors.New("distribution module must not contain replace directives")
	}
	required := make(map[string]string, len(metadata.Require))
	for _, item := range metadata.Require {
		if _, duplicate := required[item.Path]; duplicate {
			return nil, fmt.Errorf("distribution module requires %s more than once", item.Path)
		}
		required[item.Path] = item.Version
	}
	for _, selection := range repository.Release.RequiredModules {
		version, found := required[selection.Path]
		if !found || version != selection.Version {
			return nil, fmt.Errorf(
				"distribution module must require %s at exact catalog version %s; got %q",
				selection.Path,
				selection.Version,
				version,
			)
		}
	}
	_, stderr, err = runGoCommand(
		ctx,
		root,
		goExecutable,
		scratchRoot,
		catalogHostTarget(),
		maximumGoToolOutput,
		"list",
		"-mod=vendor",
		"./...",
	)
	if err != nil {
		return nil, fmt.Errorf("validate distribution vendor graph: %w", err)
	}
	if stderr != "" {
		return nil, fmt.Errorf("validate distribution vendor graph wrote unexpected stderr %q", stderr)
	}
	content, err := readBounded(filepath.Join(root, "vendor", "modules.txt"), 16<<20)
	if err != nil {
		return nil, fmt.Errorf("read distribution vendor graph: %w", err)
	}
	modules, err := parseVendorModules(content)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]string, len(modules))
	for _, item := range modules {
		selected[item.Path] = item.Version
	}
	for path, version := range required {
		if selected[path] != version {
			return nil, fmt.Errorf("vendored module %s is %q; go.mod requires %q", path, selected[path], version)
		}
	}
	return modules, nil
}

func parseVendorModules(content []byte) ([]module, error) {
	var modules []module
	seen := make(map[string]struct{})
	for line := range strings.SplitSeq(string(content), "\n") {
		if !strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "# "))
		if len(fields) != 2 || !catalog.ValidModuleVersion(fields[1]) {
			return nil, fmt.Errorf("distribution vendor header %q is malformed or uses a replacement", line)
		}
		if _, duplicate := seen[fields[0]]; duplicate {
			return nil, fmt.Errorf("distribution vendor graph repeats %s", fields[0])
		}
		seen[fields[0]] = struct{}{}
		modules = append(modules, module{Path: fields[0], Version: fields[1]})
	}
	slices.SortFunc(modules, func(left, right module) int { return strings.Compare(left.Path, right.Path) })
	return modules, nil
}

func buildTarget(
	ctx context.Context,
	prepared preparedRelease,
	target catalog.ReleaseTarget,
) (map[string][]byte, error) {
	directory, err := os.MkdirTemp(prepared.scratchRoot, "build-"+target.GOOS+"-"+target.GOARCH+"-*")
	if err != nil {
		return nil, fmt.Errorf("create distribution build directory: %w", err)
	}
	defer os.RemoveAll(directory)
	result := make(map[string][]byte, len(prepared.repository.Release.Binaries))
	for _, binary := range prepared.repository.Release.Binaries {
		archiveName := binary.Name
		if target.GOOS == "windows" {
			archiveName += ".exe"
		}
		output := filepath.Join(directory, archiveName)
		arguments := []string{
			"build", "-mod=vendor", "-trimpath", "-buildvcs=false",
			"-ldflags=" + distributionLinkerFlags(prepared), "-o", output, binary.Package,
		}
		if err := runGoBuild(ctx, prepared, target, arguments); err != nil {
			return nil, fmt.Errorf("build %s for %s/%s: %w", binary.Package, target.GOOS, target.GOARCH, err)
		}
		if err := verifyExecutableIdentity(ctx, prepared, target, binary, output); err != nil {
			return nil, err
		}
		content, err := readBounded(output, maximumArtifactBytes)
		if err != nil {
			return nil, fmt.Errorf("read built binary %q: %w", binary.Name, err)
		}
		if len(content) == 0 {
			return nil, fmt.Errorf("built binary %q is empty", binary.Name)
		}
		version := strings.TrimPrefix(prepared.repository.Release.Version, "v")
		if !bytes.Contains(content, []byte(version)) || !bytes.Contains(content, []byte(prepared.commit)) {
			return nil, fmt.Errorf(
				"built binary %q does not contain catalog-authorized version and commit identity",
				binary.Name,
			)
		}
		result[archiveName] = content
	}
	return result, nil
}

func distributionLinkerFlags(prepared preparedRelease) string {
	identity := prepared.repository.Release.BuildIdentity
	version := strings.TrimPrefix(prepared.repository.Release.Version, "v")
	return "-buildid= -X=" + identity.VersionSymbol + "=" + version +
		" -X=" + identity.CommitSymbol + "=" + prepared.commit
}

func runGoBuild(
	ctx context.Context,
	prepared preparedRelease,
	target catalog.ReleaseTarget,
	arguments []string,
) error {
	stdout, stderr, err := runGoCommand(
		ctx,
		prepared.sourceRoot,
		prepared.goExecutable,
		prepared.scratchRoot,
		target,
		maximumBuildOutput,
		arguments...,
	)
	if err != nil {
		return err
	}
	if stdout != "" || stderr != "" {
		return fmt.Errorf("go build wrote unexpected stdout %q or stderr %q", stdout, stderr)
	}
	return nil
}

type boundedBuffer struct {
	content   bytes.Buffer
	maximum   int
	truncated bool
	mutex     sync.Mutex
}

func (buffer *boundedBuffer) Write(content []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	written := len(content)
	remaining := buffer.maximum - buffer.content.Len()
	if remaining <= 0 {
		buffer.truncated = true
		return written, nil
	}
	if len(content) > remaining {
		content = content[:remaining]
		buffer.truncated = true
	}
	_, _ = buffer.content.Write(content)
	return written, nil
}

func (buffer *boundedBuffer) Bytes() []byte {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return bytes.Clone(buffer.content.Bytes())
}

func (buffer *boundedBuffer) String() string {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.content.String()
}
