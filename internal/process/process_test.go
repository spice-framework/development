package process

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecRunnerUsesDiscreteAllowedCommands(t *testing.T) {
	t.Parallel()
	output, err := (ExecRunner{}).Run(t.Context(), t.TempDir(), "go", "version")
	if err != nil || !bytes.Contains([]byte(output), []byte("go version go1.26.5")) {
		t.Fatalf("Run(go version) = %q, %v", output, err)
	}
	if _, err := (ExecRunner{}).Run(t.Context(), ".", "powershell", "echo"); err == nil {
		t.Fatal("Run(unapproved) error = nil")
	}
	if _, err := (ExecRunner{}).Run(nil, ".", "go", "version"); err == nil { //nolint:staticcheck // Intentional fail-closed boundary case.
		t.Fatal("Run(nil) error = nil")
	}
}

func TestExecRunnerIsolatesRepositoryFromParentGoWorkspace(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	module := filepath.Join(parent, "module")
	if err := os.Mkdir(module, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(parent, "go.work"),
		[]byte("go 1.26.0\n\nuse ./module\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	output, err := (ExecRunner{}).Run(t.Context(), module, "go", "env", "GOWORK")
	if err != nil || strings.TrimSpace(output) != "off" {
		t.Fatalf("Run(go env GOWORK) = %q, %v", output, err)
	}
}

func TestBoundedBufferPreservesLengthContract(t *testing.T) {
	t.Parallel()
	var buffer boundedBuffer
	content := bytes.Repeat([]byte("x"), maximumOutput+10)
	written, err := buffer.Write(content)
	if err != nil || written != len(content) || !buffer.truncated ||
		len(buffer.String()) != maximumOutput {
		t.Fatalf("Write() = %d, truncated=%t, length=%d, err=%v", written, buffer.truncated, len(buffer.String()), err)
	}
	if written, err = buffer.Write([]byte("more")); err != nil || written != 4 {
		t.Fatalf("Write(after full) = %d, %v", written, err)
	}
}

func TestExecRunnerReportsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (ExecRunner{}).Run(ctx, ".", "go", "version")
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(canceled) error = %v", err)
	}
}
