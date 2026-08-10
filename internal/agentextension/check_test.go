package agentextension

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spice-framework/development/internal/catalog"
)

type checkRunner struct {
	output string
	err    error
	calls  int
}

func (runner *checkRunner) Run(ctx context.Context, _ string, arguments ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	runner.calls++
	if len(arguments) != 4 || strings.Join(arguments, " ") != "go mod edit -json" {
		return "", errors.New("unexpected process invocation")
	}
	return runner.output, runner.err
}

func TestCheckAcceptsExactMaterializedScaffold(t *testing.T) {
	t.Parallel()
	root, profile := materializedFixture(t)
	runner := &checkRunner{output: moduleJSON(t, "example.com/acme/agent-tool", profile, "")}
	result, err := Check(t.Context(), root, testCatalog(t), runner)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Materialized || result.Root != root || runner.calls != 1 {
		t.Fatalf("Check() = %#v, calls=%d", result, runner.calls)
	}
}

func TestCheckRejectsManifestAndModuleMutations(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		mutate    func(*testing.T, string)
		directive string
		want      string
	}{
		"unknown manifest field": {
			mutate: func(t *testing.T, root string) {
				path := filepath.Join(root, "spice-agent-extension.json")
				replaceFileContent(t, path, "\n}", ",\n  \"mystery\": true\n}")
			},
			want: "unknown field",
		},
		"stale profile": {
			mutate: func(t *testing.T, root string) {
				path := filepath.Join(root, "spice-agent-extension.json")
				replaceFileContent(t, path, ProfileID, "compiled-tool/latest")
			},
			want: "not catalog-authorized",
		},
		"unsafe composition path": {
			mutate: func(t *testing.T, root string) {
				path := filepath.Join(root, "spice-agent-extension.json")
				replaceFileContent(t, path, "internal/spicegen/extensionproof", "../escape")
			},
			want: "composition layout is stale",
		},
		"trailing manifest JSON": {
			mutate: func(t *testing.T, root string) {
				path := filepath.Join(root, "spice-agent-extension.json")
				mustWrite(t, path, append(mustRead(t, path), []byte("{}\n")...))
			},
			want: "trailing JSON",
		},
		"missing sum": {
			mutate: func(t *testing.T, root string) {
				path := filepath.Join(root, "go.sum")
				replaceFileContent(t, path, "github.com/spice-framework/spice-agent v0.1.0-preview.6 h1:XJKJge+xWP/FLNoL1/rXq8z8tdu/5iEkKfmu1dTgFms=\n", "")
			},
			want: "missing catalog pin",
		},
		"stale vendor": {
			mutate: func(t *testing.T, root string) {
				path := filepath.Join(root, "vendor", "modules.txt")
				replaceFileContent(t, path, "# github.com/spice-framework/spice-agent v0.1.0-preview.6", "# github.com/spice-framework/spice-agent v0.1.0-preview.5")
			},
			want: "missing exact selection",
		},
		"missing generation": {
			mutate: func(t *testing.T, root string) {
				if err := os.Remove(filepath.Join(root, "internal", "spicegen", "extensionproof", "proof.go")); err != nil {
					t.Fatal(err)
				}
			},
			want: "generated proof",
		},
		"replace directive": {directive: "replace", want: "must not contain replace"},
		"exclude directive": {directive: "exclude", want: "must not contain replace"},
		"retract directive": {directive: "retract", want: "must not contain replace"},
		"ignore directive":  {directive: "ignore", want: "must not contain replace"},
		"module pin":        {directive: "module-pin", want: "go.mod requires github.com/spice-framework/spice-agent"},
		"unapproved tool":   {directive: "tool", want: "Go tools are"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, profile := materializedFixture(t)
			if test.mutate != nil {
				test.mutate(t, root)
			}
			runner := &checkRunner{output: moduleJSON(t, "example.com/acme/agent-tool", profile, test.directive)}
			if _, err := Check(t.Context(), root, testCatalog(t), runner); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Check() error = %v, want %q", err, test.want)
			}
		})
	}
}

func replaceFileContent(t *testing.T, path, old, replacement string) {
	t.Helper()
	content := string(mustRead(t, path))
	mutated := strings.Replace(content, old, replacement, 1)
	if mutated == content {
		t.Fatalf("mutation %q did not apply to %s", old, path)
	}
	mustWrite(t, path, []byte(mutated))
}

