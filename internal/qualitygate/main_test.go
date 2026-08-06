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

func TestGateHelpersPreserveBoundaryFailures(t *testing.T) {
	t.Parallel()
	want := errors.New("boundary failed")
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "source.go"),
		[]byte("package fixture\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	if err := verifyGo(t.Context(), root, &gateRunner{failure: want}); !errors.Is(err, want) {
		t.Fatalf("verifyGo() error = %v", err)
	}
	missingRoot := filepath.Join(t.TempDir(), "missing")
	if err := formatting(t.Context(), missingRoot, &gateRunner{}); err == nil ||
		!strings.Contains(err.Error(), "discover Go source") {
		t.Fatalf("formatting(missing root) error = %v", err)
	}
	if err := formatting(t.Context(), root, &gateRunner{failure: want}); !errors.Is(err, want) {
		t.Fatalf("formatting(runner failure) error = %v", err)
	}

	coverageDirectory := t.TempDir()
	testFailure := &gateRunner{
		failure:          want,
		failureArguments: []string{"go", "test"},
	}
	if err := coverageInDirectory(
		t.Context(), root, coverageDirectory, testFailure,
	); !errors.Is(err, want) {
		t.Fatalf("coverageInDirectory(test failure) error = %v", err)
	}
	coverFailure := &gateRunner{
		failure:          want,
		failureArguments: []string{"go", "tool", "cover"},
	}
	if err := coverageInDirectory(
		t.Context(), root, coverageDirectory, coverFailure,
	); !errors.Is(err, want) {
		t.Fatalf("coverageInDirectory(cover failure) error = %v", err)
	}
	malformed := &gateRunner{coverageOutput: "missing total\n"}
	if err := coverageInDirectory(
		t.Context(), root, coverageDirectory, malformed,
	); err == nil || !strings.Contains(err.Error(), "no total") {
		t.Fatalf("coverageInDirectory(malformed output) error = %v", err)
	}
	if _, err := totalCoverage("total:\t(statements)\tnot-a-number%\n"); err == nil {
		t.Fatal("totalCoverage(invalid percentage) error = nil")
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
	mu               sync.Mutex
	calls            [][]string
	goVersion        string
	formatting       string
	coverage         float64
	coverageOutput   string
	failure          error
	failureArguments []string
}

func (runner *gateRunner) Run(
	_ context.Context,
	_ string,
	arguments ...string,
) (string, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.calls = append(runner.calls, slices.Clone(arguments))
	if runner.failure != nil &&
		(len(runner.failureArguments) == 0 ||
			slices.Equal(arguments[:min(len(arguments), len(runner.failureArguments))], runner.failureArguments)) {
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
		if runner.coverageOutput != "" {
			return runner.coverageOutput, nil
		}
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
