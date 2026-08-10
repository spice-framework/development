package agentextension

import (
	"encoding/json"
	"fmt"
	"go/format"
	"slices"
	"strings"

	"github.com/spice-framework/development/internal/catalog"
)

type plannedFile struct {
	name    string
	content []byte
	mode    uint32
}

func render(options InitOptions, profile catalog.AgentExtensionProfile) ([]plannedFile, error) {
	spice := requireProfileModule(profile, "github.com/spice-framework/spice")
	agent := requireProfileModule(profile, "github.com/spice-framework/spice-agent")
	toolchain := requireProfileModule(profile, "github.com/spice-framework/toolchain")
	replacer := strings.NewReplacer(
		"{{MODULE}}", options.Module,
		"{{TOOL_NAME}}", options.ToolName,
		"{{PROFILE}}", profile.ID,
		"{{SPICE_VERSION}}", spice.Version,
		"{{SPICE_SUM}}", spice.Sum,
		"{{SPICE_GO_MOD_SUM}}", spice.GoModSum,
		"{{AGENT_VERSION}}", agent.Version,
		"{{AGENT_SUM}}", agent.Sum,
		"{{AGENT_GO_MOD_SUM}}", agent.GoModSum,
		"{{TOOLCHAIN_VERSION}}", toolchain.Version,
		"{{TOOLCHAIN_SUM}}", toolchain.Sum,
		"{{TOOLCHAIN_GO_MOD_SUM}}", toolchain.GoModSum,
	)
	manifest := Manifest{
		Schema: ManifestSchema, Profile: profile.ID, Module: options.Module,
		Kind: profile.Kind, Status: profile.Status, Activation: profile.Activation,
		ToolName: options.ToolName,
		Manifest: Symbol{Package: options.Module, Name: "Manifest"},
		Composition: Composition{
			Target: profile.Composition.Target, Package: profile.Composition.Package,
			Generated: profile.Composition.Generated, OwnershipFile: profile.Composition.OwnershipFile,
		},
		Documentation: Documentation{
			Architecture: "ARCHITECTURE.md", Security: "SECURITY.md", Dependencies: "DEPENDENCIES.md",
			Compatibility: "docs/compatibility.md", Deletion: "docs/deletion.md",
			Verification: "docs/verification.md", Benchmarks: "benchmarks/README.md",
		},
	}
	manifestContent, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render Agent extension manifest: %w", err)
	}
	manifestContent = append(manifestContent, '\n')
	compatibility := Compatibility{
		Schema:     profile.SpiceCompatibility.Schema,
		Minimum:    profile.SpiceCompatibility.MinimumSpice,
		Current:    profile.SpiceCompatibility.CurrentSpice,
		SpiceAgent: agent.Version, Toolchain: toolchain.Version, Go: profile.RuntimeGo,
	}
	compatibilityContent, err := json.MarshalIndent(compatibility, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render Spice compatibility metadata: %w", err)
	}
	compatibilityContent = append(compatibilityContent, '\n')

	files := []plannedFile{
		{name: ".gitignore", content: []byte("/coverage.out\n"), mode: 0o600},
		{name: "LICENSE", content: []byte(apacheLicense), mode: 0o600},
		{name: "spice-agent-extension.json", content: manifestContent, mode: 0o600},
		{name: "spice-compatibility.json", content: compatibilityContent, mode: 0o600},
	}
	for name, template := range scaffoldTemplates {
		content := []byte(replacer.Replace(template))
		if strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".go.pending") {
			content, err = format.Source(content)
			if err != nil {
				return nil, fmt.Errorf("format scaffold %s: %w", name, err)
			}
		}
		files = append(files, plannedFile{name: name, content: content, mode: 0o600})
	}
	slices.SortFunc(files, func(left, right plannedFile) int { return strings.Compare(left.name, right.name) })
	return files, nil
}

func requireProfileModule(profile catalog.AgentExtensionProfile, modulePath string) catalog.AgentExtensionModule {
	for _, module := range profile.Modules {
		if module.Path == modulePath {
			return module
		}
	}
	panic("validated Agent extension profile is missing " + modulePath)
}

