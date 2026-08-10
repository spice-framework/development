package agentextension

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spice-framework/development/internal/catalog"
	"github.com/spice-framework/development/internal/process"
)

const maximumFileBytes = 16 << 20

type modMetadata struct {
	Module    struct{ Path string }
	Go        string
	Toolchain string
	Require   []struct {
		Path     string
		Version  string
		Indirect bool
	}
	Replace []json.RawMessage
	Exclude []json.RawMessage
	Retract []json.RawMessage
	Ignore  []json.RawMessage
	Tool    []struct{ Path string }
}

func check(ctx context.Context, root string, ecosystem catalog.Catalog, runner process.Runner) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("Agent extension check context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(root) == "" {
		return Result{}, errors.New("Agent extension check root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Result{}, fmt.Errorf("resolve Agent extension root: %w", err)
	}
	root = filepath.Clean(absolute)
	if err := requireRealDirectory(root); err != nil {
		return Result{}, fmt.Errorf("inspect Agent extension root: %w", err)
	}
	if err := rejectLinks(ctx, root); err != nil {
		return Result{}, err
	}
	manifestContent, err := readBounded(filepath.Join(root, "spice-agent-extension.json"))
	if err != nil {
		return Result{}, err
	}
	manifest, err := ParseManifest(manifestContent)
	if err != nil {
		return Result{}, err
	}
	profile, found := ecosystem.AgentExtensionProfile(manifest.Profile)
	if !found {
		return Result{}, fmt.Errorf("Agent extension profile %q is not catalog-authorized", manifest.Profile)
	}
	if err := validateManifest(manifest, profile); err != nil {
		return Result{}, err
	}
	if runner == nil {
		return Result{}, errors.New("Agent extension check process runner is required")
	}
	if err := verifyModule(ctx, root, manifest, profile, runner); err != nil {
		return Result{}, err
	}
	if err := verifyCompatibility(root, profile); err != nil {
		return Result{}, err
	}
	if err := verifySums(root, profile); err != nil {
		return Result{}, err
	}
	if err := verifyLayout(root, manifest, profile); err != nil {
		return Result{}, err
	}
	return Result{Root: root, Files: requiredFiles(manifest, profile), Materialized: true}, nil
}

func validateManifest(value Manifest, profile catalog.AgentExtensionProfile) error {
	if value.Schema != ManifestSchema || value.Profile != profile.ID || value.Kind != profile.Kind ||
		value.Status != profile.Status || value.Activation != profile.Activation {
		return errors.New("Agent extension manifest schema/profile/kind/status/activation is stale")
	}
	if !safeModulePath(value.Module) || value.Manifest != (Symbol{Package: value.Module, Name: "Manifest"}) {
		return errors.New("Agent extension manifest module or public Manifest symbol is invalid")
	}
	if err := validateIdentity("tool name", value.ToolName); err != nil {
		return err
	}
	wantComposition := Composition{
		Target: profile.Composition.Target, Package: profile.Composition.Package,
		Generated: profile.Composition.Generated, OwnershipFile: profile.Composition.OwnershipFile,
	}
	if value.Composition != wantComposition {
		return errors.New("Agent extension composition layout is stale")
	}
	wantDocumentation := Documentation{
		Architecture: "ARCHITECTURE.md", Security: "SECURITY.md", Dependencies: "DEPENDENCIES.md",
		Compatibility: "docs/compatibility.md", Deletion: "docs/deletion.md",
		Verification: "docs/verification.md", Benchmarks: "benchmarks/README.md",
	}
	if value.Documentation != wantDocumentation {
		return errors.New("Agent extension documentation layout is stale")
	}
	if value.Prohibitions != (Prohibitions{}) {
		return errors.New("Agent extension manifest enables a prohibited behavior")
	}
	return nil
}

func verifyModule(
	ctx context.Context,
	root string,
	manifest Manifest,
	profile catalog.AgentExtensionProfile,
	runner process.Runner,
) error {
	output, err := runner.Run(ctx, root, "go", "mod", "edit", "-json")
	if err != nil {
		return fmt.Errorf("inspect Agent extension go.mod offline: %w", err)
	}
	var metadata modMetadata
	if err := decodeStrict([]byte(output), &metadata, "Agent extension go.mod"); err != nil {
		return err
	}
	if metadata.Module.Path != manifest.Module || metadata.Go != profile.GoDirective ||
		metadata.Toolchain != profile.GoToolchain {
		return fmt.Errorf("Agent extension go.mod identity/Go selection is stale")
	}
	if len(metadata.Replace) != 0 || len(metadata.Exclude) != 0 || len(metadata.Retract) != 0 ||
		len(metadata.Ignore) != 0 {
		return errors.New("Agent extension go.mod must not contain replace, exclude, retract, or ignore directives")
	}
	selected := make(map[string]string, len(metadata.Require))
	for _, required := range metadata.Require {
		if _, duplicate := selected[required.Path]; duplicate {
			return fmt.Errorf("Agent extension go.mod repeats requirement %s", required.Path)
		}
		selected[required.Path] = required.Version
	}
	for _, required := range profile.Modules {
		if selected[required.Path] != required.Version {
			return fmt.Errorf("Agent extension go.mod requires %s at %q; want %q", required.Path, selected[required.Path], required.Version)
		}
	}
	tools := make([]string, 0, len(metadata.Tool))
	for _, tool := range metadata.Tool {
		tools = append(tools, tool.Path)
	}
	if !slices.Equal(tools, profile.Tools) {
		return fmt.Errorf("Agent extension Go tools are %#v; want %#v", tools, profile.Tools)
	}
	return nil
}

