// Package agentextension owns the pre-stable Spice Agent extension authoring
// scaffold. It renders source only; materialization and repository gates remain
// explicit extension-repository responsibilities.
package agentextension

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spice-framework/development/internal/catalog"
	"github.com/spice-framework/development/internal/process"
)

const (
	LegacyProfileID      = "compiled-tool-autoconfigure/v1alpha1-preview5"
	ProfileID            = "compiled-tool-autoconfigure/v1alpha1-preview6"
	ManifestSchema       = "spice.agent.extension/v1alpha1"
	maximumManifestBytes = 64 << 10
)

type InitOptions struct {
	Directory string
	Module    string
	ToolName  string
	Profile   string
}

type Result struct {
	Root         string
	Files        []string
	Materialized bool
}

type Manifest struct {
	Schema        string        `json:"schema"`
	Profile       string        `json:"profile"`
	Module        string        `json:"module"`
	Kind          string        `json:"kind"`
	Status        string        `json:"status"`
	Activation    string        `json:"activation"`
	ToolName      string        `json:"tool_name"`
	Manifest      Symbol        `json:"manifest"`
	Composition   Composition   `json:"composition"`
	Documentation Documentation `json:"documentation"`
	Prohibitions  Prohibitions  `json:"prohibitions"`
}

type Symbol struct {
	Package string `json:"package"`
	Name    string `json:"name"`
}

type Composition struct {
	Target        string `json:"target"`
	Package       string `json:"package"`
	Generated     string `json:"generated"`
	OwnershipFile string `json:"ownership_file"`
}

type Documentation struct {
	Architecture  string `json:"architecture"`
	Security      string `json:"security"`
	Dependencies  string `json:"dependencies"`
	Compatibility string `json:"compatibility"`
	Deletion      string `json:"deletion"`
	Verification  string `json:"verification"`
	Benchmarks    string `json:"benchmarks"`
}

type Prohibitions struct {
	CompiledRegistry  bool `json:"compiled_registry"`
	ReflectionLookup  bool `json:"reflection_lookup"`
	RuntimeScanning   bool `json:"runtime_scanning"`
	HiddenNetwork     bool `json:"hidden_network"`
	ReplaceDirectives bool `json:"replace_directives"`
}

type Compatibility struct {
	Schema     int    `json:"schema"`
	Minimum    string `json:"minimum"`
	Current    string `json:"current"`
	SpiceAgent string `json:"spice_agent"`
	Toolchain  string `json:"toolchain"`
	Go         string `json:"go"`
}

func Init(ctx context.Context, options InitOptions, ecosystem catalog.Catalog) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("Agent extension init context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	validated, profile, err := validateInitOptions(options, ecosystem)
	if err != nil {
		return Result{}, err
	}
	files, err := render(validated, profile)
	if err != nil {
		return Result{}, err
	}
	return apply(ctx, validated.Directory, files, nil)
}

func Check(ctx context.Context, root string, ecosystem catalog.Catalog, runner process.Runner) (Result, error) {
	return check(ctx, root, ecosystem, runner)
}

func ParseManifest(content []byte) (Manifest, error) {
	if len(content) > maximumManifestBytes {
		return Manifest{}, fmt.Errorf("Agent extension manifest exceeds %d bytes", maximumManifestBytes)
	}
	var result Manifest
	if err := decodeStrict(content, &result, "Agent extension manifest"); err != nil {
		return Manifest{}, err
	}
	return result, nil
}

func parseCompatibility(content []byte) (Compatibility, error) {
	var result Compatibility
	if err := decodeStrict(content, &result, "Spice compatibility metadata"); err != nil {
		return Compatibility{}, err
	}
	return result, nil
}

func decodeStrict(content []byte, destination any, label string) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode %s: %w", label, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%s has trailing JSON values", label)
		}
		return fmt.Errorf("decode trailing %s: %w", label, err)
	}
	return nil
}

func validateInitOptions(options InitOptions, ecosystem catalog.Catalog) (InitOptions, catalog.AgentExtensionProfile, error) {
	if strings.TrimSpace(options.Directory) == "" {
		return InitOptions{}, catalog.AgentExtensionProfile{}, errors.New("Agent extension directory is required")
	}
	absolute, err := filepath.Abs(options.Directory)
	if err != nil {
		return InitOptions{}, catalog.AgentExtensionProfile{}, fmt.Errorf("resolve Agent extension directory: %w", err)
	}
	options.Directory = filepath.Clean(absolute)
	if !safeModulePath(options.Module) {
		return InitOptions{}, catalog.AgentExtensionProfile{}, fmt.Errorf("Agent extension module %q is unsafe", options.Module)
	}
	if err := validateIdentity("tool name", options.ToolName); err != nil {
		return InitOptions{}, catalog.AgentExtensionProfile{}, err
	}
	if options.Profile == "" {
		return InitOptions{}, catalog.AgentExtensionProfile{}, errors.New("Agent extension profile is required")
	}
	profile, found := ecosystem.AgentExtensionProfile(options.Profile)
	if !found {
		return InitOptions{}, catalog.AgentExtensionProfile{}, fmt.Errorf("Agent extension profile %q is not catalog-authorized", options.Profile)
	}
	return options, profile, nil
}

func safeModulePath(value string) bool {
	if value == "" || value != path.Clean(value) || strings.Contains(value, "\\") ||
		strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") ||
		strings.HasPrefix(value, "../") || !strings.Contains(strings.Split(value, "/")[0], ".") {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '/' && character != '.' &&
			character != '-' && character != '_' && character != '~' {
			return false
		}
	}
	return true
}

func validateIdentity(label, value string) error {
	if value == "" || value != strings.TrimSpace(value) {
		return fmt.Errorf("%s must be non-empty without surrounding whitespace", label)
	}
	if len(value) > 128 || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n\t") {
		return fmt.Errorf("%s contains invalid or oversized text", label)
	}
	return nil
}
