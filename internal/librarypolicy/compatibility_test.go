package librarypolicy

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

const validMetadata = `{
  "schema": 1,
  "minimum": "v0.1.0",
  "current": "v0.0.0-20260806053623-2ec6f862073f"
}`

func TestInspectReturnsExactLibraryIdentity(t *testing.T) {
	t.Parallel()
	directory := writeCompatibilityFile(t, validMetadata)
	runner := &inspectionRunner{moduleJSON: moduleJSON("v0.1.0", false)}
	inspection, output, executed, err := Inspect(
		t.Context(), directory, compatibilityPolicy(), runner,
	)
	if err != nil || output != "" || !executed ||
		inspection.Module != "github.com/spice-framework/starter-mail" ||
		inspection.Compatibility.Minimum != "v0.1.0" {
		t.Fatalf("Inspect() = %#v, %q, %t, %v", inspection, output, executed, err)
	}
	if want := [][]string{{"go", "mod", "edit", "-json"}}; !slices.EqualFunc(
		runner.calls,
		want,
		slices.Equal,
	) {
		t.Fatalf("runner calls = %v, want %v", runner.calls, want)
	}
}

func TestInspectRejectsInvalidMetadataAndModuleIdentity(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		metadata   string
		moduleJSON string
		want       string
		commands   int
	}{
		"missing metadata": {want: "open spice-compatibility.json"},
		"malformed metadata": {
			metadata: `{"schema":1`, want: "decode starter compatibility metadata",
		},
		"stale current": {
			metadata: `{"schema":1,"minimum":"v0.1.0","current":"v0.2.0"}`,
			want:     "is stale",
		},
		"malformed module metadata": {
			metadata: validMetadata, moduleJSON: `{`, want: "decode go.mod metadata", commands: 1,
		},
		"trailing module metadata": {
			metadata: validMetadata, moduleJSON: moduleJSON("v0.1.0", false) + `{}`,
			want: "trailing JSON", commands: 1,
		},
		"empty module path": {
			metadata:   validMetadata,
			moduleJSON: `{"Module":{"Path":""},"Require":[{"Path":"github.com/spice-framework/spice","Version":"v0.1.0"}]}`,
			want:       "module path must be explicit", commands: 1,
		},
		"missing core requirement": {
			metadata:   validMetadata,
			moduleJSON: `{"Module":{"Path":"github.com/spice-framework/starter-mail"},"Require":[]}`,
			want:       "must directly require", commands: 1,
		},
		"duplicate core requirement": {
			metadata:   validMetadata,
			moduleJSON: `{"Module":{"Path":"github.com/spice-framework/starter-mail"},"Require":[{"Path":"github.com/spice-framework/spice","Version":"v0.1.0"},{"Path":"github.com/spice-framework/spice","Version":"v0.1.0"}]}`,
			want:       "2 requirements", commands: 1,
		},
		"indirect core requirement": {
			metadata: validMetadata, moduleJSON: moduleJSON("v0.1.0", true),
			want: "must be direct", commands: 1,
		},
		"minimum mismatch": {
			metadata: validMetadata, moduleJSON: moduleJSON("v0.2.0", false),
			want: "does not match", commands: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			if test.metadata != "" {
				if err := os.WriteFile(
					filepath.Join(directory, compatibilityPolicy().MetadataFile),
					[]byte(test.metadata),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
			}
			runner := &inspectionRunner{moduleJSON: test.moduleJSON}
			_, _, executed, err := Inspect(t.Context(), directory, compatibilityPolicy(), runner)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Inspect() error = %v, want containing %q", err, test.want)
			}
			if got := len(runner.calls); got != test.commands || executed != (test.commands == 1) {
				t.Fatalf("commands = %d, executed = %t, want %d", got, executed, test.commands)
			}
		})
	}
}

func TestInspectReturnsFailedModuleCommandOutput(t *testing.T) {
	t.Parallel()
	directory := writeCompatibilityFile(t, validMetadata)
	runner := &inspectionRunner{failure: errors.New("canceled")}
	_, output, executed, err := Inspect(t.Context(), directory, compatibilityPolicy(), runner)
	if !executed || output != "module output" || err == nil ||
		!strings.Contains(err.Error(), "inspect go.mod") {
		t.Fatalf("Inspect() = %q, %t, %v", output, executed, err)
	}
}

func TestReadCompatibilityMetadataRejectsNonRegularAndOversizedFiles(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if _, err := readCompatibilityMetadata(directory, "."); err == nil ||
		!strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("readCompatibilityMetadata(directory) error = %v", err)
	}
	name := "spice-compatibility.json"
	if err := os.WriteFile(
		filepath.Join(directory, name),
		make([]byte, maximumCompatibilityMetadata+1),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := readCompatibilityMetadata(directory, name); err == nil ||
		!strings.Contains(err.Error(), "bounded") {
		t.Fatalf("readCompatibilityMetadata(oversized) error = %v", err)
	}
}

type inspectionRunner struct {
	calls      [][]string
	moduleJSON string
	failure    error
}

func (runner *inspectionRunner) Run(
	ctx context.Context,
	_ string,
	arguments ...string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	runner.calls = append(runner.calls, slices.Clone(arguments))
	if runner.failure != nil {
		return "module output", runner.failure
	}
	return runner.moduleJSON, nil
}

func compatibilityPolicy() catalog.StarterCompatibilityPolicy {
	return catalog.StarterCompatibilityPolicy{
		RepositoryPrefix: "starter-",
		MetadataFile:     "spice-compatibility.json",
		MetadataSchema:   1,
		CoreModule:       "github.com/spice-framework/spice",
		CurrentCore:      "v0.0.0-20260806053623-2ec6f862073f",
	}
}

func writeCompatibilityFile(t *testing.T, content string) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(directory, compatibilityPolicy().MetadataFile),
		[]byte(content),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	return directory
}

func moduleJSON(version string, indirect bool) string {
	indirectText := "false"
	if indirect {
		indirectText = "true"
	}
	return `{
  "Module": {"Path": "github.com/spice-framework/starter-mail"},
  "Require": [{
    "Path": "github.com/spice-framework/spice",
    "Version": "` + version + `",
    "Indirect": ` + indirectText + `
  }]
}`
}
