package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spice-framework/development/internal/catalog"
)

func TestRenderAndApplyDeterministicNativeWorkspace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	value, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, repository := range value.Active() {
		directory := filepath.Join(root, repository.Directory)
		if err := os.Mkdir(directory, 0o750); err != nil {
			t.Fatal(err)
		}
		if repository.Artifact == "go-module" {
			content := []byte("module " + repository.Module + "\n\ngo 1.26.0\n")
			if err := os.WriteFile(filepath.Join(directory, "go.mod"), content, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	plan, err := Render(root, value)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"./development", "./spice", goWorkMarker} {
		if !strings.Contains(string(plan.GoWork), expected) {
			t.Fatalf("go.work missing %q:\n%s", expected, plan.GoWork)
		}
	}
	if !strings.Contains(string(plan.Editor), `"name": ".github"`) ||
		!strings.Contains(string(plan.Editor), `"spiceGenerated": true`) {
		t.Fatalf("editor workspace:\n%s", plan.Editor)
	}
	if err := Apply(root, plan, false); err != nil {
		t.Fatal(err)
	}
	if err := Apply(root, plan, true); err != nil {
		t.Fatalf("Apply(check current) error = %v", err)
	}
}

func TestApplyRefusesUnownedAndReportsOwnedStaleFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	plan := Plan{
		GoWork: []byte(goWorkMarker + "\n\ngo 1.26.0\n"),
		Editor: []byte("{\n  \"spiceGenerated\": true,\n  \"folders\": []\n}\n"),
	}
	if err := os.WriteFile(filepath.Join(root, goWorkName), []byte("go 1.26.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Apply(root, plan, false); !errors.Is(err, ErrUnowned) {
		t.Fatalf("Apply(unowned) error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, goWorkName),
		[]byte(goWorkMarker+"\n\ngo 1.25.0\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := Apply(root, plan, true); !errors.Is(err, ErrStale) {
		t.Fatalf("Apply(stale check) error = %v", err)
	}
}

func TestRenderRejectsModuleIdentityMismatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	value := catalog.Catalog{
		Schema: 1,
		Toolchains: catalog.Toolchains{
			Go: "1.26.5", Java: "25", GoLand: "2026.2",
		},
		Repositories: []catalog.Repository{{
			Name: "core", Directory: "core", Status: "active",
			CanonicalURL: "https://github.com/spice-framework/core",
			CloneURL:     "https://github.com/spice-framework/core.git",
			Artifact:     "go-module", Module: "github.com/spice-framework/core",
			Dependencies: []string{}, Fast: []catalog.Invocation{}, Full: []catalog.Invocation{},
		}},
	}
	if err := os.Mkdir(filepath.Join(root, "core"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "core", "go.mod"),
		[]byte("module example.com/wrong\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(root, value); err == nil || !strings.Contains(err.Error(), "catalog expects") {
		t.Fatalf("Render(mismatch) error = %v", err)
	}
}

func TestRenderRejectsMissingAndUnsafeRepositories(t *testing.T) {
	t.Parallel()
	value := documentationCatalog()
	if _, err := Render(t.TempDir(), value); err == nil ||
		!strings.Contains(err.Error(), "no active repository") {
		t.Fatalf("Render(missing) error = %v", err)
	}
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "docs"),
		[]byte("not a directory\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(root, value); err == nil ||
		!strings.Contains(err.Error(), "not a real directory") {
		t.Fatalf("Render(file) error = %v", err)
	}
	if _, err := Render("", value); err == nil {
		t.Fatal("Render(empty root) error = nil")
	}
}

func TestRenderSupportsDocumentationOnlyWorkspace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o750); err != nil {
		t.Fatal(err)
	}
	plan, err := Render(root, documentationCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plan.GoWork), "use (") ||
		!strings.Contains(string(plan.Editor), `"name": "docs"`) {
		t.Fatalf("documentation plan = go.work %q, editor %q", plan.GoWork, plan.Editor)
	}
}

func TestApplyHandlesMissingOwnedAndUnownedEditorFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	plan := Plan{
		GoWork: []byte(goWorkMarker + "\n\ngo 1.26.0\n"),
		Editor: []byte("{\n  \"spiceGenerated\": true,\n  \"folders\": []\n}\n"),
	}
	if err := Apply(root, plan, true); !errors.Is(err, ErrStale) {
		t.Fatalf("Apply(missing check) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, goWorkName), plan.GoWork, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, editorName), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Apply(root, plan, false); !errors.Is(err, ErrUnowned) {
		t.Fatalf("Apply(unowned editor) error = %v", err)
	}
	ownedStale := []byte("{\n  \"spiceGenerated\": true,\n  \"folders\": [{\"name\":\"old\",\"path\":\"old\"}]\n}\n")
	if err := os.WriteFile(filepath.Join(root, editorName), ownedStale, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Apply(root, plan, false); err != nil {
		t.Fatalf("Apply(owned stale) error = %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(root, editorName)); err != nil ||
		string(content) != string(plan.Editor) {
		t.Fatalf("updated editor = %q, %v", content, err)
	}
}

func TestReadModulePathRejectsMissingDirective(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "go.mod")
	if err := os.WriteFile(name, []byte("go 1.26.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readModulePath(name); err == nil {
		t.Fatal("readModulePath(no module) error = nil")
	}
	if _, err := readModulePath(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("readModulePath(missing) error = nil")
	}
}

func documentationCatalog() catalog.Catalog {
	return catalog.Catalog{
		Schema: 1,
		Toolchains: catalog.Toolchains{
			Go: "1.26.5", Java: "25", GoLand: "2026.2",
		},
		Repositories: []catalog.Repository{{
			Name: "docs", Directory: "docs", Status: "active",
			CanonicalURL: "https://github.com/spice-framework/docs",
			CloneURL:     "https://github.com/spice-framework/docs.git",
			Artifact:     "documentation", Dependencies: []string{},
			Fast: []catalog.Invocation{}, Full: []catalog.Invocation{},
		}},
	}
}