var scaffoldTemplates = map[string]string{
	"AGENTS.md": `# Extension implementation contract

This separately versioned module is an experimental Spice Agent extension.
Keep source valid Go, composition explicit, and generated output committed.
Never add a replace directive, hidden network access, runtime scanning,
reflection-based lookup, or a mutable compiled registry. Run make verify before
committing and rerun make materialize only as an explicit reviewed dependency
or generation operation.
`,
	"ARCHITECTURE.md": `# Architecture

This module contributes one ordinary tool.Tool through explicit blank-imported
/autoconfigure. Spice owns the static graph and generates direct Go calls.
The extension owns no container or runtime registry. internal/composition is an
executable construction proof; internal/spicegen and .spice are Spice-owned.
`,
	"DEPENDENCIES.md": `# Dependency review

The initial profile pins Spice {{SPICE_VERSION}}, Spice Agent {{AGENT_VERSION}},
and Toolchain {{TOOLCHAIN_VERSION}}. Their exact checksums are catalog-owned and
recorded in go.sum after materialization. No runtime network dependency is
introduced by the example tool. Review licenses, maintenance, security,
cancellation, observability, and vendor changes before changing any pin.
`,
	"SECURITY.md": `# Security

The example tool is read-only, replay-safe, filesystem-neutral, and declares no
capability. It receives model-provided JSON and must keep input and output
bounded. Capability metadata is descriptive and is not a sandbox. Never place
credentials, paths, free-form failures, or private data in durable metadata.
Report security concerns privately to the Spice Framework maintainers.
`,
	"README.md": `# {{MODULE}}

This is a pre-stable compiled Spice Agent tool extension created from profile
{{PROFILE}}. Import defaults explicitly:

` + "```go" + `
import _ "{{MODULE}}/autoconfigure"
` + "```" + `

The source scaffold is intentionally not publishable yet. It contains no
bundled vendor tree or copied generated output. Run make materialize as an
explicit network-capable action, review and commit vendor plus generated files,
then run make verify and spice-dev agent-extension check --root . offline.
Development's spice-dev utility is currently source-run and non-distributed;
this scaffold does not count as clean-room public-authoring acceptance.
`,
	"Makefile": `.PHONY: materialize fast check verify benchmark

export GOWORK := off
export GOTOOLCHAIN := local

materialize:
	go run ./internal/qualitygate -mode=materialize

fast:
	go run ./internal/qualitygate -mode=fast

check:
	go run ./internal/qualitygate -mode=check

verify:
	go run ./internal/qualitygate -mode=verify

benchmark:
	go run ./internal/qualitygate -mode=benchmark
`,
	"go.mod": `module {{MODULE}}

go 1.26.0

toolchain go1.26.5

tool (
	github.com/spice-framework/toolchain/cmd/spice
	github.com/spice-framework/toolchain/cmd/spice-annotation-core
)

require (
	github.com/spice-framework/spice {{SPICE_VERSION}}
	github.com/spice-framework/spice-agent {{AGENT_VERSION}}
)

require github.com/spice-framework/toolchain {{TOOLCHAIN_VERSION}} // indirect
`,
	"go.sum": `github.com/spice-framework/spice {{SPICE_VERSION}} {{SPICE_SUM}}
github.com/spice-framework/spice {{SPICE_VERSION}}/go.mod {{SPICE_GO_MOD_SUM}}
github.com/spice-framework/spice-agent {{AGENT_VERSION}} {{AGENT_SUM}}
github.com/spice-framework/spice-agent {{AGENT_VERSION}}/go.mod {{AGENT_GO_MOD_SUM}}
github.com/spice-framework/toolchain {{TOOLCHAIN_VERSION}} {{TOOLCHAIN_SUM}}
github.com/spice-framework/toolchain {{TOOLCHAIN_VERSION}}/go.mod {{TOOLCHAIN_GO_MOD_SUM}}
`,
	"manifest.go": `package extension

import spicestarter "github.com/spice-framework/spice/annotation/sdk/starter"

// Manifest returns exact compatibility and review metadata.
func Manifest() spicestarter.Manifest {
	return spicestarter.Must(spicestarter.Spec{
		Schema: spicestarter.Schema,
		ID: "{{MODULE}}", Version: "0.1.0-dev", Module: "{{MODULE}}",
		SpiceAPI: spicestarter.APIVersion, MinimumGo: "1.26.5",
		License: "Apache-2.0", Review: "DEPENDENCIES.md",
		Capabilities: []string{"agent.tool.read-only"},
		Activation: spicestarter.Activation{
			Mode: spicestarter.ActivationExplicitConstructor,
			EntryPoints: []spicestarter.EntryPoint{{Package: "{{MODULE}}", Symbol: "New"}},
		},
		Dependencies: []spicestarter.Dependency{{
			Module: "github.com/spice-framework/spice-agent",
			Version: "{{AGENT_VERSION}}", License: "Apache-2.0",
		}},
	})
}
`,
	"manifest_test.go": `package extension

import "testing"

func TestManifestPinsPublicContract(t *testing.T) {
	t.Parallel()
	spec := Manifest().Spec()
	if spec.ID != "{{MODULE}}" || spec.Version != "0.1.0-dev" || spec.Module != "{{MODULE}}" || spec.MinimumGo != "1.26.5" || spec.License != "Apache-2.0" {
		t.Fatalf("Manifest() = %#v", spec)
	}
}
`,
	"tool.go": `package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spice-framework/spice-agent/tool"
)

type inspectTool struct{ definition tool.Definition }

// New constructs the extension's explicit tool implementation.
func New() (tool.Tool, error) {
	definition, err := tool.NewDefinition(
		"{{TOOL_NAME}}", "Echo one bounded value for extension authoring proof.",
		json.RawMessage(` + "`" + `{"type":"object","properties":{"value":{"type":"string","maxLength":4096}},"required":["value"],"additionalProperties":false}` + "`" + `),
		tool.EffectReadOnly, tool.ReplaySafe,
	)
	if err != nil { return nil, fmt.Errorf("construct tool definition: %w", err) }
	return &inspectTool{definition: definition}, nil
}

func (value *inspectTool) Definition() tool.Definition { return value.definition.Clone() }

func (value *inspectTool) Execute(ctx context.Context, call tool.Call, _ tool.Reporter) (tool.Result, error) {
	if err := ctx.Err(); err != nil { return tool.Result{}, executionFailure(call.ID(), err) }
	var input struct { Value string ` + "`json:\"value\"`" + ` }
	decoder := json.NewDecoder(bytes.NewReader(call.Arguments()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.Value == "" || len(input.Value) > 4096 {
		return tool.NewErrorResult(call.ID(), json.RawMessage(` + "`" + `{"ok":false}` + "`" + `), "invalid arguments")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return tool.NewErrorResult(call.ID(), json.RawMessage(` + "`" + `{"ok":false}` + "`" + `), "invalid arguments")
	}
	content, err := json.Marshal(struct { Value string ` + "`json:\"value\"`" + ` }{Value: input.Value})
	if err != nil { return tool.Result{}, fmt.Errorf("encode result: %w", err) }
	return tool.NewResult(call.ID(), content)
}

func executionFailure(callID tool.CallID, cause error) error {
	failure, err := tool.NewExecutionError(callID, tool.ExecutionDefinitive, tool.RetryNever, cause)
	if err != nil { return errors.Join(cause, err) }
	return failure
}
`,
	"tool_test.go": `package extension

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/spice-framework/spice-agent/tool"
)

func TestToolContract(t *testing.T) {
	t.Parallel()
	implementation, err := New()
	if err != nil { t.Fatal(err) }
	definition := implementation.Definition()
	if definition.Name() != "{{TOOL_NAME}}" || definition.Effect() != tool.EffectReadOnly || definition.ReplaySafety() != tool.ReplaySafe { t.Fatalf("definition = %#v", definition) }
	call, err := tool.NewCall("call-1", definition.Name(), json.RawMessage(` + "`" + `{"value":"hello"}` + "`" + `))
	if err != nil { t.Fatal(err) }
	result, err := implementation.Execute(t.Context(), call, nil)
	if err != nil || result.CallID() != call.ID() { t.Fatalf("Execute() = %#v, %v", result, err) }
	mutated := definition.InputSchema(); mutated[0] = '['
	if implementation.Definition().Validate() != nil { t.Fatal("definition was not defensively copied") }
}

func TestToolRejectsInvalidAndPreservesPriorCancellation(t *testing.T) {
	t.Parallel()
	implementation, err := New()
	if err != nil { t.Fatal(err) }
	call, err := tool.NewCall("call-2", "{{TOOL_NAME}}", json.RawMessage(` + "`" + `{"unknown":true}` + "`" + `))
	if err != nil { t.Fatal(err) }
	result, err := implementation.Execute(t.Context(), call, nil)
	if problem, found := result.Problem(); err != nil || !found || problem != "invalid arguments" { t.Fatalf("invalid Execute() = %#v, %v", result, err) }
	ctx, cancel := context.WithCancel(t.Context()); cancel()
	_, err = implementation.Execute(ctx, call, nil)
	var failure *tool.ExecutionError
	if !errors.Is(err, context.Canceled) || !errors.As(err, &failure) || failure.CallID() != call.ID() { t.Fatalf("cancel error = %T %v", err, err) }
	if err = executionFailure("", context.Canceled); !errors.Is(err, context.Canceled) { t.Fatalf("invalid correlation failure = %v", err) }
}

func FuzzToolArguments(f *testing.F) {
	f.Add([]byte(` + "`" + `{"value":"seed"}` + "`" + `))
	implementation, err := New()
	if err != nil { f.Fatal(err) }
	f.Fuzz(func(t *testing.T, content []byte) {
		if !json.Valid(content) { return }
		call, err := tool.NewCall("fuzz", "{{TOOL_NAME}}", content)
		if err != nil { return }
		result, executionErr := implementation.Execute(t.Context(), call, nil)
		if executionErr == nil && !result.IsZero() && result.CallID() != call.ID() { t.Fatal("result correlation changed") }
	})
}

func BenchmarkTool(b *testing.B) {
	implementation, err := New()
	if err != nil { b.Fatal(err) }
	call, err := tool.NewCall("benchmark", "{{TOOL_NAME}}", json.RawMessage(` + "`" + `{"value":"benchmark"}` + "`" + `))
	if err != nil { b.Fatal(err) }
	b.ReportAllocs()
	for range b.N {
		result, err := implementation.Execute(context.Background(), call, nil)
		if err != nil || result.CallID() != call.ID() { b.Fatalf("Execute() = %#v, %v", result, err) }
	}
}
`,
	"autoconfigure/autoconfigure.go": `// Package autoconfigure contributes the fallback tool only through an explicit blank import.
package autoconfigure

import (
	extension "{{MODULE}}"
	"github.com/spice-framework/spice-agent/tool"
	"github.com/spice-framework/spice/starter"
)

func DefaultTool() (tool.Tool, error) { return extension.New() }

// SpiceAutoConfiguration is statically decoded and is never executed during analysis.
func SpiceAutoConfiguration() starter.AutoConfiguration {
	return starter.AutoConfiguration{
		Review: "DEPENDENCIES.md",
		Beans: []starter.AutoBean{{Factory: DefaultTool, Name: "{{TOOL_NAME}}", Fallback: true}},
	}
}
`,
	"autoconfigure/autoconfigure_test.go": `package autoconfigure

import "testing"

func TestAutoConfigurationIsExplicitFallback(t *testing.T) {
	t.Parallel()
	configuration := SpiceAutoConfiguration()
	if len(configuration.Beans) != 1 || configuration.Beans[0].Name != "{{TOOL_NAME}}" || !configuration.Beans[0].Fallback { t.Fatalf("configuration = %#v", configuration) }
	implementation, err := DefaultTool()
	if err != nil || implementation.Definition().Name() != "{{TOOL_NAME}}" { t.Fatalf("DefaultTool() = %#v, %v", implementation, err) }
}
`,
	"internal/composition/application.go": `package composition

import _ "{{MODULE}}/autoconfigure"

// @import { Application } from "github.com/spice-framework/spice/annotation/core"

// ExtensionProof is compile-time metadata and is never executed.
//
// @Application
func ExtensionProof(*Proof) { panic("Spice must never execute an application marker") }
`,
	"internal/composition/proof.go": `package composition

import "github.com/spice-framework/spice-agent/tool"

// @import { Bean } from "github.com/spice-framework/spice/annotation/core"

type Proof struct { tools map[string]tool.Tool }

// @Bean(name="proof")
func NewProof(tools map[string]tool.Tool) *Proof {
	copy := make(map[string]tool.Tool, len(tools)); for name, value := range tools { copy[name] = value }; return &Proof{tools: copy}
}

func (value *Proof) HasTool(name string) bool { _, found := value.tools[name]; return found }
`,
	"internal/composition/composition_test.go.pending": `package composition_test

import (
	"context"
	"testing"

	spicegen "{{MODULE}}/internal/spicegen/extensionproof"
)

func TestGeneratedApplicationInjectsExtension(t *testing.T) {
	application, err := spicegen.NewApplication(t.Context())
	if err != nil { t.Fatal(err) }
	if !application.Components().Proof.HasTool("{{TOOL_NAME}}") { t.Fatal("generated tool map is missing the extension") }
	if err := application.Stop(context.Background()); err != nil { t.Fatal(err) }
}
`,
	"benchmarks/README.md": `# Provisional benchmarks

Run make benchmark with Go 1.26.5. Record platform, CPU, commit, command, ns/op,
B/op, and allocs/op. Five fixed 500-iteration samples at CPU=1 are evidence,
not stable thresholds. Investigate material regressions before adding policy.
`,
	"docs/compatibility.md": `# Compatibility

This module records {{PROFILE}}, Spice {{SPICE_VERSION}}, Agent
{{AGENT_VERSION}}, Toolchain {{TOOLCHAIN_VERSION}}, and Go 1.26.5. The profile
and authoring manifest are v1alpha1. A future profile is an explicit migration;
checks never silently rewrite an existing module to a newer profile.
`,
	"docs/deletion.md": `# Deletion

Remove the application's blank import and dependency, regenerate its Spice
targets, then delete this repository. No runtime registry, persisted core state,
or hidden activation remains. Removing this scaffold has no Agent kernel effect.
`,
	"docs/verification.md": `# Verification

make materialize is the only network-capable repository operation: it resolves
the exact graph, commits vendor, and generates the construction proof. Review
that diff. make fast/check/verify and spice-dev agent-extension check are
offline. The Development utility is pre-stable and currently source-run, not a
published tool dependency. Clean-room public-authoring proof remains pending.
`,
	"internal/qualitygate/main.go": qualityGateTemplate,
}

