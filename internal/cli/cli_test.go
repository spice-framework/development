package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spice-framework/development/internal/catalog"
	"github.com/spice-framework/development/internal/libraryrelease"
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
		!strings.Contains(stdout.String(), "spice-dev bootstrap") ||
		!strings.Contains(stdout.String(), "snapshot materialize") ||
		!strings.Contains(stdout.String(), "library-release public-key") ||
		!strings.Contains(stdout.String(), "library-release sign") ||
		!strings.Contains(stdout.String(), "go-release policy-check") ||
		!strings.Contains(stdout.String(), "go-release render") ||
		!strings.Contains(stdout.String(), "go-release verify") ||
		!strings.Contains(stdout.String(), "distribution-release render") ||
		!strings.Contains(stdout.String(), "distribution-release verify") ||
		!strings.Contains(stdout.String(), "agent-extension init") ||
		!strings.Contains(stdout.String(), "agent-extension check") {
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

func TestRuntimeInitializesSourceOnlyAgentExtensionWithoutRunner(t *testing.T) {
	t.Parallel()
	value, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "extension")
	var stdout, stderr strings.Builder
	code := (Runtime{Catalog: value, Runner: nil}).Run(t.Context(), []string{
		"agent-extension", "init", "--directory", root,
		"--module", "example.com/acme/agent-tool", "--tool-name", "acme.inspect",
		"--profile", "compiled-tool-autoconfigure/v1alpha1-preview6",
	}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "source-only") || stderr.Len() != 0 {
		t.Fatalf("agent-extension init = %d, %q, %q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, "spice-agent-extension.json")); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{
		{"agent-extension"},
		{"agent-extension", "unknown"},
		{"agent-extension", "init", "extra"},
		{"agent-extension", "check", "extra"},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := (Runtime{Catalog: value}).Run(t.Context(), arguments, &stdout, &stderr); code != 2 {
			t.Fatalf("Run(%v) code = %d, stderr=%q", arguments, code, stderr.String())
		}
	}
}

func TestRuntimeChecksGoReleasePolicyWithoutReleaseInputs(t *testing.T) {
	t.Parallel()
	runtime := testRuntime(t)
	// A nil process runner makes any accidental Git, Go, or network-capable
	// command execution fail immediately. Policy comparison is catalog-only.
	runtime.Runner = nil
	accepted := []struct {
		name      string
		arguments []string
		want      string
	}{
		{
			name: "Spice foundation module",
			arguments: []string{
				"go-release", "policy-check",
				"--repo", "spice",
				"--module", "github.com/spice-framework/spice",
				"--version", "v0.1.0-preview.4",
				"--profile", "go-module-v1",
			},
			want: "go-module-v1\tspice\tgithub.com/spice-framework/spice\tv0.1.0-preview.4\n",
		},
		{
			name: "Toolchain distribution",
			arguments: []string{
				"go-release", "policy-check",
				"--repo", "toolchain",
				"--module", "github.com/spice-framework/toolchain",
				"--version", "v0.1.0-preview.5",
				"--profile", "go-distribution-v1",
			},
			want: "go-distribution-v1\ttoolchain\tgithub.com/spice-framework/toolchain\tv0.1.0-preview.5\n",
		},
		{
			name: "Agent module",
			arguments: []string{
				"go-release", "policy-check",
				"--repo", "spice-agent",
				"--module", "github.com/spice-framework/spice-agent",
				"--version", "v0.1.0-preview.7",
				"--profile", "go-module-v1",
			},
			want: "go-module-v1\tspice-agent\tgithub.com/spice-framework/spice-agent\tv0.1.0-preview.7\n",
		},
		{
			name: "coding distribution",
			arguments: []string{
				"go-release", "policy-check",
				"--repo", "spice-agent-coding",
				"--module", "github.com/spice-framework/spice-agent-coding",
				"--version", "v0.1.0-preview.4",
				"--profile", "go-distribution-v1",
			},
			want: "go-distribution-v1\tspice-agent-coding\tgithub.com/spice-framework/spice-agent-coding\tv0.1.0-preview.4\n",
		},
		{
			name: "TUI module",
			arguments: []string{
				"go-release", "policy-check",
				"--repo", "spice-agent-tui",
				"--module", "github.com/spice-framework/spice-agent-tui",
				"--version", "v0.1.0-preview.2",
				"--profile", "go-module-v1",
			},
			want: "go-module-v1\tspice-agent-tui\tgithub.com/spice-framework/spice-agent-tui\tv0.1.0-preview.2\n",
		},
	}
	for _, test := range accepted {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			if code := runtime.Run(t.Context(), test.arguments, &stdout, &stderr); code != 0 || stdout.String() != test.want || stderr.Len() != 0 {
				t.Fatalf("policy-check code/output = %d, %q, %q", code, stdout.String(), stderr.String())
			}
			stdout.Reset()
			if code := runtime.Run(t.Context(), test.arguments, errorWriter{}, &stderr); code != 1 {
				t.Fatalf("policy-check output failure code = %d", code)
			}
		})
	}

	tests := []struct {
		name      string
		arguments []string
		wantCode  int
		wantError string
	}{
		{name: "stale preview.2", arguments: []string{"--repo", "spice-agent", "--module", "github.com/spice-framework/spice-agent", "--version", "v0.1.0-preview.2", "--profile", "go-module-v1"}, wantCode: 1, wantError: "does not match catalog"},
		{name: "stale Spice foundation preview.2", arguments: []string{"--repo", "spice", "--module", "github.com/spice-framework/spice", "--version", "v0.1.0-preview.2", "--profile", "go-module-v1"}, wantCode: 1, wantError: "does not match catalog"},
		{name: "stale Spice foundation preview.3", arguments: []string{"--repo", "spice", "--module", "github.com/spice-framework/spice", "--version", "v0.1.0-preview.3", "--profile", "go-module-v1"}, wantCode: 1, wantError: "does not match catalog"},
		{name: "stale Toolchain preview.1", arguments: []string{"--repo", "toolchain", "--module", "github.com/spice-framework/toolchain", "--version", "v0.1.0-preview.1", "--profile", "go-distribution-v1"}, wantCode: 1, wantError: "does not match catalog"},
		{name: "stale Toolchain preview.2", arguments: []string{"--repo", "toolchain", "--module", "github.com/spice-framework/toolchain", "--version", "v0.1.0-preview.2", "--profile", "go-distribution-v1"}, wantCode: 1, wantError: "does not match catalog"},
		{name: "stale Toolchain preview.3", arguments: []string{"--repo", "toolchain", "--module", "github.com/spice-framework/toolchain", "--version", "v0.1.0-preview.3", "--profile", "go-distribution-v1"}, wantCode: 1, wantError: "does not match catalog"},
		{name: "stale Toolchain preview.4", arguments: []string{"--repo", "toolchain", "--module", "github.com/spice-framework/toolchain", "--version", "v0.1.0-preview.4", "--profile", "go-distribution-v1"}, wantCode: 1, wantError: "does not match catalog"},
		{name: "stale preview.3", arguments: []string{"--repo", "spice-agent", "--module", "github.com/spice-framework/spice-agent", "--version", "v0.1.0-preview.3", "--profile", "go-module-v1"}, wantCode: 1, wantError: "does not match catalog"},
		{name: "stale preview.4", arguments: []string{"--repo", "spice-agent", "--module", "github.com/spice-framework/spice-agent", "--version", "v0.1.0-preview.4", "--profile", "go-module-v1"}, wantCode: 1, wantError: "does not match catalog"},
		{name: "stale preview.5", arguments: []string{"--repo", "spice-agent", "--module", "github.com/spice-framework/spice-agent", "--version", "v0.1.0-preview.5", "--profile", "go-module-v1"}, wantCode: 1, wantError: "does not match catalog"},
		{name: "stale preview.6", arguments: []string{"--repo", "spice-agent", "--module", "github.com/spice-framework/spice-agent", "--version", "v0.1.0-preview.6", "--profile", "go-module-v1"}, wantCode: 1, wantError: "does not match catalog"},
		{name: "stale distribution preview.2", arguments: []string{"--repo", "spice-agent-coding", "--module", "github.com/spice-framework/spice-agent-coding", "--version", "v0.1.0-preview.2", "--profile", "go-distribution-v1"}, wantCode: 1, wantError: "does not match catalog"},
		{name: "stale distribution preview.3", arguments: []string{"--repo", "spice-agent-coding", "--module", "github.com/spice-framework/spice-agent-coding", "--version", "v0.1.0-preview.3", "--profile", "go-distribution-v1"}, wantCode: 1, wantError: "does not match catalog"},
		{name: "stale TUI preview.1", arguments: []string{"--repo", "spice-agent-tui", "--module", "github.com/spice-framework/spice-agent-tui", "--version", "v0.1.0-preview.1", "--profile", "go-module-v1"}, wantCode: 1, wantError: "does not match catalog"},
		{name: "module drift", arguments: []string{"--repo", "spice-agent", "--module", "example.invalid/agent", "--version", "v0.1.0-preview.7", "--profile", "go-module-v1"}, wantCode: 1, wantError: "module does not match"},
		{name: "profile drift", arguments: []string{"--repo", "spice-agent", "--module", "github.com/spice-framework/spice-agent", "--version", "v0.1.0-preview.7", "--profile", "go-distribution-v1"}, wantCode: 1, wantError: "profile does not match"},
		{name: "unknown repository", arguments: []string{"--repo", "unknown", "--module", "github.com/spice-framework/spice-agent", "--version", "v0.1.0-preview.7", "--profile", "go-module-v1"}, wantCode: 1, wantError: "not in the catalog"},
		{name: "positional", arguments: []string{"extra"}, wantCode: 2, wantError: "accepts no positional arguments"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var testOut, testErr strings.Builder
			command := append([]string{"go-release", "policy-check"}, test.arguments...)
			code := runtime.Run(t.Context(), command, &testOut, &testErr)
			if code != test.wantCode || testOut.Len() != 0 || !strings.Contains(testErr.String(), test.wantError) {
				t.Fatalf("policy-check code/output = %d, %q, %q", code, testOut.String(), testErr.String())
			}
		})
	}
}

