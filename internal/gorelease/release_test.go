package gorelease

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spice-framework/development/internal/catalog"
	"github.com/spice-framework/development/internal/process"
)

func TestRenderAndVerifyDeterministicModuleRelease(t *testing.T) {
	t.Parallel()
	fixture := newReleaseFixture(t, fixtureOptions{dependency: true, toolDependency: true})
	first := filepath.Join(fixture.parent, "first")
	result, err := Render(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, first)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []string{
		"checksums.txt",
		"spice-agent_0.1.0-preview.6_release.json",
		"spice-agent_0.1.0-preview.6_sbom.spdx.json",
		"spice-agent_0.1.0-preview.6_source.tar.gz",
	}
	if !slices.Equal(result.Files, wantFiles) || result.Commit != fixture.commit {
		t.Fatalf("Render() = %#v", result)
	}
	verified, err := Verify(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, first)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(verified.Files, wantFiles) || verified.Commit != fixture.commit {
		t.Fatalf("Verify() = %#v", verified)
	}
	second := filepath.Join(fixture.parent, "second")
	if _, err := Render(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, second); err != nil {
		t.Fatal(err)
	}
	for _, name := range wantFiles {
		left, err := os.ReadFile(filepath.Join(first, name))
		if err != nil {
			t.Fatal(err)
		}
		right, err := os.ReadFile(filepath.Join(second, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(left, right) {
			t.Fatalf("artifact %s differs between renders", name)
		}
	}
	assertArchive(t, filepath.Join(first, wantFiles[3]), "spice-agent_0.1.0-preview.6/agent.go")
	assertMetadata(t, filepath.Join(first, wantFiles[1]), fixture.commit)
	if _, err := gitBinary(t.Context(), fixture.root, 1, "show", fixture.commit+":README.md"); err == nil || !strings.Contains(err.Error(), "exceeds 1 bytes") {
		t.Fatalf("gitBinary(truncated) error = %v", err)
	}
	sbomContent, err := os.ReadFile(filepath.Join(first, wantFiles[2]))
	if err != nil {
		t.Fatal(err)
	}
	var sbom spdxDocument
	if err := json.Unmarshal(sbomContent, &sbom); err != nil || len(sbom.Packages) != 2 || len(sbom.Relationships) != 2 {
		t.Fatalf("SBOM dependency graph = %#v, %v", sbom, err)
	}
}

func TestRenderAndVerifyDependencyFreeModuleRelease(t *testing.T) {
	t.Parallel()
	fixture := newReleaseFixture(t, fixtureOptions{omitGoSum: true, omitVendorModules: true})
	output := filepath.Join(fixture.parent, "release")
	result, err := Render(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, output)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, output); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(output, "spice-agent_0.1.0-preview.6_source.tar.gz")
	entries := archiveEntries(t, archive)
	for _, unexpected := range []string{
		"spice-agent_0.1.0-preview.6/go.sum",
		"spice-agent_0.1.0-preview.6/vendor/modules.txt",
	} {
		if slices.Contains(entries, unexpected) {
			t.Fatalf("dependency-free source archive unexpectedly contains %q", unexpected)
		}
	}
	sbomContent, err := os.ReadFile(filepath.Join(output, "spice-agent_0.1.0-preview.6_sbom.spdx.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sbom spdxDocument
	if err := json.Unmarshal(sbomContent, &sbom); err != nil || len(sbom.Packages) != 1 || len(sbom.Relationships) != 1 {
		t.Fatalf("dependency-free SBOM = %#v, %v", sbom, err)
	}
	if result.Commit != fixture.commit {
		t.Fatalf("Render() commit = %q, want %q", result.Commit, fixture.commit)
	}
}

func TestCheckPolicyRequiresExactCatalogAuthorization(t *testing.T) {
	t.Parallel()
	value, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	foundationWant := Policy{
		Profile:    catalog.ReleaseProfileGoModule,
		Repository: "spice",
		Module:     "github.com/spice-framework/spice",
		Version:    "v0.1.0-preview.4",
	}
	foundationGot, err := CheckPolicy(PolicyOptions{
		Profile:    foundationWant.Profile,
		Repository: foundationWant.Repository,
		Module:     foundationWant.Module,
		Version:    foundationWant.Version,
	}, value)
	if err != nil {
		t.Fatal(err)
	}
	if foundationGot != foundationWant {
		t.Fatalf("CheckPolicy(Spice foundation) = %#v, want %#v", foundationGot, foundationWant)
	}
	toolchainWant := Policy{
		Profile:    catalog.ReleaseProfileDistribution,
		Repository: "toolchain",
		Module:     "github.com/spice-framework/toolchain",
		Version:    "v0.1.0-preview.3",
	}
	toolchainGot, err := CheckPolicy(PolicyOptions{
		Profile:    toolchainWant.Profile,
		Repository: toolchainWant.Repository,
		Module:     toolchainWant.Module,
		Version:    toolchainWant.Version,
	}, value)
	if err != nil {
		t.Fatal(err)
	}
	if toolchainGot != toolchainWant {
		t.Fatalf("CheckPolicy(Toolchain distribution) = %#v, want %#v", toolchainGot, toolchainWant)
	}
	want := Policy{
		Profile:    catalog.ReleaseProfileGoModule,
		Repository: "spice-agent",
		Module:     "github.com/spice-framework/spice-agent",
		Version:    "v0.1.0-preview.6",
	}
	got, err := CheckPolicy(PolicyOptions{
		Profile:    want.Profile,
		Repository: want.Repository,
		Module:     want.Module,
		Version:    want.Version,
	}, value)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("CheckPolicy() = %#v, want %#v", got, want)
	}
	tuiWant := Policy{
		Profile:    catalog.ReleaseProfileGoModule,
		Repository: "spice-agent-tui",
		Module:     "github.com/spice-framework/spice-agent-tui",
		Version:    "v0.1.0-preview.2",
	}
	tuiGot, err := CheckPolicy(PolicyOptions{
		Profile:    tuiWant.Profile,
		Repository: tuiWant.Repository,
		Module:     tuiWant.Module,
		Version:    tuiWant.Version,
	}, value)
	if err != nil {
		t.Fatal(err)
	}
	if tuiGot != tuiWant {
		t.Fatalf("CheckPolicy(TUI) = %#v, want %#v", tuiGot, tuiWant)
	}
	distributionWant := Policy{
		Profile:    catalog.ReleaseProfileDistribution,
		Repository: "spice-agent-coding",
		Module:     "github.com/spice-framework/spice-agent-coding",
		Version:    "v0.1.0-preview.4",
	}
	distributionGot, err := CheckPolicy(PolicyOptions{
		Profile:    distributionWant.Profile,
		Repository: distributionWant.Repository,
		Module:     distributionWant.Module,
		Version:    distributionWant.Version,
	}, value)
	if err != nil {
		t.Fatal(err)
	}
	if distributionGot != distributionWant {
		t.Fatalf("CheckPolicy(distribution) = %#v, want %#v", distributionGot, distributionWant)
	}

	tests := []struct {
		name    string
		options PolicyOptions
		mutate  func(*catalog.Catalog)
		want    string
	}{
		{name: "missing repository", options: PolicyOptions{Profile: want.Profile, Module: want.Module, Version: want.Version}, want: "repository is required"},
		{name: "unknown repository", options: PolicyOptions{Profile: want.Profile, Repository: "unknown", Module: want.Module, Version: want.Version}, want: "not in the catalog"},
		{name: "starter", options: PolicyOptions{Profile: want.Profile, Repository: "starter-smtp", Module: want.Module, Version: want.Version}, want: "must use library-release"},
		{name: "distribution profile drift", options: PolicyOptions{Profile: want.Profile, Repository: distributionWant.Repository, Module: distributionWant.Module, Version: distributionWant.Version}, want: "profile does not match"},
		{name: "stale Spice foundation preview.2", options: PolicyOptions{Profile: foundationWant.Profile, Repository: foundationWant.Repository, Module: foundationWant.Module, Version: "v0.1.0-preview.2"}, want: "does not match catalog"},
		{name: "stale Spice foundation preview.3", options: PolicyOptions{Profile: foundationWant.Profile, Repository: foundationWant.Repository, Module: foundationWant.Module, Version: "v0.1.0-preview.3"}, want: "does not match catalog"},
		{name: "stale Toolchain preview.1", options: PolicyOptions{Profile: toolchainWant.Profile, Repository: toolchainWant.Repository, Module: toolchainWant.Module, Version: "v0.1.0-preview.1"}, want: "does not match catalog"},
		{name: "stale Toolchain preview.2", options: PolicyOptions{Profile: toolchainWant.Profile, Repository: toolchainWant.Repository, Module: toolchainWant.Module, Version: "v0.1.0-preview.2"}, want: "does not match catalog"},
		{name: "stale distribution preview.2", options: PolicyOptions{Profile: distributionWant.Profile, Repository: distributionWant.Repository, Module: distributionWant.Module, Version: "v0.1.0-preview.2"}, want: "does not match catalog"},
		{name: "stale distribution preview.3", options: PolicyOptions{Profile: distributionWant.Profile, Repository: distributionWant.Repository, Module: distributionWant.Module, Version: "v0.1.0-preview.3"}, want: "does not match catalog"},
		{name: "stale TUI preview.1", options: PolicyOptions{Profile: tuiWant.Profile, Repository: tuiWant.Repository, Module: tuiWant.Module, Version: "v0.1.0-preview.1"}, want: "does not match catalog"},
		{name: "stale preview.2", options: PolicyOptions{Profile: want.Profile, Repository: want.Repository, Module: want.Module, Version: "v0.1.0-preview.2"}, want: "does not match catalog"},
		{name: "stale preview.3", options: PolicyOptions{Profile: want.Profile, Repository: want.Repository, Module: want.Module, Version: "v0.1.0-preview.3"}, want: "does not match catalog"},
		{name: "stale preview.4", options: PolicyOptions{Profile: want.Profile, Repository: want.Repository, Module: want.Module, Version: "v0.1.0-preview.4"}, want: "does not match catalog"},
		{name: "stale preview.5", options: PolicyOptions{Profile: want.Profile, Repository: want.Repository, Module: want.Module, Version: "v0.1.0-preview.5"}, want: "does not match catalog"},
		{name: "missing profile", options: PolicyOptions{Repository: want.Repository, Module: want.Module, Version: want.Version}, want: "profile is required"},
		{name: "profile drift", options: PolicyOptions{Profile: catalog.ReleaseProfileDistribution, Repository: want.Repository, Module: want.Module, Version: want.Version}, want: "profile does not match"},
		{name: "missing module", options: PolicyOptions{Profile: want.Profile, Repository: want.Repository, Version: want.Version}, want: "module is required"},
		{name: "module drift", options: PolicyOptions{Profile: want.Profile, Repository: want.Repository, Module: "example.invalid/agent", Version: want.Version}, want: "module does not match"},
		{name: "invalid catalog", options: PolicyOptions{Profile: want.Profile, Repository: want.Repository, Module: want.Module, Version: want.Version}, mutate: func(value *catalog.Catalog) { value.Schema = 0 }, want: "validate Go release catalog"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := value
			if test.mutate != nil {
				test.mutate(&input)
			}
			if _, err := CheckPolicy(test.options, input); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CheckPolicy() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRenderRejectsUntrustedSourceAndPolicy(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		fixture fixtureOptions
		mutate  func(*releaseFixture)
		want    string
	}{
		{name: "unknown repository", mutate: func(value *releaseFixture) { value.options.Repository = "unknown" }, want: "not in the catalog"},
		{name: "stale preview.2", mutate: func(value *releaseFixture) { value.options.Version = "v0.1.0-preview.2" }, want: "does not match catalog"},
		{name: "stale preview.3", mutate: func(value *releaseFixture) { value.options.Version = "v0.1.0-preview.3" }, want: "does not match catalog"},
		{name: "stale preview.4", mutate: func(value *releaseFixture) { value.options.Version = "v0.1.0-preview.4" }, want: "does not match catalog"},
		{name: "stale preview.5", mutate: func(value *releaseFixture) { value.options.Version = "v0.1.0-preview.5" }, want: "does not match catalog"},
		{name: "distribution profile", mutate: func(value *releaseFixture) { value.options.Repository = "spice-agent-coding" }, want: "not authorized"},
		{name: "starter bypass", mutate: func(value *releaseFixture) { value.options.Repository = "starter-smtp" }, want: "must use library-release"},
		{name: "origin mismatch", mutate: func(value *releaseFixture) {
			git(t, value.root, "remote", "set-url", "origin", "https://github.com/spice-framework/not-agent.git")
		}, want: "does not match"},
		{name: "dirty checkout", mutate: func(value *releaseFixture) { writeFile(t, filepath.Join(value.root, "dirty"), "dirty") }, want: "must be clean"},
		{name: "missing tag", mutate: func(value *releaseFixture) { git(t, value.root, "tag", "-d", value.options.Version) }, want: "resolve Go release tag"},
		{name: "intent mismatch", fixture: fixtureOptions{intentModule: "example.invalid/wrong"}, want: "does not match catalog contract"},
		{name: "unknown intent field", fixture: fixtureOptions{unknownIntent: true}, want: "unknown field"},
		{name: "replace directive", fixture: fixtureOptions{replaceDirective: true}, want: "must not contain replace directives"},
		{name: "dependency-free missing only go sum", fixture: fixtureOptions{omitGoSum: true}, want: "both go.sum and vendor/modules.txt or neither"},
		{name: "dependency-free missing only vendor graph", fixture: fixtureOptions{omitVendorModules: true}, want: "both go.sum and vendor/modules.txt or neither"},
		{name: "dependency-free inconsistent catalog", fixture: fixtureOptions{omitGoSum: true, omitVendorModules: true}, mutate: func(value *releaseFixture) {
			value.repository.Release.RequiredModules = []catalog.ReleaseModule{{Path: "example.invalid/required", Version: "v1.0.0"}}
			replaceRepository(value)
		}, want: "catalog requires modules"},
		{name: "dependency-free require smuggling", fixture: fixtureOptions{dependency: true, omitCatalogDependency: true, omitGoSum: true, omitVendorModules: true}, want: "must not contain require directives"},
		{name: "dependency-free tool smuggling", fixture: fixtureOptions{toolDependency: true, omitGoSum: true, omitVendorModules: true}, want: "must not contain tool directives"},
		{name: "dependency-free replace smuggling", fixture: fixtureOptions{replaceDirective: true, omitGoSum: true, omitVendorModules: true}, want: "must not contain replace directives"},
		{name: "dependency-free source dependency smuggling", fixture: fixtureOptions{sourceDependency: true, omitGoSum: true, omitVendorModules: true}, want: "validate dependency-free Go release packages"},
		{name: "dependency-free unexpected vendor", fixture: fixtureOptions{omitGoSum: true, omitVendorModules: true, unexpectedVendor: true}, want: "unexpected committed vendor path"},
		{name: "missing required module", mutate: func(value *releaseFixture) {
			value.repository.Release.RequiredModules = []catalog.ReleaseModule{{Path: "example.invalid/required", Version: "v1.0.0"}}
			replaceRepository(value)
		}, want: "must require"},
		{name: "wrong required module version", mutate: func(value *releaseFixture) {
			value.repository.Release.RequiredModules = []catalog.ReleaseModule{{Path: "example.com/dependency", Version: "v1.2.4"}}
			replaceRepository(value)
		}, fixture: fixtureOptions{dependency: true}, want: "exact catalog version v1.2.4"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newReleaseFixture(t, test.fixture)
			if test.mutate != nil {
				test.mutate(&fixture)
			}
			output := filepath.Join(fixture.parent, "release")
			if _, err := Render(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, output); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Render() error = %v, want %q", err, test.want)
			}
			if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed render created output: %v", err)
			}
		})
	}
}

func TestRenderAndVerifyRejectOutputAttacks(t *testing.T) {
	t.Parallel()
	fixture := newReleaseFixture(t, fixtureOptions{})
	if _, err := Render(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, filepath.Join(fixture.root, "dist")); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("Render(inside repository) error = %v", err)
	}
	output := filepath.Join(fixture.parent, "release")
	if _, err := Render(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, output); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, output); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Render(existing) error = %v", err)
	}
	checksumsPath := filepath.Join(output, "checksums.txt")
	if err := os.WriteFile(checksumsPath, []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, output); err == nil || !strings.Contains(err.Error(), "not reproducible") {
		t.Fatalf("Verify(tampered) error = %v", err)
	}
	if err := os.Remove(checksumsPath); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, output); err == nil || !strings.Contains(err.Error(), "do not match contract") {
		t.Fatalf("Verify(missing) error = %v", err)
	}
}

