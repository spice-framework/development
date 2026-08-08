package gorelease

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/spice-framework/development/internal/process"
)

const (
	maximumGitTreeBytes = 16 << 20
	maximumArchiveBytes = 256 << 20
	maximumGitError     = 32 << 10
)

func requirePortableTree(ctx context.Context, root string, commit string) error {
	content, err := gitBinary(ctx, root, maximumGitTreeBytes, "ls-tree", "-rz", "--full-tree", commit)
	if err != nil {
		return fmt.Errorf("list Go release tree: %w", err)
	}
	return validateTree(content)
}

func validateTree(content []byte) error {
	if len(content) == 0 {
		return errors.New("Go release commit has no tracked files")
	}
	seen := make(map[string]string)
	for _, item := range bytes.Split(bytes.TrimSuffix(content, []byte{0}), []byte{0}) {
		metadata, nameBytes, found := bytes.Cut(item, []byte{'\t'})
		fields := strings.Fields(string(metadata))
		name := string(nameBytes)
		if !found || len(fields) != 3 || fields[1] != "blob" ||
			(fields[0] != "100644" && fields[0] != "100755") || !commitPattern.MatchString(fields[2]) {
			return fmt.Errorf("Go release source has unsupported tree entry %q", item)
		}
		if err := validatePortablePath(name); err != nil {
			return fmt.Errorf("Go release source path %q: %w", name, err)
		}
		key := strings.ToLower(name)
		if prior, found := seen[key]; found {
			return fmt.Errorf("Go release source paths %q and %q collide on case-insensitive filesystems", prior, name)
		}
		seen[key] = name
	}
	return nil
}

func buildSourceArchive(ctx context.Context, prepared source, version string) ([]byte, error) {
	prefix := prepared.repository.Name + "_" + strings.TrimPrefix(version, "v") + "/"
	tarContent, err := gitBinary(
		ctx,
		prepared.root,
		maximumArchiveBytes,
		"archive",
		"--format=tar",
		"--prefix="+prefix,
		prepared.commit,
	)
	if err != nil {
		return nil, fmt.Errorf("archive committed Go release source: %w", err)
	}
	var output bytes.Buffer
	writer, err := gzip.NewWriterLevel(&output, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("create Go release gzip writer: %w", err)
	}
	writer.Header.ModTime = unixTime(prepared.epoch)
	writer.Header.OS = 255
	if _, err := writer.Write(tarContent); err != nil {
		return nil, errors.Join(fmt.Errorf("compress Go release source: %w", err), writer.Close())
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close Go release gzip writer: %w", err)
	}
	return output.Bytes(), nil
}

func validatePortablePath(name string) error {
	clean := path.Clean(name)
	if name == "" || clean == "." || clean == ".." || clean != name || path.IsAbs(name) ||
		strings.HasPrefix(name, "../") || strings.Contains(name, "\\") {
		return errors.New("path is empty, absolute, or contains traversal")
	}
	if !utf8.ValidString(name) {
		return errors.New("path is not valid UTF-8")
	}
	for index := range len(name) {
		character := name[index]
		if character < 0x20 || character > 0x7e || strings.ContainsRune(`<>:"|?*`, rune(character)) {
			return errors.New("path is not portable printable ASCII")
		}
	}
	for component := range strings.SplitSeq(name, "/") {
		if component == "" || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return errors.New("path has an empty or non-portable component")
		}
		base, _, _ := strings.Cut(component, ".")
		switch strings.ToUpper(base) {
		case "CON", "PRN", "AUX", "NUL",
			"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
			"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
			return errors.New("path contains a Windows reserved device name")
		}
	}
	return nil
}

func gitBinary(
	ctx context.Context,
	root string,
	maximum int,
	arguments ...string,
) ([]byte, error) {
	// #nosec G204 -- the executable is fixed, no shell is used, and caller
	// arguments are validated Git object IDs or repository-owned literals.
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = root
	command.Env = process.IndependentEnvironment()
	var stdout boundedBuffer
	stdout.maximum = maximum
	var stderr boundedBuffer
	stderr.maximum = maximumGitError
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(stderr.String()))
	}
	if stdout.truncated {
		return nil, fmt.Errorf("git %s output exceeds %d bytes", strings.Join(arguments, " "), maximum)
	}
	return bytes.Clone(stdout.Bytes()), nil
}

type boundedBuffer struct {
	content   bytes.Buffer
	maximum   int
	truncated bool
}

func (buffer *boundedBuffer) Write(content []byte) (int, error) {
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

func (buffer *boundedBuffer) Bytes() []byte { return buffer.content.Bytes() }

func (buffer *boundedBuffer) String() string { return buffer.content.String() }