func TestCheckRejectsUnmaterializedLinksCancellationAndRunnerFailures(t *testing.T) {
	t.Parallel()
	ecosystem := testCatalog(t)
	root := filepath.Join(t.TempDir(), "source")
	if _, err := Init(t.Context(), InitOptions{Directory: root, Module: "example.com/acme/agent-tool", ToolName: "acme.inspect", Profile: ProfileID}, ecosystem); err != nil {
		t.Fatal(err)
	}
	profile, _ := ecosystem.AgentExtensionProfile(ProfileID)
	runner := &checkRunner{output: moduleJSON(t, "example.com/acme/agent-tool", profile, "")}
	if _, err := Check(t.Context(), root, ecosystem, runner); err == nil || !strings.Contains(err.Error(), "not materialized") {
		t.Fatalf("Check(source-only) error = %v", err)
	}
	if _, err := Check(nil, root, ecosystem, runner); err == nil { //nolint:staticcheck // Fail-closed nil boundary.
		t.Fatal("Check(nil) error = nil")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Check(ctx, root, ecosystem, runner); !errors.Is(err, context.Canceled) {
		t.Fatalf("Check(cancelled) error = %v", err)
	}
	if _, err := Check(t.Context(), root, ecosystem, nil); err == nil || !strings.Contains(err.Error(), "runner") {
		t.Fatalf("Check(nil runner) error = %v", err)
	}

	materialized, profile := materializedFixture(t)
	failing := &checkRunner{err: errors.New("offline inspection failed")}
	if _, err := Check(t.Context(), materialized, ecosystem, failing); err == nil || !strings.Contains(err.Error(), "offline inspection failed") {
		t.Fatalf("Check(runner failure) error = %v", err)
	}
	_ = profile
}

func TestCheckRejectsTreeSymlink(t *testing.T) {
	root, profile := materializedFixture(t)
	link := filepath.Join(root, "docs", "linked.md")
	if err := os.Symlink(filepath.Join(root, "README.md"), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	runner := &checkRunner{output: moduleJSON(t, "example.com/acme/agent-tool", profile, "")}
	if _, err := Check(t.Context(), root, testCatalog(t), runner); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Check(symlink) error = %v", err)
	}
}

func TestCheckRejectsStrictManifestBoundaries(t *testing.T) {
	t.Parallel()
	ecosystem := testCatalog(t)
	profile, _ := ecosystem.AgentExtensionProfile(ProfileID)
	root, _ := materializedFixture(t)
	content := mustRead(t, filepath.Join(root, "spice-agent-extension.json"))
	manifest, err := ParseManifest(content)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Manifest){
		"stale kind":          func(value *Manifest) { value.Kind = "runtime-plugin" },
		"invalid module":      func(value *Manifest) { value.Module = "local/module" },
		"invalid symbol":      func(value *Manifest) { value.Manifest.Name = "Other" },
		"invalid tool name":   func(value *Manifest) { value.ToolName = " bad" },
		"stale documentation": func(value *Manifest) { value.Documentation.Security = "docs/security.md" },
		"network enabled":     func(value *Manifest) { value.Prohibitions.HiddenNetwork = true },
	} {
		t.Run(name, func(t *testing.T) {
			value := manifest
			mutate(&value)
			if err := validateManifest(value, profile); err == nil {
				t.Fatal("validateManifest() error = nil")
			}
		})
	}
}

func TestCheckRejectsOfflineMetadataCorruption(t *testing.T) {
	t.Parallel()
	root, profile := materializedFixture(t)
	manifestContent := mustRead(t, filepath.Join(root, "spice-agent-extension.json"))
	manifest, err := ParseManifest(manifestContent)
	if err != nil {
		t.Fatal(err)
	}
	validModule := moduleJSON(t, manifest.Module, profile, "")
	for name, output := range map[string]string{
		"malformed module JSON": "{",
		"trailing module JSON":  validModule + `{}`,
		"wrong module identity": strings.Replace(validModule, manifest.Module, "example.com/other/tool", 1),
		"unknown module field":  strings.Replace(validModule, `{"Module":`, `{"Unknown":true,"Module":`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			runner := &checkRunner{output: output}
			if err := verifyModule(t.Context(), root, manifest, profile, runner); err == nil {
				t.Fatal("verifyModule() error = nil")
			}
		})
	}

	var metadata modMetadata
	if err := json.Unmarshal([]byte(validModule), &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.Require = append(metadata.Require, metadata.Require[0])
	duplicate, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyModule(t.Context(), root, manifest, profile, &checkRunner{output: string(duplicate)}); err == nil ||
		!strings.Contains(err.Error(), "repeats requirement") {
		t.Fatalf("verifyModule(duplicate) error = %v", err)
	}
}