func TestRenderRejectsInvalidBoundaries(t *testing.T) {
	t.Parallel()
	fixture := newReleaseFixture(t, fixtureOptions{})
	if _, err := Render(nil, fixture.options, fixture.catalog, process.ExecRunner{}, filepath.Join(fixture.parent, "nil")); err == nil || !strings.Contains(err.Error(), "context") { //nolint:staticcheck // Fail-closed nil boundary.
		t.Fatalf("Render(nil context) error = %v", err)
	}
	if _, err := Render(t.Context(), fixture.options, fixture.catalog, nil, filepath.Join(fixture.parent, "runner")); err == nil || !strings.Contains(err.Error(), "runner") {
		t.Fatalf("Render(nil runner) error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Render(canceled, fixture.options, fixture.catalog, process.ExecRunner{}, filepath.Join(fixture.parent, "canceled")); !errors.Is(err, context.Canceled) && (err == nil || !strings.Contains(err.Error(), "canceled")) {
		t.Fatalf("Render(canceled) error = %v", err)
	}
	if _, err := Verify(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, filepath.Join(fixture.parent, "missing")); err == nil || !strings.Contains(err.Error(), "artifact directory") {
		t.Fatalf("Verify(missing) error = %v", err)
	}
}

func TestPrepareOutputRejectsSymlinkAncestorIntoGoRepository(t *testing.T) {
	t.Parallel()
	fixture := newReleaseFixture(t, fixtureOptions{})
	indirect := filepath.Join(fixture.parent, "indirect")
	if err := os.Symlink(fixture.root, indirect); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, os.ErrPermission) {
			t.Skipf("creating a Windows symlink requires developer mode or privilege: %v", err)
		}
		t.Fatal(err)
	}
	configured := filepath.Join(indirect, "vendor", "release")
	if _, _, err := prepareOutput(fixture.root, configured); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("prepareOutput(symlink ancestor) error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(fixture.root, "vendor", "release")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe Go release output was created through symlink ancestor: %v", err)
	}
}