const qualityGateTemplate = `package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	mode := flag.String("mode", "verify", "materialize, fast, check, verify, or benchmark")
	flag.Parse()
	if flag.NArg() != 0 { fail(fmt.Errorf("quality gate accepts no positional arguments")) }
	if err := run(context.Background(), *mode); err != nil { fail(err) }
}

func run(ctx context.Context, mode string) error {
	if mode == "materialize" {
		for _, command := range [][]string{{"go", "mod", "tidy"}, {"go", "tool", "github.com/spice-framework/toolchain/cmd/spice", "generate", "--target", "ExtensionProof", "./..."}} {
			if err := execute(ctx, true, command...); err != nil { return err }
		}
		if err := activateAcceptance(); err != nil { return err }
		for _, command := range [][]string{{"go", "mod", "tidy"}, {"go", "mod", "vendor"}} {
			if err := execute(ctx, true, command...); err != nil { return err }
		}
		return nil
	}
	if mode == "benchmark" { return execute(ctx, false, "go", "test", "-run=^$", "-bench=.", "-benchmem", "-benchtime=500x", "-count=5", "-cpu=1", "./...") }
	if mode != "fast" && mode != "check" && mode != "verify" { return fmt.Errorf("unsupported quality mode %q", mode) }
	if err := execute(ctx, false, "go", "test", "-shuffle=on", "-count=1", "./..."); err != nil { return err }
	if mode == "fast" { return nil }
	commands := [][]string{
		{"go", "mod", "tidy", "-diff"},
		{"go", "tool", "github.com/spice-framework/toolchain/cmd/spice", "generate", "--check", "--target", "ExtensionProof", "./..."},
		{"go", "vet", "./..."},
	}
	for _, command := range commands { if err := execute(ctx, false, command...); err != nil { return err } }
	if mode == "check" { return nil }
	if err := execute(ctx, false, "go", "test", "-race", "-shuffle=on", "-count=1", "./..."); err != nil { return err }
	if err := coverage(ctx); err != nil { return err }
	return execute(ctx, false, "go", "test", "-run=^$", "-fuzz=FuzzToolArguments", "-fuzztime=100x", ".")
}

func activateAcceptance() error {
	pending := filepath.FromSlash("internal/composition/composition_test.go.pending")
	active := filepath.FromSlash("internal/composition/composition_test.go")
	_, pendingErr := os.Stat(pending)
	_, activeErr := os.Stat(active)
	if pendingErr == nil && os.IsNotExist(activeErr) {
		if err := os.Rename(pending, active); err != nil { return fmt.Errorf("activate generated composition acceptance: %w", err) }
		return nil
	}
	if os.IsNotExist(pendingErr) && activeErr == nil { return nil }
	return fmt.Errorf("composition acceptance activation is inconsistent: pending=%v active=%v", pendingErr, activeErr)
}

func coverage(ctx context.Context) error {
	directory, err := os.MkdirTemp("", "spice-agent-extension-coverage-*")
	if err != nil { return err }
	defer os.RemoveAll(directory)
	profile := filepath.Join(directory, "coverage.out")
	if err := execute(ctx, false, "go", "test", "-covermode=atomic", "-coverprofile="+profile, ".", "./autoconfigure", "./internal/composition"); err != nil { return err }
	output, err := capture(ctx, false, "go", "tool", "cover", "-func="+profile)
	if err != nil { return err }
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "total:") { continue }
		fields := strings.Fields(line)
		if len(fields) == 0 { break }
		value, parseErr := strconv.ParseFloat(strings.TrimSuffix(fields[len(fields)-1], "%"), 64)
		if parseErr != nil { return fmt.Errorf("parse coverage total: %w", parseErr) }
		if value < 85 { return fmt.Errorf("handwritten product coverage %.1f%% is below 85.0%%", value) }
		return nil
	}
	return fmt.Errorf("coverage output has no total")
}

func execute(ctx context.Context, network bool, arguments ...string) error {
	command := exec.CommandContext(ctx, arguments[0], arguments[1:]...)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	command.Env = isolatedEnvironment(network)
	if err := command.Run(); err != nil { return fmt.Errorf("run %s: %w", strings.Join(arguments, " "), err) }
	return nil
}

func capture(ctx context.Context, network bool, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, arguments[0], arguments[1:]...)
	command.Env = isolatedEnvironment(network)
	output, err := command.CombinedOutput()
	if err != nil { return "", fmt.Errorf("run %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(string(output))) }
	return string(output), nil
}

func isolatedEnvironment(network bool) []string {
	result := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) { case "GOWORK", "GOTOOLCHAIN", "GOPROXY", "GOFLAGS": continue }
		result = append(result, entry)
	}
	result = append(result, "GOWORK=off", "GOTOOLCHAIN=local")
	if network { return append(result, "GOFLAGS=") }
	return append(result, "GOPROXY=off", "GOFLAGS=-mod=vendor")
}

func fail(err error) { _, _ = fmt.Fprintln(os.Stderr, err); os.Exit(1) }
`