func TestRuntimeMaterializesAndVerifiesSnapshot(t *testing.T) {
	value, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	const commit = "0123456789abcdef0123456789abcdef01234567"
	lockFile := filepath.Join(t.TempDir(), "ecosystem.lock.json")
	if err := os.WriteFile(lockFile, []byte(`{
  "schema": 1,
  "snapshot": "test",
  "sources": [{"repository":"spice","commit":"`+commit+`"}]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &cliSnapshotRunner{catalog: value, commit: commit}
	runtime := Runtime{Catalog: value, Runner: runner}
	rootParent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(rootParent, "sources")
	var stdout, stderr strings.Builder
	code := runtime.Run(t.Context(), []string{
		"snapshot", "materialize", "--lock", lockFile, "--root", root,
	}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), `"repository": "spice"`) {
		t.Fatalf("snapshot materialize code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = runtime.Run(t.Context(), []string{
		"snapshot", "verify", "--lock", lockFile, "--root", root, "--offline",
	}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), `"commit": "`+commit+`"`) {
		t.Fatalf("snapshot verify code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, arguments := range [][]string{
		{"snapshot"},
		{"snapshot", "unknown"},
		{"snapshot", "verify", "--lock", lockFile, "--root", root},
		{"snapshot", "materialize", "--offline", "--lock", lockFile, "--root", root},
		{"snapshot", "verify", "extra"},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := runtime.Run(t.Context(), arguments, &stdout, &stderr); code != 2 {
			t.Fatalf("Run(%v) code = %d, stderr=%q", arguments, code, stderr.String())
		}
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

func TestRuntimeCreatesLibraryReleasePlan(t *testing.T) {
	t.Parallel()
	value, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	directory := filepath.Join(root, "starter-smtp")
	if err := os.Mkdir(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	metadata := `{"schema":1,"minimum":"v0.1.0","current":"` +
		value.StarterCompatibility.CurrentCore + `"}`
	if err := os.WriteFile(
		filepath.Join(directory, value.StarterCompatibility.MetadataFile),
		[]byte(metadata),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	runtime := Runtime{Catalog: value, Runner: releasePlanRunner{}}
	var stdout, stderr strings.Builder
	code := runtime.Run(t.Context(), []string{
		"library-release", "plan", "--root", directory, "--repo", "starter-smtp",
		"--version", "v1.2.3", "--source-date-epoch", "1700000000", "--rehearsal",
	}, &stdout, &stderr)
	var plan libraryrelease.Plan
	if err := json.Unmarshal([]byte(stdout.String()), &plan); err != nil || code != 0 ||
		plan.Repository != "starter-smtp" || plan.Mode != "rehearsal" ||
		plan.SourceDateEpoch != 1_700_000_000 {
		t.Fatalf("library release plan code=%d plan=%#v err=%v stderr=%q", code, plan, err, stderr.String())
	}
}

func TestRuntimeSignsProductionLibraryRelease(t *testing.T) {
	repository, plan, value, privateKey, publicKey := cliProductionSigningFixture(t)
	planFile := filepath.Join(t.TempDir(), "plan.json")
	content, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planFile, content, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "release")
	var stdout, stderr strings.Builder
	code := (Runtime{Catalog: value}).Run(t.Context(), []string{
		"library-release", "sign",
		"--root", repository,
		"--plan", planFile,
		"--output", output,
		"--signing-key", privateKey,
		"--trusted-public-key", publicKey,
	}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "5 signed artifact(s)") {
		t.Fatalf("library release sign code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("signed release entries = %v", entries)
	}
}

func TestRuntimeDerivesReleasePublicKeyWithoutLoggingPrivateMaterial(t *testing.T) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x27}, ed25519.SeedSize))
	privateKeyContent := base64.StdEncoding.EncodeToString(privateKey.Seed())
	privateKeyFile := filepath.Join(t.TempDir(), "release.key")
	if err := os.WriteFile(privateKeyFile, []byte(privateKeyContent), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "release-public.pem")
	var stdout, stderr strings.Builder
	code := testRuntime(t).Run(t.Context(), []string{
		"library-release", "public-key",
		"--signing-key", privateKeyFile,
		"--output", output,
	}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "Ed25519 PKIX public key") {
		t.Fatalf("library-release public-key code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), privateKeyFile) ||
		strings.Contains(stdout.String(), privateKeyContent) || stderr.Len() != 0 {
		t.Fatalf("private signing material was logged: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	block, rest := pem.Decode(content)
	if block == nil || block.Type != "PUBLIC KEY" || len(rest) != 0 {
		t.Fatalf("public-key output = %q", content)
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
	if code := runtime.Run(t.Context(), []string{"library-release"}, &stdout, &stderr); code != 2 {
		t.Fatalf("library-release missing plan code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{"go-release"}, &stdout, &stderr); code != 2 {
		t.Fatalf("go-release missing subcommand code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{"go-release", "unknown"}, &stdout, &stderr); code != 2 {
		t.Fatalf("go-release unknown subcommand code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{
		"go-release", "render", "--repo", "unknown", "--version", "v1.0.0",
		"--root", t.TempDir(), "--output", filepath.Join(t.TempDir(), "release"),
	}, &stdout, &stderr); code != 1 {
		t.Fatalf("go-release unknown repository code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{
		"go-release", "render", "--artifacts", t.TempDir(),
	}, &stdout, &stderr); code != 2 {
		t.Fatalf("go-release render artifacts code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{
		"go-release", "verify", "--output", t.TempDir(),
	}, &stdout, &stderr); code != 2 {
		t.Fatalf("go-release verify output code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{"go-release", "verify", "extra"}, &stdout, &stderr); code != 2 {
		t.Fatalf("go-release positional code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{"distribution-release"}, &stdout, &stderr); code != 2 {
		t.Fatalf("distribution-release missing subcommand code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{"distribution-release", "unknown"}, &stdout, &stderr); code != 2 {
		t.Fatalf("distribution-release unknown subcommand code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{
		"distribution-release", "render", "--artifacts", t.TempDir(),
	}, &stdout, &stderr); code != 2 {
		t.Fatalf("distribution-release render artifacts code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{
		"distribution-release", "verify", "--output", t.TempDir(),
	}, &stdout, &stderr); code != 2 {
		t.Fatalf("distribution-release verify output code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{
		"library-release", "public-key", "extra",
	}, &stdout, &stderr); code != 2 {
		t.Fatalf("library-release public-key positional code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{
		"library-release", "render", "--root", t.TempDir(),
		"--plan", filepath.Join(t.TempDir(), "missing.json"),
		"--output", filepath.Join(t.TempDir(), "release"),
	}, &stdout, &stderr); code != 1 {
		t.Fatalf("library-release missing render plan code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{
		"library-release", "sign", "--root", t.TempDir(),
		"--plan", filepath.Join(t.TempDir(), "missing.json"),
		"--output", filepath.Join(t.TempDir(), "release"),
		"--signing-key", filepath.Join(t.TempDir(), "release.key"),
		"--trusted-public-key", filepath.Join(t.TempDir(), "release.pub"),
	}, &stdout, &stderr); code != 1 {
		t.Fatalf("library-release missing sign plan code = %d", code)
	}
	if code := runtime.Run(t.Context(), []string{
		"library-release", "sign", "extra",
	}, &stdout, &stderr); code != 2 {
		t.Fatalf("library-release sign positional code = %d", code)
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

type releasePlanRunner struct{}

type cliSnapshotRunner struct {
	catalog catalog.Catalog
	commit  string
}

func (runner *cliSnapshotRunner) Run(
	ctx context.Context,
	directory string,
	arguments ...string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if slices.Equal(arguments, []string{"git", "remote", "get-url", "origin"}) {
		name := filepath.Base(directory)
		for _, repository := range runner.catalog.Repositories {
			if repository.Directory == name {
				return repository.CloneURL, nil
			}
		}
	}
	if slices.Equal(arguments, []string{"git", "rev-parse", "--verify", "HEAD^{commit}"}) {
		return runner.commit, nil
	}
	return "", nil
}

type cliReleaseFixture struct {
	Plan libraryrelease.Plan `json:"plan"`
	Tree []struct {
		Mode    string `json:"mode"`
		Name    string `json:"name"`
		Content string `json:"content"`
	} `json:"tree"`
}

func cliProductionSigningFixture(
	t *testing.T,
) (string, libraryrelease.Plan, catalog.Catalog, string, string) {
	t.Helper()
	fixtureContent, err := os.ReadFile(filepath.Join(
		"..", "libraryrelease", "testdata", "parity", "newer.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	var fixture cliReleaseFixture
	if err := json.Unmarshal(fixtureContent, &fixture); err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	for _, entry := range fixture.Tree {
		name := filepath.Join(repository, filepath.FromSlash(entry.Name))
		if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o600)
		if entry.Mode == "100755" {
			mode = 0o700
		}
		if err := os.WriteFile(name, []byte(entry.Content), mode); err != nil {
			t.Fatal(err)
		}
	}
	cliGitCommand(t, repository, "init")
	cliGitCommand(t, repository, "config", "commit.gpgsign", "false")
	cliGitCommand(t, repository, "config", "tag.gpgsign", "false")
	cliGitCommand(t, repository, "config", "user.name", "Spice Test")
	cliGitCommand(t, repository, "config", "user.email", "spice@example.invalid")
	cliGitCommand(
		t,
		repository,
		"remote",
		"add",
		"origin",
		"https://github.com/spice-framework/starter-oidc.git",
	)
	cliGitCommand(t, repository, "add", ".")
	command := exec.Command("git", "commit", "-m", "fixture")
	command.Dir = repository
	date := time.Unix(fixture.Plan.SourceDateEpoch, 0).UTC().Format(time.RFC3339)
	command.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if output, commitErr := command.CombinedOutput(); commitErr != nil {
		t.Fatalf("git commit: %v\n%s", commitErr, output)
	}
	fixture.Plan.Mode = "production"
	fixture.Plan.Commit = strings.TrimSpace(cliGitCommand(t, repository, "rev-parse", "HEAD"))
	fixture.Plan.Artifacts = []string{
		"checksums.txt",
		"checksums.txt.pem",
		"checksums.txt.sig",
		"starter-oidc_1.2.3_sbom.spdx.json",
		"starter-oidc_1.2.3_source.tar.gz",
	}
	cliGitCommand(t, repository, "tag", fixture.Plan.Version)
	value, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	value.StarterCompatibility.CurrentCore = fixture.Plan.CompatibilityCurrent
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	keyDirectory := t.TempDir()
	privateKeyPath := filepath.Join(keyDirectory, "release.key")
	publicKeyPath := filepath.Join(keyDirectory, "release.pub")
	privateKey := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	if err := os.WriteFile(
		privateKeyPath,
		[]byte(base64.StdEncoding.EncodeToString(privateKey.Seed())),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(privateKey.Public())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		publicKeyPath,
		pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	return repository, fixture.Plan, value, privateKeyPath, publicKeyPath
}

func cliGitCommand(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func (releasePlanRunner) Run(
	ctx context.Context,
	_ string,
	arguments ...string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	switch {
	case slices.Equal(arguments, []string{"git", "remote", "get-url", "origin"}):
		return "https://github.com/spice-framework/starter-smtp.git", nil
	case slices.Equal(arguments, []string{"go", "mod", "edit", "-json"}):
		return `{"Module":{"Path":"github.com/spice-framework/starter-smtp"},"Require":[{"Path":"github.com/spice-framework/spice","Version":"v0.1.0"}]}`, nil
	case slices.Equal(arguments, []string{"git", "rev-parse", "--verify", "HEAD^{commit}"}):
		return "0123456789abcdef0123456789abcdef01234567", nil
	case len(arguments) == 5 && slices.Equal(arguments[:4], []string{"git", "show", "-s", "--format=%ct"}):
		return "1700000000", nil
	case len(arguments) == 4 && slices.Equal(arguments[:3], []string{"git", "cat-file", "-e"}):
		return "", nil
	case len(arguments) == 8 && slices.Equal(arguments[:4], []string{"git", "diff", "--no-ext-diff", "--unified=0"}):
		return "", nil
	default:
		return "", errors.New("unexpected release plan command")
	}
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