func TestPureValidationHelpers(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "../x", "/x", `a\\b`, "AUX.txt", "bad?.go", "x./y"} {
		if err := validatePortablePath(value); err == nil {
			t.Errorf("validatePortablePath(%q) error = nil", value)
		}
	}
	if err := validatePortablePath("docs/release.md"); err != nil {
		t.Fatal(err)
	}
	const objectID = "0123456789abcdef0123456789abcdef01234567"
	if err := validateTree([]byte("100644 blob " + objectID + "\tREADME.md\x00")); err != nil {
		t.Fatal(err)
	}
	for _, content := range [][]byte{
		nil,
		[]byte("120000 blob " + objectID + "\tlink\x00"),
		[]byte("100644 blob " + objectID + "\tFile.go\x00100644 blob " + objectID + "\tfile.go\x00"),
	} {
		if err := validateTree(content); err == nil {
			t.Fatalf("validateTree(%q) error = nil", content)
		}
	}
	for _, value := range []string{"file:///tmp/repo", "https://user@example.com/repo", "ssh://user@example.com/repo", "https://example.com/../repo", "https://example.com:443/repo", "ssh://git@example.com:22/repo"} {
		if _, err := remoteIdentity(value); err == nil {
			t.Errorf("remoteIdentity(%q) error = nil", value)
		}
	}
	if left, err := remoteIdentity("git@github.com:spice-framework/spice-agent.git"); err != nil || left != "github.com/spice-framework/spice-agent" {
		t.Fatalf("remoteIdentity(SCP) = %q, %v", left, err)
	}
	var decoded intent
	if err := decodeStrict([]byte(`{"schema":1} trailing`), &decoded); err == nil {
		t.Fatal("decodeStrict(trailing) error = nil")
	}
	if got := string(checksums(map[string][]byte{"b": []byte("b"), "a": []byte("a")})); !strings.HasSuffix(got, "  b\n") || strings.Index(got, "  a\n") > strings.Index(got, "  b\n") {
		t.Fatalf("checksums order = %q", got)
	}
	for name, content := range map[string]string{
		"malformed":   "# example.com/module not-a-version\n",
		"replacement": "# example.com/module v1.0.0 => ../local\n",
		"duplicate":   "# example.com/module v1.0.0\n# example.com/module v1.0.0\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseVendorModules([]byte(content)); err == nil {
				t.Fatal("parseVendorModules() error = nil")
			}
		})
	}
	for name, content := range map[string]string{
		"extra dependency": "{\"Path\":\"example.com/main\",\"Main\":true}\n{\"Path\":\"example.com/dependency\",\"Version\":\"v1.0.0\"}\n",
		"wrong main":       "{\"Path\":\"example.com/other\",\"Main\":true}\n",
		"non-main":         "{\"Path\":\"example.com/main\"}\n",
		"versioned main":   "{\"Path\":\"example.com/main\",\"Version\":\"v1.0.0\",\"Main\":true}\n",
		"replaced main":    "{\"Path\":\"example.com/main\",\"Main\":true,\"Replace\":{\"Path\":\"example.com/other\"}}\n",
		"malformed":        "not json",
	} {
		t.Run("dependency-free graph "+name, func(t *testing.T) {
			t.Parallel()
			if err := validateDependencyFreeModuleList([]byte(content), "example.com/main"); err == nil {
				t.Fatal("validateDependencyFreeModuleList() error = nil")
			}
		})
	}
	if err := validateDependencyFreeModuleList(
		[]byte("{\"Path\":\"example.com/main\",\"Main\":true}\n"),
		"example.com/main",
	); err != nil {
		t.Fatalf("validateDependencyFreeModuleList(main only) error = %v", err)
	}
	buffer := boundedBuffer{maximum: 2}
	if written, err := buffer.Write([]byte("long")); err != nil || written != 4 || !buffer.truncated || buffer.String() != "lo" {
		t.Fatalf("boundedBuffer = %#v, written=%d err=%v", buffer, written, err)
	}
	if written, err := buffer.Write([]byte("more")); err != nil || written != 4 {
		t.Fatalf("boundedBuffer full write = %d, %v", written, err)
	}
	directory := t.TempDir()
	if _, err := realDirectory("", "test"); err == nil {
		t.Fatal("realDirectory(empty) error = nil")
	}
	file := filepath.Join(directory, "file")
	writeFile(t, file, "x")
	if _, err := realDirectory(file, "test"); err == nil {
		t.Fatal("realDirectory(file) error = nil")
	}
	if _, err := readBounded(file, 0); err == nil {
		t.Fatal("readBounded(oversized) error = nil")
	}
	if err := writeArtifact(directory, "../bad", []byte("x")); err == nil {
		t.Fatal("writeArtifact(unsafe) error = nil")
	}
	if err := writeArtifact(directory, "new", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := writeArtifact(directory, "new", []byte("x")); err == nil {
		t.Fatal("writeArtifact(existing) error = nil")
	}
}

