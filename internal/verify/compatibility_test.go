package verify

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

const validStarterMetadata = `{
  "schema": 1,
  "minimum": "v0.1.0",
  "current": "v0.0.0-20260806053623-2ec6f862073f"
}`

func TestRunVerifiesStarterCompatibilityBeforeRepositoryGate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"spice", "starter-mail"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(
		filepath.Join(root, "starter-mail", "spice-compatibility.json"),
		[]byte(validStarterMetadata),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	value := catalog.Catalog{
		Schema: catalog.CurrentSchema,
		Toolchains: catalog.Toolchains{
			Go: "1.26.5", Java: "25", GoLand: "2026.2.0.1",
		},
		StarterCompatibility: testStarterCompatibilityPolicy(),
		Repositories: []catalog.Repository{
			{
				Name: "spice", Directory: "spice", Status: "active",
				CanonicalURL: "https://github.com/spice-framework/spice",
				CloneURL:     "https://github.com/spice-framework/spice.git",
				Artifact:     "go-module", Module: "github.com/spice-framework/spice",
				Dependencies: []string{}, Fast: []catalog.Invocation{}, Full: []catalog.Invocation{},
			},
			{
				Name: "starter-mail", Directory: "starter-mail", Status: "active",
				CanonicalURL: "https://github.com/spice-framework/starter-mail",
				CloneURL:     "https://github.com/spice-framework/starter-mail.git",
				Artifact:     "go-module", Module: "github.com/spice-framework/starter-mail",
				Dependencies: []string{"spice"},
				Fast:         []catalog.Invocation{{Name: "test", Arguments: []string{"go", "test", "./..."}}},
				Full:         []catalog.Invocation{{Name: "test", Arguments: []string{"go", "test", "./..."}}},
			},
		},
	}
	runner := &compatibilityRunner{moduleJSON: directRequirement("v0.1.0", false)}
	results, err := Run(t.Context(), value, Options{
		Root: root, Mode: Fast, Jobs: 1, Repositories: []string{"starter-mail"},
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Commands != 2 || results[0].Output != "test:\nok" {
		t.Fatalf("Run() = %#v", results)
	}
	want := [][]string{{"go", "mod", "edit", "-json"}, {"go", "test", "./..."}}
	if !slices.EqualFunc(runner.calls, want, slices.Equal) {
		t.Fatalf("runner calls = %v, want %v", runner.calls, want)
	}
}

func TestVerifyStarterCompatibilityRejectsInvalidMetadataAndModuleAlignment(t *testing.T) {
	t.Parallel()
	policy := testStarterCompatibilityPolicy()
	for name, test := range map[string]struct {
		metadata   string
		moduleJSON string
		want       string
		commands   int
	}{
		"missing metadata": {want: "open spice-compatibility.json", commands: 0},
		"malformed metadata": {
			metadata: `{"schema":1`, want: "decode starter compatibility metadata", commands: 0,
		},
		"stale current": {
			metadata: `{"schema":1,"minimum":"v0.1.0","current":"v0.2.0"}`,
			want:     "is stale", commands: 0,
		},
		"mismatched minimum": {
			metadata: validStarterMetadata, moduleJSON: directRequirement("v0.2.0", false),
			want: "does not match", commands: 1,
		},
		"indirect requirement": {
			metadata: validStarterMetadata, moduleJSON: directRequirement("v0.1.0", true),
			want: "must be direct", commands: 1,
		},
		"missing requirement": {
			metadata:   validStarterMetadata,
			moduleJSON: `{"Module":{"Path":"github.com/spice-framework/starter-mail"},"Require":[]}`,
			want:       "must directly require", commands: 1,
		},
		"malformed module metadata": {
			metadata: validStarterMetadata, moduleJSON: `{`,
			want: "decode go.mod metadata", commands: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			if test.metadata != "" {
				if err := os.WriteFile(
					filepath.Join(directory, policy.MetadataFile),
					[]byte(test.metadata),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
			}
			runner := &compatibilityRunner{moduleJSON: test.moduleJSON}
			_, executed, err := verifyStarterCompatibility(
				t.Context(), directory, policy, runner,
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("verifyStarterCompatibility() error = %v, want %q", err, test.want)
			}
			if got := len(runner.calls); got != test.commands || executed != (test.commands == 1) {
				t.Fatalf("commands = %d, executed = %t, want %d", got, executed, test.commands)
			}
		})
	}
}

func TestVerifyStarterCompatibilityPropagatesModuleInspectionFailure(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	policy := testStarterCompatibilityPolicy()
	if err := os.WriteFile(
		filepath.Join(directory, policy.MetadataFile),
		[]byte(validStarterMetadata),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	runner := &compatibilityRunner{failure: errors.New("canceled")}
	output, executed, err := verifyStarterCompatibility(
		t.Context(), directory, policy, runner,
	)
	if !executed || output != "module output" || err == nil ||
		!strings.Contains(err.Error(), "inspect go.mod") {
		t.Fatalf("verifyStarterCompatibility() = %q, %t, %v", output, executed, err)
	}
}

type compatibilityRunner struct {
	calls      [][]string
	moduleJSON string
	failure    error
}

func (runner *compatibilityRunner) Run(
	ctx context.Context,
	_ string,
	arguments ...string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	runner.calls = append(runner.calls, slices.Clone(arguments))
	if slices.Equal(arguments, []string{"go", "mod", "edit", "-json"}) {
		if runner.failure != nil {
			return "module output", runner.failure
		}
		return runner.moduleJSON, nil
	}
	return "ok", nil
}

func directRequirement(version string, indirect bool) string {
	indirectValue := "false"
	if indirect {
		indirectValue = "true"
	}
	return `{
  "Module": {"Path": "github.com/spice-framework/starter-mail"},
  "Go": "1.26.0",
  "Require": [{
    "Path": "github.com/spice-framework/spice",
    "Version": "` + version + `",
    "Indirect": ` + indirectValue + `
  }]
}`
}