func verifyCompatibility(root string, profile catalog.AgentExtensionProfile) error {
	content, err := readBounded(filepath.Join(root, "spice-compatibility.json"))
	if err != nil {
		return err
	}
	value, err := parseCompatibility(content)
	if err != nil {
		return err
	}
	agent := requireProfileModule(profile, "github.com/spice-framework/spice-agent")
	toolchain := requireProfileModule(profile, "github.com/spice-framework/toolchain")
	want := Compatibility{
		Schema:     profile.SpiceCompatibility.Schema,
		Minimum:    profile.SpiceCompatibility.MinimumSpice,
		Current:    profile.SpiceCompatibility.CurrentSpice,
		SpiceAgent: agent.Version, Toolchain: toolchain.Version, Go: profile.RuntimeGo,
	}
	if value != want {
		return fmt.Errorf("Spice compatibility metadata is %#v; want %#v", value, want)
	}
	return nil
}

func verifySums(root string, profile catalog.AgentExtensionProfile) error {
	content, err := readBounded(filepath.Join(root, "go.sum"))
	if err != nil {
		return err
	}
	lines := make(map[string]struct{})
	for line := range strings.SplitSeq(strings.TrimSpace(string(content)), "\n") {
		if _, duplicate := lines[line]; duplicate {
			return fmt.Errorf("go.sum repeats %q", line)
		}
		lines[line] = struct{}{}
	}
	for _, module := range profile.Modules {
		for _, line := range []string{
			module.Path + " " + module.Version + " " + module.Sum,
			module.Path + " " + module.Version + "/go.mod " + module.GoModSum,
		} {
			if _, found := lines[line]; !found {
				return fmt.Errorf("go.sum is missing catalog pin %q", line)
			}
		}
	}
	return nil
}

func verifyLayout(root string, manifest Manifest, profile catalog.AgentExtensionProfile) error {
	files := requiredFiles(manifest, profile)
	for _, name := range files {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return fmt.Errorf("Agent extension is not materialized: require %s: %w", name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("Agent extension required file %s is not regular", name)
		}
	}
	vendorContent, err := readBounded(filepath.Join(root, "vendor", "modules.txt"))
	if err != nil {
		return err
	}
	for _, module := range profile.Modules {
		header := "# " + module.Path + " " + module.Version
		if !containsLine(string(vendorContent), header) {
			return fmt.Errorf("vendor/modules.txt is missing exact selection %s", header)
		}
	}
	generated := filepath.Join(root, filepath.FromSlash(manifest.Composition.Generated))
	if !containsDirectGoFile(generated) || !containsGoFile(filepath.Join(generated, "sources")) {
		return errors.New("Agent extension generated proof or source maps are missing")
	}
	return nil
}

func requiredFiles(manifest Manifest, profile catalog.AgentExtensionProfile) []string {
	result := []string{
		".gitignore", "AGENTS.md", "ARCHITECTURE.md", "DEPENDENCIES.md", "LICENSE", "Makefile",
		"README.md", "SECURITY.md", "autoconfigure/autoconfigure.go",
		"autoconfigure/autoconfigure_test.go", "benchmarks/README.md", "docs/compatibility.md",
		"docs/deletion.md", "docs/verification.md", "go.mod", "go.sum",
		"internal/composition/application.go", "internal/composition/composition_test.go",
		"internal/composition/proof.go", "internal/qualitygate/main.go", "manifest.go",
		"spice-agent-extension.json", "spice-compatibility.json", "tool.go", "tool_test.go",
		"vendor/modules.txt", profile.Composition.OwnershipFile,
	}
	slices.Sort(result)
	return result
}

func rejectLinks(ctx context.Context, root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("Agent extension tree contains symbolic link or reparse point %q", path)
		}
		return nil
	})
}

func containsLine(content, want string) bool {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		if scanner.Text() == want {
			return true
		}
	}
	return false
}

func containsDirectGoFile(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func containsGoFile(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if info.Mode().IsRegular() {
				found = true
				return fs.SkipAll
			}
		}
		return nil
	})
	return found
}

func readBounded(path string) ([]byte, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maximumFileBytes {
		return nil, fmt.Errorf("%s is not a bounded regular file", path)
	}
	content, err := io.ReadAll(io.LimitReader(handle, maximumFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if len(content) > maximumFileBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", path, maximumFileBytes)
	}
	return content, nil
}