type fixtureOptions struct {
	intentModule          string
	unknownIntent         bool
	replaceDirective      bool
	dependency            bool
	omitCatalogDependency bool
	toolDependency        bool
	omitGoSum             bool
	omitVendorModules     bool
	sourceDependency      bool
	unexpectedVendor      bool
}

type releaseFixture struct {
	parent     string
	root       string
	commit     string
	options    Options
	catalog    catalog.Catalog
	repository catalog.Repository
}

func newReleaseFixture(t *testing.T, options fixtureOptions) releaseFixture {
	t.Helper()
	value, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	repositoryIndex := slices.IndexFunc(value.Repositories, func(repository catalog.Repository) bool {
		return repository.Name == "spice-agent"
	})
	repository := value.Repositories[repositoryIndex]
	repository.Release.RequiredModules = nil
	if options.dependency && !options.omitCatalogDependency {
		repository.Release.RequiredModules = []catalog.ReleaseModule{{Path: "example.com/dependency", Version: "v1.2.3"}}
	}
	value.Repositories[repositoryIndex] = repository
	parent := canonicalTestDirectory(t, t.TempDir())
	root := filepath.Join(parent, "repository")
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o750); err != nil {
		t.Fatal(err)
	}
	modulePath := repository.Module
	intentModule := modulePath
	if options.intentModule != "" {
		intentModule = options.intentModule
	}
	metadata := `{"schema":1,"profile":"go-module-v1","repository":"spice-agent","module":"` + intentModule + `","version":"v0.1.0-preview.6"}`
	if options.unknownIntent {
		metadata = strings.TrimSuffix(metadata, "}") + `,"unexpected":true}`
	}
	modfile := "module " + modulePath + "\n\ngo 1.26.0\n\ntoolchain go1.26.5\n"
	if options.dependency {
		modfile += "\nrequire example.com/dependency v1.2.3"
		if options.toolDependency {
			modfile += " // indirect"
		}
		modfile += "\n"
	}
	if options.toolDependency {
		modfile += "\ntool example.com/dependency/cmd/tool\n"
	}
	if options.replaceDirective {
		modfile += "\nreplace example.invalid/old => example.invalid/new v1.0.0\n"
	}
	files := map[string]string{
		"LICENSE":            "Apache License fixture\n",
		"README.md":          "# Agent fixture\n",
		"agent.go":           "package agent\n",
		"go.mod":             modfile,
		"spice-release.json": metadata + "\n",
	}
	if !options.omitGoSum {
		files["go.sum"] = ""
	}
	if !options.omitVendorModules {
		files["vendor/modules.txt"] = ""
	}
	if options.dependency && !options.omitVendorModules {
		if options.toolDependency {
			files["vendor/modules.txt"] = "# example.com/dependency v1.2.3\n## explicit; go 1.26.0\nexample.com/dependency/cmd/tool\n"
			files["vendor/example.com/dependency/cmd/tool/main.go"] = "package main\n\nfunc main() {}\n"
		} else {
			files["agent.go"] = "package agent\n\nimport _ \"example.com/dependency\"\n"
			files["vendor/modules.txt"] = "# example.com/dependency v1.2.3\n## explicit; go 1.26.0\nexample.com/dependency\n"
			files["vendor/example.com/dependency/dependency.go"] = "package dependency\n"
		}
	}
	if options.sourceDependency {
		files["agent.go"] = "package agent\n\nimport _ \"example.invalid/hidden\"\n"
	}
	if options.unexpectedVendor {
		files["vendor/unexpected.go"] = "package unexpected\n"
	}
	for name, content := range files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(name)), content)
	}
	git(t, root, "init")
	git(t, root, "config", "core.autocrlf", "false")
	git(t, root, "config", "core.eol", "lf")
	git(t, root, "config", "commit.gpgsign", "false")
	git(t, root, "config", "tag.gpgsign", "false")
	git(t, root, "config", "user.name", "Spice Test")
	git(t, root, "config", "user.email", "spice@example.invalid")
	git(t, root, "remote", "add", "origin", repository.CloneURL)
	git(t, root, "add", ".")
	command := exec.Command("git", "commit", "-m", "fixture")
	command.Dir = root
	date := time.Unix(1_700_000_000, 0).UTC().Format(time.RFC3339)
	command.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, output)
	}
	commit := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	git(t, root, "tag", repository.Release.Version)
	return releaseFixture{
		parent: parent, root: root, commit: commit, catalog: value, repository: repository,
		options: Options{Root: root, Repository: repository.Name, Version: repository.Release.Version},
	}
}

