package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spice-framework/development/internal/catalog"
)

func TestEnsureOfflineValidatesExactExistingCheckout(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repository := testRepository()
	target := filepath.Join(root, repository.Directory)
	if err := os.MkdirAll(filepath.Join(target, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{outputs: []string{repository.CloneURL}}
	results, err := Ensure(t.Context(), root, testCatalog(repository), true, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != "checked" || len(runner.calls) != 1 {
		t.Fatalf("Ensure() = %#v, calls %#v", results, runner.calls)
	}
}

func TestEnsureClonesMissingRepositoryAndFetchesExistingRepository(t *testing.T) {
	t.Parallel()
	repository := testRepository()
	cloneRoot := t.TempDir()
	cloneRunner := new(fakeRunner)
	results, err := Ensure(t.Context(), cloneRoot, testCatalog(repository), false, cloneRunner)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Action != "cloned" ||
		!slices.Contains(cloneRunner.calls[0], repository.CloneURL) {
		t.Fatalf("clone Ensure() = %#v, calls %#v", results, cloneRunner.calls)
	}
	fetchRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fetchRoot, repository.Directory, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	fetchRunner := &fakeRunner{outputs: []string{repository.CloneURL, ""}}
	results, err = Ensure(t.Context(), fetchRoot, testCatalog(repository), false, fetchRunner)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Action != "fetched" || len(fetchRunner.calls) != 2 ||
		!slices.Contains(fetchRunner.calls[1], "fetch") {
		t.Fatalf("fetch Ensure() = %#v, calls %#v", results, fetchRunner.calls)
	}
}

func TestEnsureRefusesMissingOfflineAndMismatchedRemote(t *testing.T) {
	t.Parallel()
	repository := testRepository()
	if _, err := Ensure(
		t.Context(),
		t.TempDir(),
		testCatalog(repository),
		true,
		new(fakeRunner),
	); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("Ensure(missing offline) error = %v", err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, repository.Directory, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	_, err := Ensure(
		t.Context(),
		root,
		testCatalog(repository),
		false,
		&fakeRunner{outputs: []string{"https://github.com/other/project.git"}},
	)
	if err == nil || !strings.Contains(err.Error(), "refusing to rewrite") {
		t.Fatalf("Ensure(mismatched remote) error = %v", err)
	}
}

func TestEnsureRejectsInvalidInputsAndCancellation(t *testing.T) {
	t.Parallel()
	value := testCatalog(testRepository())
	if _, err := Ensure(nil, ".", value, true, new(fakeRunner)); err == nil { //nolint:staticcheck // Intentional fail-closed boundary case.
		t.Fatal("Ensure(nil) error = nil")
	}
	if _, err := Ensure(t.Context(), ".", value, true, nil); err == nil {
		t.Fatal("Ensure(nil runner) error = nil")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Ensure(ctx, t.TempDir(), value, true, new(fakeRunner)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Ensure(canceled) error = %v", err)
	}
}

func TestPrepareRootCreatesMissingAndRejectsFiles(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "workspace")
	root, err := prepareRoot(missing)
	if err != nil || root != missing {
		t.Fatalf("prepareRoot(missing) = %q, %v", root, err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareRoot(file); err == nil {
		t.Fatal("prepareRoot(file) error = nil")
	}
	if _, err := prepareRoot(""); err == nil {
		t.Fatal("prepareRoot(empty) error = nil")
	}
}

func TestEnsureRejectsUnsafeCheckoutsAndGitFailures(t *testing.T) {
	t.Parallel()
	repository := testRepository()
	fileRoot := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(fileRoot, repository.Directory),
		[]byte("not a checkout\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(
		t.Context(), fileRoot, testCatalog(repository), false, new(fakeRunner),
	); err == nil || !strings.Contains(err.Error(), "not a real directory") {
		t.Fatalf("Ensure(file checkout) error = %v", err)
	}
	nonGitRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(nonGitRoot, repository.Directory), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(
		t.Context(), nonGitRoot, testCatalog(repository), false, new(fakeRunner),
	); err == nil || !strings.Contains(err.Error(), "not a Git checkout") {
		t.Fatalf("Ensure(non-Git) error = %v", err)
	}
	cloneFailure := errors.New("clone failed")
	if _, err := Ensure(
		t.Context(),
		t.TempDir(),
		testCatalog(repository),
		false,
		&fakeRunner{errors: []error{cloneFailure}},
	); !errors.Is(err, cloneFailure) {
		t.Fatalf("Ensure(clone failure) error = %v", err)
	}
	gitRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gitRoot, repository.Directory, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	remoteFailure := errors.New("remote failed")
	if _, err := Ensure(
		t.Context(),
		gitRoot,
		testCatalog(repository),
		false,
		&fakeRunner{errors: []error{remoteFailure}},
	); !errors.Is(err, remoteFailure) {
		t.Fatalf("Ensure(remote failure) error = %v", err)
	}
	fetchFailure := errors.New("fetch failed")
	if _, err := Ensure(
		t.Context(),
		gitRoot,
		testCatalog(repository),
		false,
		&fakeRunner{
			outputs: []string{repository.CloneURL, ""},
			errors:  []error{nil, fetchFailure},
		},
	); !errors.Is(err, fetchFailure) {
		t.Fatalf("Ensure(fetch failure) error = %v", err)
	}
}

type fakeRunner struct {
	outputs []string
	errors  []error
	calls   [][]string
}

func (runner *fakeRunner) Run(
	_ context.Context,
	_ string,
	arguments ...string,
) (string, error) {
	runner.calls = append(runner.calls, slices.Clone(arguments))
	var runErr error
	if len(runner.errors) != 0 {
		runErr = runner.errors[0]
		runner.errors = runner.errors[1:]
	}
	if len(runner.outputs) == 0 {
		return "", runErr
	}
	output := runner.outputs[0]
	runner.outputs = runner.outputs[1:]
	return output, runErr
}

func testRepository() catalog.Repository {
	return catalog.Repository{
		Name:         "core",
		Directory:    "core",
		Status:       "active",
		CanonicalURL: "https://github.com/spice-framework/core",
		CloneURL:     "https://github.com/spice-framework/core.git",
		Artifact:     "go-module",
		Module:       "github.com/spice-framework/core",
		Dependencies: []string{},
		Fast:         []catalog.Invocation{},
		Full:         []catalog.Invocation{},
	}
}

func testCatalog(repository catalog.Repository) catalog.Catalog {
	return catalog.Catalog{
		Schema: catalog.CurrentSchema,
		Toolchains: catalog.Toolchains{
			Go: "1.26.5", Java: "25", GoLand: "2026.2.0.1",
		},
		Repositories: []catalog.Repository{repository},
	}
}