const apacheLicense = `                                 Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

   TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION

   1. Definitions.

      "License" shall mean the terms and conditions for use, reproduction,
      and distribution as defined by Sections 1 through 9 of this document.

      "Licensor" shall mean the copyright owner or entity authorized by
      the copyright owner that is granting the License.

      "Legal Entity" shall mean the union of the acting entity and all
      other entities that control, are controlled by, or are under common
      control with that entity. For the purposes of this definition,
      "control" means (i) the power, direct or indirect, to cause the
      direction or management of such entity, whether by contract or
      otherwise, or (ii) ownership of fifty percent (50%) or more of the
      outstanding shares, or (iii) beneficial ownership of such entity.

      "You" (or "Your") shall mean an individual or Legal Entity
      exercising permissions granted by this License.

      "Source" form shall mean the preferred form for making modifications,
      including but not limited to software source code, documentation
      source, and configuration files.

      "Object" form shall mean any form resulting from mechanical
      transformation or translation of a Source form, including but
      not limited to compiled object code, generated documentation,
      and conversions to other media types.

      "Work" shall mean the work of authorship, whether in Source or
      Object form, made available under the License, as indicated by a
      copyright notice that is included in or attached to the work
      (an example is provided in the Appendix below).

      "Derivative Works" shall mean any work, whether in Source or Object
      form, that is based on (or derived from) the Work and for which the
      editorial revisions, annotations, elaborations, or other modifications
      represent, as a whole, an original work of authorship. For the purposes
      of this License, Derivative Works shall not include works that remain
      separable from, or merely link (or bind by name) to the interfaces of,
      the Work and Derivative Works thereof.

      "Contribution" shall mean any work of authorship, including
      the original version of the Work and any modifications or additions
      to that Work or Derivative Works thereof, that is intentionally
      submitted to Licensor for inclusion in the Work by the copyright owner
      or by an individual or Legal Entity authorized to submit on behalf of
      the copyright owner. For the purposes of this definition, "submitted"
      means any form of electronic, verbal, or written communication sent
      to the Licensor or its representatives, including but not limited to
      communication on electronic mailing lists, source code control systems,
      and issue tracking systems that are managed by, or on behalf of, the
      Licensor for the purpose of discussing and improving the Work, but
      excluding communication that is conspicuously marked or otherwise
      designated in writing by the copyright owner as "Not a Contribution."

      "Contributor" shall mean Licensor and any individual or Legal Entity
      on behalf of whom a Contribution has been received by Licensor and
      subsequently incorporated within the Work.

   2. Grant of Copyright License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      copyright license to reproduce, prepare Derivative Works of,
      publicly display, publicly perform, sublicense, and distribute the
      Work and such Derivative Works in Source or Object form.

   3. Grant of Patent License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      (except as stated in this section) patent license to make, have made,
      use, offer to sell, sell, import, and otherwise transfer the Work,
      where such license applies only to those patent claims licensable
      by such Contributor that are necessarily infringed by their
      Contribution(s) alone or by combination of their Contribution(s)
      with the Work to which such Contribution(s) was submitted. If You
      institute patent litigation against any entity (including a
      cross-claim or counterclaim in a lawsuit) alleging that the Work
      or a Contribution incorporated within the Work constitutes direct
      or contributory patent infringement, then any patent licenses
      granted to You under this License for that Work shall terminate
      as of the date such litigation is filed.

   4. Redistribution. You may reproduce and distribute copies of the
      Work or Derivative Works thereof in any medium, with or without
      modifications, and in Source or Object form, provided that You
      meet the following conditions:

      (a) You must give any other recipients of the Work or
          Derivative Works a copy of this License; and

      (b) You must cause any modified files to carry prominent notices
          stating that You changed the files; and

      (c) You must retain, in the Source form of any Derivative Works
          that You distribute, all copyright, patent, trademark, and
          attribution notices from the Source form of the Work,
          excluding those notices that do not pertain to any part of
          the Derivative Works; and

      (d) If the Work includes a "NOTICE" text file as part of its
          distribution, then any Derivative Works that You distribute must
          include a readable copy of the attribution notices contained
          within such NOTICE file, excluding those notices that do not
          pertain to any part of the Derivative Works, in at least one
          of the following places: within a NOTICE text file distributed
          as part of the Derivative Works; within the Source form or
          documentation, if provided along with the Derivative Works; or,
          within a display generated by the Derivative Works, if and
          wherever such third-party notices normally appear. The contents
          of the NOTICE file are for informational purposes only and
          do not modify the License. You may add Your own attribution
          notices within Derivative Works that You distribute, alongside
          or as an addendum to the NOTICE text from the Work, provided
          that such additional attribution notices cannot be construed
          as modifying the License.

      You may add Your own copyright statement to Your modifications and
      may provide additional or different license terms and conditions
      for use, reproduction, or distribution of Your modifications, or
      for any such Derivative Works as a whole, provided Your use,
      reproduction, and distribution of the Work otherwise complies with
      the conditions stated in this License.

   5. Submission of Contributions. Unless You explicitly state otherwise,
      any Contribution intentionally submitted for inclusion in the Work
      by You to the Licensor shall be under the terms and conditions of
      this License, without any additional terms or conditions.
      Notwithstanding the above, nothing herein shall supersede or modify
      the terms of any separate license agreement you may have executed
      with Licensor regarding such Contributions.

   6. Trademarks. This License does not grant permission to use the trade
      names, trademarks, service marks, or product names of the Licensor,
      except as required for reasonable and customary use in describing the
      origin of the Work and reproducing the content of the NOTICE file.

   7. Disclaimer of Warranty. Unless required by applicable law or
      agreed to in writing, Licensor provides the Work (and each
      Contributor provides its Contributions) on an "AS IS" BASIS,
      WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
      implied, including, without limitation, any warranties or conditions
      of TITLE, NON-INFRINGEMENT, MERCHANTABILITY, or FITNESS FOR A
      PARTICULAR PURPOSE. You are solely responsible for determining the
      appropriateness of using or redistributing the Work and assume any
      risks associated with Your exercise of permissions under this License.

   8. Limitation of Liability. In no event and under no legal theory,
      whether in tort (including negligence), contract, or otherwise,
      unless required by applicable law (such as deliberate and grossly
      negligent acts) or agreed to in writing, shall any Contributor be
      liable to You for damages, including any direct, indirect, special,
      incidental, or consequential damages of any character arising as a
      result of this License or out of the use or inability to use the
      Work (including but not limited to damages for loss of goodwill,
      work stoppage, computer failure or malfunction, or any and all
      other commercial damages or losses), even if such Contributor
      has been advised of the possibility of such damages.

   9. Accepting Warranty or Additional Liability. While redistributing
      the Work or Derivative Works thereof, You may choose to offer,
      and charge a fee for, acceptance of support, warranty, indemnity,
      or other liability obligations and/or rights consistent with this
      License. However, in accepting such obligations, You may act only
      on Your own behalf and on Your sole responsibility, not on behalf
      of any other Contributor, and only if You agree to indemnify,
      defend, and hold each Contributor harmless for any liability
      incurred by, or claims asserted against, such Contributor by reason
      of your accepting any such warranty or additional liability.

   END OF TERMS AND CONDITIONS

   APPENDIX: How to apply the Apache License to your work.

      To apply the Apache License to your work, attach the following
      boilerplate notice, with the fields enclosed by brackets "[]"
      replaced with your own identifying information. (Don't include
      the brackets!)  The text should be enclosed in the appropriate
      comment syntax for the file format. We also recommend that a
      file or class name and description of purpose be included on the
      same "printed page" as the copyright notice for easier
      identification within third-party archives.

   Copyright [yyyy] [name of copyright owner]

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
`
