package verify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/spice-framework/development/internal/catalog"
)

func TestRunReportsInCatalogOrderAndSelectsMode(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	value := verificationCatalog(t, root)
	nested := filepath.Join(root, "b", "fixture")
	if err := os.Mkdir(nested, 0o750); err != nil {
		t.Fatal(err)
	}
	value.Repositories[1].Full[0].Directory = "fixture"
	runner := new(recordingRunner)
	results, err := Run(t.Context(), value, Options{
		Root: root, Mode: Full, Jobs: 2,
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Repository != "a" ||
		results[1].Repository != "b" || results[0].Commands != 1 ||
		results[1].Commands != 1 {
		t.Fatalf("Run() = %#v", results)
	}
	for _, call := range runner.calls {
		if !slices.Contains(call, "full") {
			t.Fatalf("full mode call = %v", call)
		}
	}
	if !slices.Contains(runner.directories, nested) {
		t.Fatalf("working directories = %v, want %s", runner.directories, nested)
	}
}

func TestRunRejectsMissingOrNonDirectoryInvocationDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	value := verificationCatalog(t, root)
	value.Repositories[0].Fast[0].Directory = "missing"
	_, err := Run(t.Context(), value, Options{
		Root: root, Mode: Fast, Jobs: 1, Repositories: []string{"a"},
	}, new(recordingRunner))
	if err == nil || !strings.Contains(err.Error(), "inspect working directory") {
		t.Fatalf("Run(missing directory) error = %v", err)
	}
	file := filepath.Join(root, "a", "fixture")
	if err := os.WriteFile(file, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value.Repositories[0].Fast[0].Directory = "fixture"
	_, err = Run(t.Context(), value, Options{
		Root: root, Mode: Fast, Jobs: 1, Repositories: []string{"a"},
	}, new(recordingRunner))
	if err == nil || !strings.Contains(err.Error(), "not a real directory") {
		t.Fatalf("Run(file directory) error = %v", err)
	}
}

func TestRunSelectsRepositoriesAndRejectsInvalidOptions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	value := verificationCatalog(t, root)
	results, err := Run(t.Context(), value, Options{
		Root: root, Mode: Fast, Jobs: 1, Repositories: []string{"b"},
	}, new(recordingRunner))
	if err != nil || len(results) != 1 || results[0].Repository != "b" {
		t.Fatalf("Run(selected) = %#v, %v", results, err)
	}
	for name, options := range map[string]Options{
		"mode":      {Root: root, Mode: "other", Jobs: 1},
		"jobs":      {Root: root, Mode: Fast, Jobs: 0},
		"unknown":   {Root: root, Mode: Fast, Jobs: 1, Repositories: []string{"missing"}},
		"duplicate": {Root: root, Mode: Fast, Jobs: 1, Repositories: []string{"a", "a"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Run(t.Context(), value, options, new(recordingRunner)); err == nil {
				t.Fatal("Run() error = nil")
			}
		})
	}
}

func TestRunCancelsAfterRepositoryFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	value := verificationCatalog(t, root)
	runner := &recordingRunner{failure: errors.New("failed")}
	results, err := Run(t.Context(), value, Options{
		Root: root, Mode: Fast, Jobs: 1,
	}, runner)
	if err == nil || !strings.Contains(err.Error(), "failed") ||
		len(results) != 2 || results[0].Err == nil || results[1].Err == nil {
		t.Fatalf("Run(failure) = %#v, %v", results, err)
	}
}

func TestRunReportsActualFailureBeforeCanceledSibling(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	value := verificationCatalog(t, root)
	runner := &namedFailureRunner{failedArgument: "b"}
	for index := range value.Repositories {
		value.Repositories[index].Fast[0].Arguments = []string{"go", value.Repositories[index].Name}
	}
	_, err := Run(t.Context(), value, Options{
		Root: root, Mode: Fast, Jobs: 2,
	}, runner)
	if err == nil || !strings.Contains(err.Error(), "repository b") {
		t.Fatalf("Run(actual failure) error = %v", err)
	}
}

type recordingRunner struct {
	mu          sync.Mutex
	calls       [][]string
	directories []string
	failure     error
}

type namedFailureRunner struct {
	failedArgument string
}

func (runner *namedFailureRunner) Run(
	ctx context.Context,
	_ string,
	arguments ...string,
) (string, error) {
	if len(arguments) > 1 && arguments[1] == runner.failedArgument {
		return "", errors.New("failed")
	}
	<-ctx.Done()
	return "", ctx.Err()
}

func (runner *recordingRunner) Run(
	ctx context.Context,
	directory string,
	arguments ...string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	runner.mu.Lock()
	runner.calls = append(runner.calls, slices.Clone(arguments))
	runner.directories = append(runner.directories, directory)
	failure := runner.failure
	runner.failure = nil
	runner.mu.Unlock()
	if failure != nil {
		return "failed output", failure
	}
	return "ok", nil
}

func verificationCatalog(t *testing.T, root string) catalog.Catalog {
	t.Helper()
	repositories := make([]catalog.Repository, 0, 2)
	for _, name := range []string{"a", "b"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o750); err != nil {
			t.Fatal(err)
		}
		repositories = append(repositories, catalog.Repository{
			Name: name, Directory: name, Status: "active",
			CanonicalURL: "https://github.com/spice-framework/" + name,
			CloneURL:     "https://github.com/spice-framework/" + name + ".git",
			Artifact:     "docs", Dependencies: []string{},
			Fast: []catalog.Invocation{{Name: "fast", Arguments: []string{"go", "fast"}}},
			Full: []catalog.Invocation{{Name: "full", Arguments: []string{"go", "full"}}},
		})
	}
	return catalog.Catalog{
		Schema: catalog.CurrentSchema,
		Toolchains: catalog.Toolchains{
			Go: "1.26.5", Java: "25", GoLand: "2026.2.0.1",
		},
		StarterCompatibility: testStarterCompatibilityPolicy(),
		Repositories:         repositories,
	}
}

func testStarterCompatibilityPolicy() catalog.StarterCompatibilityPolicy {
	return catalog.StarterCompatibilityPolicy{
		RepositoryPrefix: "starter-", MetadataFile: "spice-compatibility.json",
		MetadataSchema: 1, CoreModule: "github.com/spice-framework/spice",
		CurrentCore: "v0.0.0-20260806053623-2ec6f862073f",
	}
}
