package process

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
	if !allowedExecutable("cargo") {
		t.Fatal("cargo is not an approved repository verification executable")
	}
	if !allowedExecutable("java") {
		t.Fatal("java is not an approved repository verification executable")
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
	output, err := (ExecRunner{}).Run(t.Context(), module, "go", "env", "GOWORK", "GOPROXY")
	if fields := strings.Fields(output); err != nil ||
		!slices.Equal(fields, []string{"off", "off"}) {
		t.Fatalf("Run(go env GOWORK GOPROXY) = %q, %v", output, err)
	}
	values := make(map[string]string)
	for _, entry := range IndependentEnvironment() {
		name, value, _ := strings.Cut(entry, "=")
		values[strings.ToUpper(name)] = value
	}
	if values["CARGO_NET_OFFLINE"] != "true" ||
		values["RUSTUP_AUTO_INSTALL"] != "0" ||
		values["GIT_NO_LAZY_FETCH"] != "1" ||
		values["GIT_NO_REPLACE_OBJECTS"] != "1" ||
		values["GIT_OPTIONAL_LOCKS"] != "0" ||
		values["GIT_TERMINAL_PROMPT"] != "0" ||
		values["GOTOOLCHAIN"] != "local" || values["GOFLAGS"] != "" {
		t.Fatalf("independent process environment = %#v", values)
	}
}

func TestIndependentEnvironmentRejectsRepositoryRedirection(t *testing.T) {
	t.Parallel()
	values := make(map[string]string)
	for _, entry := range independentEnvironmentFrom([]string{
		"PATH=fixture",
		"GIT_DIR=elsewhere",
		"git_work_tree=elsewhere",
		"GIT_OBJECT_DIRECTORY=elsewhere",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=elsewhere",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.fsmonitor",
		"GIT_CONFIG_VALUE_0=malicious",
		"GIT_NO_REPLACE_OBJECTS=0",
		"GIT_NO_LAZY_FETCH=0",
	}) {
		name, value, _ := strings.Cut(entry, "=")
		values[strings.ToUpper(name)] = value
	}
	if values["PATH"] != "fixture" || values["GIT_DIR"] != "" ||
		values["GIT_WORK_TREE"] != "" || values["GIT_OBJECT_DIRECTORY"] != "" ||
		values["GIT_ALTERNATE_OBJECT_DIRECTORIES"] != "" ||
		values["GIT_CONFIG_COUNT"] != "" || values["GIT_CONFIG_KEY_0"] != "" ||
		values["GIT_CONFIG_VALUE_0"] != "" || values["GIT_NO_REPLACE_OBJECTS"] != "1" ||
		values["GIT_NO_LAZY_FETCH"] != "1" {
		t.Fatalf("independent Git environment = %#v", values)
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
