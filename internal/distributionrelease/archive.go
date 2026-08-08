package distributionrelease

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spice-framework/development/internal/catalog"
	"github.com/spice-framework/development/internal/process"
)

type archiveEntry struct {
	name       string
	content    []byte
	executable bool
}

func buildArchive(
	prepared preparedRelease,
	target catalog.ReleaseTarget,
	binaries map[string][]byte,
	payloads map[string][]byte,
) ([]byte, error) {
	entries := make([]archiveEntry, 0, len(binaries)+len(payloads))
	for name, content := range binaries {
		entries = append(entries, archiveEntry{name: name, content: content, executable: true})
	}
	for name, content := range payloads {
		entries = append(entries, archiveEntry{name: name, content: content})
	}
	slices.SortFunc(entries, func(left, right archiveEntry) int { return strings.Compare(left.name, right.name) })
	prefix := targetBase(prepared.repository.Name, prepared.repository.Release.Version, target) + "/"
	if target.GOOS == "windows" {
		return buildZip(prefix, time.Unix(prepared.epoch, 0).UTC(), entries)
	}
	return buildTarGzip(prefix, time.Unix(prepared.epoch, 0).UTC(), entries)
}

func buildTarGzip(prefix string, epoch time.Time, entries []archiveEntry) ([]byte, error) {
	output := limitedArchiveBuffer{maximum: int(maximumArtifactBytes)}
	gzipWriter, err := gzip.NewWriterLevel(&output, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("create distribution gzip writer: %w", err)
	}
	gzipWriter.Header.ModTime = epoch
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		mode := int64(0o644)
		if entry.executable {
			mode = 0o755
		}
		header := &tar.Header{
			Name: prefix + entry.name, Mode: mode, Size: int64(len(entry.content)),
			ModTime: epoch, AccessTime: epoch, ChangeTime: epoch, Typeflag: tar.TypeReg, Format: tar.FormatPAX,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return nil, errors.Join(fmt.Errorf("write distribution tar header %q: %w", entry.name, err), tarWriter.Close(), gzipWriter.Close())
		}
		if _, err := tarWriter.Write(entry.content); err != nil {
			return nil, errors.Join(fmt.Errorf("write distribution tar entry %q: %w", entry.name, err), tarWriter.Close(), gzipWriter.Close())
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, errors.Join(fmt.Errorf("close distribution tar: %w", err), gzipWriter.Close())
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, fmt.Errorf("close distribution gzip: %w", err)
	}
	return output.Bytes(), nil
}

func buildZip(prefix string, epoch time.Time, entries []archiveEntry) ([]byte, error) {
	output := limitedArchiveBuffer{maximum: int(maximumArtifactBytes)}
	writer := zip.NewWriter(&output)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: prefix + entry.name, Method: zip.Deflate}
		header.Modified = epoch
		mode := fs.FileMode(0o644)
		if entry.executable {
			mode = fs.FileMode(0o755)
		}
		header.SetMode(mode)
		file, err := writer.CreateHeader(header)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("write distribution zip header %q: %w", entry.name, err), writer.Close())
		}
		if _, err := file.Write(entry.content); err != nil {
			return nil, errors.Join(fmt.Errorf("write distribution zip entry %q: %w", entry.name, err), writer.Close())
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close distribution zip: %w", err)
	}
	return output.Bytes(), nil
}

type limitedArchiveBuffer struct {
	content bytes.Buffer
	maximum int
}

func (buffer *limitedArchiveBuffer) Write(content []byte) (int, error) {
	remaining := buffer.maximum - buffer.content.Len()
	if remaining <= 0 {
		return 0, fmt.Errorf("distribution archive exceeds %d bytes", buffer.maximum)
	}
	if len(content) > remaining {
		written, _ := buffer.content.Write(content[:remaining])
		return written, fmt.Errorf("distribution archive exceeds %d bytes", buffer.maximum)
	}
	return buffer.content.Write(content)
}

func (buffer *limitedArchiveBuffer) Bytes() []byte {
	return bytes.Clone(buffer.content.Bytes())
}

func loadPayloads(ctx context.Context, prepared preparedRelease) (map[string][]byte, error) {
	result := make(map[string][]byte, len(prepared.repository.Release.PayloadFiles))
	for _, name := range prepared.repository.Release.PayloadFiles {
		content, err := gitBinary(ctx, prepared.root, maximumGitBytes, "show", prepared.commit+":"+name)
		if err != nil {
			return nil, fmt.Errorf("read committed distribution payload %q: %w", name, err)
		}
		result[name] = content
	}
	return result, nil
}

func gitBinary(
	ctx context.Context,
	root string,
	maximum int,
	arguments ...string,
) ([]byte, error) {
	// #nosec G204 -- executable is fixed and arguments are validated catalog
	// paths or full Git object identities, without a shell.
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = root
	command.Env = process.IndependentEnvironment()
	var stdout boundedBuffer
	stdout.maximum = maximum
	var stderr boundedBuffer
	stderr.maximum = maximumBuildOutput
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

func portablePath(name string) error {
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

func targetBase(repository string, version string, target catalog.ReleaseTarget) string {
	return repository + "_" + strings.TrimPrefix(version, "v") + "_" + target.GOOS + "_" + target.GOARCH
}