func TestCheckRejectsCompatibilitySumsAndBoundedFileFailures(t *testing.T) {
	t.Parallel()
	root, profile := materializedFixture(t)
	compatibilityPath := filepath.Join(root, "spice-compatibility.json")
	compatibility := mustRead(t, compatibilityPath)
	mustWrite(t, compatibilityPath, bytes.Replace(compatibility, []byte(`"go": "1.26.5"`), []byte(`"go": "1.26.4"`), 1))
	if err := verifyCompatibility(root, profile); err == nil || !strings.Contains(err.Error(), "want") {
		t.Fatalf("verifyCompatibility() error = %v", err)
	}

	root, profile = materializedFixture(t)
	sumPath := filepath.Join(root, "go.sum")
	sums := mustRead(t, sumPath)
	mustWrite(t, sumPath, append(sums, sums...))
	if err := verifySums(root, profile); err == nil || !strings.Contains(err.Error(), "repeats") {
		t.Fatalf("verifySums() error = %v", err)
	}

	if _, err := readBounded(root); err == nil || !strings.Contains(err.Error(), "bounded regular file") {
		t.Fatalf("readBounded(directory) error = %v", err)
	}
	missing := filepath.Join(root, "missing.json")
	if _, err := readBounded(missing); err == nil || !strings.Contains(err.Error(), "missing.json") {
		t.Fatalf("readBounded(missing) error = %v", err)
	}
}

func FuzzParseManifest(f *testing.F) {
	root, _ := materializedFixture(f)
	f.Add(mustRead(f, filepath.Join(root, "spice-agent-extension.json")))
	f.Add([]byte(`{"schema":"spice.agent.extension/v1alpha1"}`))
	f.Fuzz(func(t *testing.T, content []byte) {
		value, err := ParseManifest(content)
		if err == nil && value.Schema == "" {
			t.Fatal("accepted manifest has no schema")
		}
	})
}

func materializedFixture(t testing.TB) (string, catalog.AgentExtensionProfile) {
	t.Helper()
	ecosystem := testCatalogTB(t)
	root := filepath.Join(t.TempDir(), "extension")
	if _, err := Init(t.Context(), InitOptions{
		Directory: root, Module: "example.com/acme/agent-tool", ToolName: "acme.inspect", Profile: ProfileID,
	}, ecosystem); err != nil {
		t.Fatal(err)
	}
	profile, _ := ecosystem.AgentExtensionProfile(ProfileID)
	pending := filepath.Join(root, "internal", "composition", "composition_test.go.pending")
	active := filepath.Join(root, "internal", "composition", "composition_test.go")
	if err := os.Rename(pending, active); err != nil {
		t.Fatal(err)
	}
	var vendor strings.Builder
	for _, module := range profile.Modules {
		vendor.WriteString("# " + module.Path + " " + module.Version + "\n")
	}
	writeFixture(t, root, "vendor/modules.txt", vendor.String())
	writeFixture(t, root, profile.Composition.OwnershipFile, "{}\n")
	writeFixture(t, root, profile.Composition.Generated+"/proof.go", "package extensionproof\n")
	writeFixture(t, root, profile.Composition.Generated+"/sources/source.go", "package sources\n")
	return root, profile
}

func moduleJSON(t testing.TB, module string, profile catalog.AgentExtensionProfile, directive string) string {
	t.Helper()
	metadata := modMetadata{Go: profile.GoDirective, Toolchain: profile.GoToolchain}
	metadata.Module.Path = module
	for _, selected := range profile.Modules {
		metadata.Require = append(metadata.Require, struct {
			Path     string
			Version  string
			Indirect bool
		}{Path: selected.Path, Version: selected.Version, Indirect: selected.Path == "github.com/spice-framework/toolchain"})
	}
	for _, selected := range profile.Tools {
		metadata.Tool = append(metadata.Tool, struct{ Path string }{Path: selected})
	}
	switch directive {
	case "replace":
		metadata.Replace = []json.RawMessage{json.RawMessage(`{"Old":{"Path":"example.com/old"},"New":{"Path":"../local"}}`)}
	case "exclude":
		metadata.Exclude = []json.RawMessage{json.RawMessage(`{"Path":"example.com/old","Version":"v1.0.0"}`)}
	case "retract":
		metadata.Retract = []json.RawMessage{json.RawMessage(`{"Low":"v1.0.0","High":"v1.0.0"}`)}
	case "ignore":
		metadata.Ignore = []json.RawMessage{json.RawMessage(`{"Path":"example.com/ignored"}`)}
	case "module-pin":
		metadata.Require[1].Version = "v0.1.0-preview.4"
	case "tool":
		metadata.Tool = append(metadata.Tool, struct{ Path string }{Path: "example.invalid/cmd/tool"})
	}
	content, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func writeFixture(t testing.TB, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, []byte(content))
}

func mustWrite(t testing.TB, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t testing.TB, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func testCatalogTB(t testing.TB) catalog.Catalog {
	t.Helper()
	value, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	return value
}
