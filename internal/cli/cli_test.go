package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/spice-framework/development/internal/catalog"
)

func TestRuntimeCatalogAndHelp(t *testing.T) {
	t.Parallel()
	runtime := testRuntime(t)
	var stdout, stderr strings.Builder
	if code := runtime.Run(t.Context(), []string{"catalog", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("catalog code=%d stderr=%q", code, stderr.String())
	}
	var decoded catalog.Catalog
	if err := json.Unmarshal([]byte(stdout.String()), &decoded); err != nil ||
		decoded.Schema != catalog.CurrentSchema {
		t.Fatalf("catalog JSON = %#v, %v", decoded, err)
	}
	stdout.Reset()
	if code := runtime.Run(t.Context(), nil, &stdout, &stderr); code != 0 ||
		!strings.Contains(stdout.String(), "spice-dev bootstrap") {
		t.Fatalf("help code=%d stdout=%q", code, stdout.String())
	}
	stdout.Reset()
	if code := runtime.Run(t.Context(), []string{"version"}, &stdout, &stderr); code != 0 ||
		!strings.Contains(stdout.String(), Version) {
		t.Fatalf("version code=%d stdout=%q", code, stdout.String())
	}
	stdout.Reset()
	if code := runtime.Run(t.Context(), []string{"catalog"}, &stdout, &stderr); code != 0 ||
		!strings.Contains(stdout.String(), "spice\tactive") {
		t.Fatalf("catalog text code=%d stdout=%q", code, stdout.String())
	}
}

func TestMainUsesEmbeddedCatalog(t *testing.T) {
	t.Parallel()
	var stdout, stderr strings.Builder
	if code := Main(t.Context(), []string{"catalog"}, &stdout, &stderr); code != 0 ||
		!strings.Contains(stdout.String(), "development\tactive") {
		t.Fatalf("Main(catalog) code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRuntimeWorkspaceAndVerification(t *testing.T) {
	t.Parallel()
	runtime := testRuntime(t)
	root := t.TempDir()
	for _, repository := range runtime.Catalog.Active() {
		directory := filepath.Join(root, repository.Directory)
		if err := os.Mkdir(directory, 0o750); err != nil {
			t.Fatal(err)
		}
		if repository.Artifact == "go-module" {
			if err := os.WriteFile(
				filepath.Join(directory, "go.mod"),
				[]byte("module "+repository.Module+"\n"),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
		}
	}
	var stdout, stderr strings.Builder
	if code := runtime.Run(t.Context(), []string{
		"workspace", "--root", root,
	}, &stdout, &stderr); code != 0 {
		t.Fatalf("workspace code=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	if code := runtime.Run(t.Context(), []string{
		"workspace", "--root", root, "--check",
	}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "current") {
		t.Fatalf("workspace check code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := runtime.Run(t.Context(), []string{
		"verify", "--root", root, "--jobs", "1", "--repo", "development",
	}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "development\tpassed") {
		t.Fatalf("verify code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := runtime.Run(t.Context(), []string{
		"verify", "--root", root, "--full", "--jobs", "2", "--repo", "spice",
	}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "spice\tpassed") {
		t.Fatalf("full verify code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRuntimeBootstrapReportsExplicitActions(t *testing.T) {
	t.Parallel()
	runtime := testRuntime(t)
	var stdout, stderr strings.Builder
	root := filepath.Join(t.TempDir(), "workspace")
	if code := runtime.Run(t.Context(), []string{
		"bootstrap", "--root", root,
	}, &stdout, &stderr); code != 0 ||
		strings.Count(stdout.String(), "\tcloned\t") != len(runtime.Catalog.Active()) {
		t.Fatalf("bootstrap code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRuntimeRejectsInvalidCommandsAndWrites(t *testing.T) {
	t.Parallel()
	runtime := testRuntime(t)
	var stdout, stderr strings.Builder
	if code := runtime.Run(t.Context(), []string{"unknown"}, &stdout, &stderr); code != 2 {
		t.Fatalf("unknown code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{"catalog", "extra"}, &stdout, &stderr); code != 2 {
		t.Fatalf("catalog positional code = %d", code)
	}
	if code := runtime.Run(nil, nil, &stdout, &stderr); code != 1 { //nolint:staticcheck // Intentional fail-closed boundary case.
		t.Fatalf("nil context code = %d", code)
	}
	if code := runtime.Run(t.Context(), nil, errorWriter{}, &stderr); code != 1 {
		t.Fatalf("help write failure code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{"version"}, errorWriter{}, &stderr); code != 1 {
		t.Fatalf("version write failure code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{"catalog"}, errorWriter{}, &stderr); code != 1 {
		t.Fatalf("catalog write failure code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{"bootstrap", "--root", t.TempDir(), "extra"}, &stdout, &stderr); code != 2 {
		t.Fatalf("bootstrap positional code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{"workspace", "--root", t.TempDir(), "extra"}, &stdout, &stderr); code != 2 {
		t.Fatalf("workspace positional code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{"verify", "--root", t.TempDir(), "--jobs", "0"}, &stdout, &stderr); code != 1 {
		t.Fatalf("verify invalid jobs code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{"catalog", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("catalog help code = %d", code)
	}
	var values stringList
	if err := values.Set(""); err == nil {
		t.Fatal("stringList.Set(empty) error = nil")
	}
	if code := commandError(errorWriter{}, "test", errors.New("failed")); code != 1 {
		t.Fatalf("commandError(write failure) code = %d", code)
	}
}

type fakeRunner struct {
	mu    sync.Mutex
	calls [][]string
}

func (runner *fakeRunner) Run(
	ctx context.Context,
	_ string,
	arguments ...string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	runner.mu.Lock()
	runner.calls = append(runner.calls, slices.Clone(arguments))
	runner.mu.Unlock()
	return "ok", nil
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func testRuntime(t *testing.T) Runtime {
	t.Helper()
	value, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	return Runtime{Catalog: value, Runner: new(fakeRunner)}
}
