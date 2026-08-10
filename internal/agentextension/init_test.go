package agentextension

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/spice-framework/development/internal/catalog"
)

func TestInitMatchesReviewedGolden(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "extension")
	result, err := Init(t.Context(), InitOptions{
		Directory: root, Module: "example.com/acme/agent-tool", ToolName: "acme.inspect", Profile: ProfileID,
	}, testCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	var actual strings.Builder
	for _, name := range result.Files {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		actual.WriteString(hex.EncodeToString(digest[:]) + "  " + name + "\n")
	}
	want, err := os.ReadFile(filepath.Join("testdata", "golden-files.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if actual.String() != string(want) {
		t.Fatalf("scaffold golden drift:\n%s", actual.String())
	}
}

func TestInitCreatesDeterministicSourceOnlyScaffold(t *testing.T) {
	t.Parallel()
	ecosystem := testCatalog(t)
	parent := t.TempDir()
	first := filepath.Join(parent, "first")
	second := filepath.Join(parent, "second")
	options := InitOptions{
		Module: "example.com/acme/agent-tool", ToolName: "acme.inspect", Profile: ProfileID,
	}
	options.Directory = first
	firstResult, err := Init(t.Context(), options, ecosystem)
	if err != nil {
		t.Fatal(err)
	}
	options.Directory = second
	secondResult, err := Init(t.Context(), options, ecosystem)
	if err != nil {
		t.Fatal(err)
	}
	if firstResult.Materialized || secondResult.Materialized || !slices.Equal(firstResult.Files, secondResult.Files) {
		t.Fatalf("Init() results = %#v, %#v", firstResult, secondResult)
	}
	for _, name := range firstResult.Files {
		left, readErr := os.ReadFile(filepath.Join(first, filepath.FromSlash(name)))
		if readErr != nil {
			t.Fatal(readErr)
		}
		right, readErr := os.ReadFile(filepath.Join(second, filepath.FromSlash(name)))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !reflect.DeepEqual(left, right) || strings.Contains(string(left), parent) ||
			strings.Contains(string(left), "{{MODULE}}") || strings.Contains(string(left), "{{PROFILE}}") ||
			strings.Contains(string(left), "{{TOOL_NAME}}") || strings.Contains(string(left), "\r\n") {
			t.Fatalf("scaffold file %s is nondeterministic or unrendered", name)
		}
	}
	if _, err := os.Stat(filepath.Join(first, "vendor")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source scaffold unexpectedly bundled vendor: %v", err)
	}
	if _, err := os.Stat(filepath.Join(first, "internal", "spicegen")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source scaffold unexpectedly copied generated output: %v", err)
	}
	readme, err := os.ReadFile(filepath.Join(first, "README.md"))
	if err != nil || !strings.Contains(string(readme), "not publishable yet") ||
		!strings.Contains(string(readme), "non-distributed") ||
		!strings.Contains(string(readme), "clean-room public-authoring") ||
		strings.Contains(string(readme), "external-author") {
		t.Fatalf("README boundary = %q, %v", readme, err)
	}
	moduleFile := string(mustRead(t, filepath.Join(first, "go.mod")))
	sumFile := string(mustRead(t, filepath.Join(first, "go.sum")))
	for _, want := range []string{"github.com/spice-framework/spice-agent v0.1.0-preview.6", "github.com/spice-framework/toolchain v0.1.0-preview.2"} {
		if !strings.Contains(moduleFile, want) {
			t.Fatalf("current go.mod missing %q: %s", want, moduleFile)
		}
	}
	for _, want := range []string{"h1:XJKJge+xWP/FLNoL1/rXq8z8tdu/5iEkKfmu1dTgFms=", "h1:Hv/Ur+Uc3cG00jVCo/R5zINZ1w33jH0O6/ekeNOrFyk="} {
		if !strings.Contains(sumFile, want) {
			t.Fatalf("current go.sum missing %q: %s", want, sumFile)
		}
	}
}

func TestInitPreservesImmutableLegacyProfile(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "legacy")
	_, err := Init(t.Context(), InitOptions{
		Directory: root, Module: "example.com/acme/legacy-tool", ToolName: "acme.legacy", Profile: LegacyProfileID,
	}, testCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	moduleFile := string(mustRead(t, filepath.Join(root, "go.mod")))
	sumFile := string(mustRead(t, filepath.Join(root, "go.sum")))
	for _, want := range []string{
		"github.com/spice-framework/spice-agent v0.1.0-preview.5",
		"github.com/spice-framework/toolchain v0.1.0-preview.1.0.20260806203056-d0b9ac086bd6",
		"h1:rGND9DYx3pssliD1tZQOvPDOZ5GVfQLDc7VJQI3HLOM=",
		"h1:paTYw/o/6OsbNAvOWvjicOOqWyyt2Nd3vWdoPq8+BjA=",
	} {
		if !strings.Contains(moduleFile+sumFile, want) {
			t.Fatalf("legacy scaffold missing %q", want)
		}
	}
}

func TestInitRejectsUnsafeOrIncompleteRequests(t *testing.T) {
	t.Parallel()
	ecosystem := testCatalog(t)
	valid := InitOptions{
		Directory: filepath.Join(t.TempDir(), "extension"), Module: "example.com/acme/tool",
		ToolName: "acme.inspect", Profile: ProfileID,
	}
	for name, mutate := range map[string]func(*InitOptions){
		"directory":             func(value *InitOptions) { value.Directory = "" },
		"module missing domain": func(value *InitOptions) { value.Module = "local/tool" },
		"module escape":         func(value *InitOptions) { value.Module = "../tool" },
		"module slash":          func(value *InitOptions) { value.Module = "example.com\\tool" },
		"tool name":             func(value *InitOptions) { value.ToolName = " bad" },
		"profile missing":       func(value *InitOptions) { value.Profile = "" },
		"profile unknown":       func(value *InitOptions) { value.Profile = "compiled-tool/latest" },
	} {
		t.Run(name, func(t *testing.T) {
			options := valid
			mutate(&options)
			if _, err := Init(t.Context(), options, ecosystem); err == nil {
				t.Fatal("Init() error = nil")
			}
		})
	}
	if _, err := Init(nil, valid, ecosystem); err == nil { //nolint:staticcheck // Fail-closed nil boundary.
		t.Fatal("Init(nil) error = nil")
	}
}

func TestInitRefusesExistingContentAndRollsBack(t *testing.T) {
	t.Parallel()
	ecosystem := testCatalog(t)
	root := filepath.Join(t.TempDir(), "extension")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "owned.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	options := InitOptions{Directory: root, Module: "example.com/acme/tool", ToolName: "inspect", Profile: ProfileID}
	if _, err := Init(t.Context(), options, ecosystem); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("Init(nonempty) error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "owned.txt"))
	if err != nil || string(content) != "keep" {
		t.Fatalf("existing content = %q, %v", content, err)
	}

	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.Mkdir(empty, 0o750); err != nil {
		t.Fatal(err)
	}
	files := []plannedFile{
		{name: "a.txt", content: []byte("a"), mode: 0o600},
		{name: "nested/b.txt", content: []byte("b"), mode: 0o600},
	}
	want := errors.New("injected write failure")
	_, err = apply(t.Context(), empty, files, func(name string) error {
		if name == "nested/b.txt" {
			return want
		}
		return nil
	})
	if !errors.Is(err, want) {
		t.Fatalf("apply() error = %v", err)
	}
	entries, err := os.ReadDir(empty)
	if err != nil || len(entries) != 0 {
		t.Fatalf("rollback left %#v, %v", entries, err)
	}
}

func TestInitJoinsCancellationWithoutPartialCommit(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	root := filepath.Join(parent, "cancelled")
	ctx, cancel := context.WithCancel(t.Context())
	files := []plannedFile{{name: "a.txt", content: []byte("a"), mode: 0o600}, {name: "b.txt", content: []byte("b"), mode: 0o600}}
	_, err := apply(ctx, root, files, func(name string) error {
		if name == "b.txt" {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("apply cancellation error = %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled target exists: %v", err)
	}
}

func TestInitRejectsSymlinkedParent(t *testing.T) {
	parent := t.TempDir()
	real := filepath.Join(parent, "real")
	link := filepath.Join(parent, "link")
	if err := os.Mkdir(real, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	options := InitOptions{
		Directory: filepath.Join(link, "extension"), Module: "example.com/acme/tool",
		ToolName: "inspect", Profile: ProfileID,
	}
	if _, err := Init(t.Context(), options, testCatalog(t)); err == nil ||
		(!strings.Contains(err.Error(), "symbolic link") && !strings.Contains(err.Error(), "real directory")) {
		t.Fatalf("Init(symlink) error = %v", err)
	}
}

func testCatalog(t *testing.T) catalog.Catalog {
	t.Helper()
	value, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	return value
}