func replaceRepository(fixture *releaseFixture) {
	index := slices.IndexFunc(fixture.catalog.Repositories, func(repository catalog.Repository) bool {
		return repository.Name == fixture.options.Repository || repository.Name == "spice-agent"
	})
	fixture.catalog.Repositories[index] = fixture.repository
}

func writeFile(t *testing.T, name string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func assertArchive(t *testing.T, name string, wanted string) {
	t.Helper()
	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			t.Fatalf("archive does not contain %q", wanted)
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == wanted {
			return
		}
	}
}

func archiveEntries(t *testing.T, name string) []string {
	t.Helper()
	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	var result []string
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return result
		}
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, header.Name)
	}
}

func assertMetadata(t *testing.T, name string, commit string) {
	t.Helper()
	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var metadata releaseMetadata
	if err := json.Unmarshal(content, &metadata); err != nil {
		t.Fatal(err)
	}
	volume := filepath.VolumeName(name)
	if metadata.Commit != commit || metadata.Go != "1.26.5" || len(metadata.Artifacts) != 2 ||
		volume != "" && strings.Contains(string(content), volume) {
		t.Fatalf("release metadata = %#v", metadata)
	}
}

func canonicalTestDirectory(t *testing.T, directory string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatalf("resolve test directory: %v", err)
	}
	return resolved
}
