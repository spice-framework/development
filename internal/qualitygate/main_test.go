package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestTotalCoverage(t *testing.T) {
	t.Parallel()
	total, err := totalCoverage("example.go:1:\tFunction\t80.0%\ntotal:\t(statements)\t87.5%\n")
	if err != nil || total != 87.5 {
		t.Fatalf("totalCoverage() = %v, %v", total, err)
	}
	if _, err := totalCoverage("missing\n"); err == nil {
		t.Fatal("totalCoverage(missing) error = nil")
	}
}

func TestRunAtExecutesCompleteGate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "source.go"),
		[]byte("package fixture\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	runner := &gateRunner{coverage: 91.5}
	if err := runAt(t.Context(), root, runner); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 10 {
		t.Fatalf("runAt() calls = %#v", runner.calls)
	}
	for _, executable := range []string{"gofmt", "git", "go"} {
		if !runner.used(executable) {
			t.Errorf("runAt() did not use %s", executable)
		}
	}
}

func TestGateHelpersRejectToolchainFormattingAndCoverageFailures(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "source.go"),
		[]byte("package fixture\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	wrongGo := &gateRunner{goVersion: "go version go1.25.0 windows/amd64", coverage: 90}
	if err := verifyGo(t.Context(), root, wrongGo); err == nil {
		t.Fatal("verifyGo(wrong) error = nil")
	}
	dirty := &gateRunner{formatting: "source.go", coverage: 90}
	if err := formatting(t.Context(), root, dirty); err == nil {
		t.Fatal("formatting(dirty) error = nil")
	}
	low := &gateRunner{coverage: 84.9}
	if err := coverage(t.Context(), root, low); err == nil ||
		!strings.Contains(err.Error(), "below") {
		t.Fatalf("coverage(low) error = %v", err)
	}
}

func TestCommandReturnsRunnerFailure(t *testing.T) {
	t.Parallel()
	want := errors.New("failed")
	runner := &gateRunner{failure: want}
	if err := command(t.Context(), t.TempDir(), runner, "go", "test"); !errors.Is(err, want) {
		t.Fatalf("command() error = %v", err)
	}
}

type gateRunner struct {
	mu         sync.Mutex
	calls      [][]string
	goVersion  string
	formatting string
	coverage   float64
	failure    error
}

func (runner *gateRunner) Run(
	_ context.Context,
	_ string,
	arguments ...string,
) (string, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.calls = append(runner.calls, slices.Clone(arguments))
	if runner.failure != nil {
		return "runner output", runner.failure
	}
	if slices.Equal(arguments, []string{"go", "version"}) {
		if runner.goVersion != "" {
			return runner.goVersion, nil
		}
		return "go version go1.26.5 windows/amd64", nil
	}
	if len(arguments) >= 2 && arguments[0] == "gofmt" {
		return runner.formatting, nil
	}
	if len(arguments) >= 3 && arguments[0] == "go" &&
		arguments[1] == "tool" && arguments[2] == "cover" {
		return fmt.Sprintf("total:\t(statements)\t%.1f%%\n", runner.coverage), nil
	}
	return "", nil
}

func (runner *gateRunner) used(executable string) bool {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	for _, call := range runner.calls {
		if len(call) != 0 && call[0] == executable {
			return true
		}
	}
	return false
}
