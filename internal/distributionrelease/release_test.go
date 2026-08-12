package distributionrelease

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spice-framework/development/internal/catalog"
	"github.com/spice-framework/development/internal/process"
)

func TestRenderAndVerifyDeterministicCrossPlatformDistribution(t *testing.T) {
	t.Parallel()
	fixture := newDistributionFixture(t, fixtureOptions{toolDependency: true})
	first := filepath.Join(fixture.parent, "first")
	result, err := Render(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, first)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []string{
		"checksums.txt",
		"spice-agent-coding_0.1.0-preview.4_linux_amd64.tar.gz",
		"spice-agent-coding_0.1.0-preview.4_release.json",
		"spice-agent-coding_0.1.0-preview.4_sbom.spdx.json",
		"spice-agent-coding_0.1.0-preview.4_windows_amd64.zip",
	}
	if result.Commit != fixture.commit || !slices.Equal(result.Files, wantFiles) {
		t.Fatalf("Render() = %#v", result)
	}
	if _, err := Verify(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, first); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(fixture.parent, "second")
	if _, err := Render(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, second); err != nil {
		t.Fatal(err)
	}
	for _, name := range wantFiles {
		left := readTestFile(t, filepath.Join(first, name))
		right := readTestFile(t, filepath.Join(second, name))
		if !bytes.Equal(left, right) {
			t.Fatalf("distribution artifact %q is nondeterministic", name)
		}
		if bytes.Contains(left, []byte(fixture.root)) || bytes.Contains(left, []byte("\\"+fixture.root)) {
			t.Fatalf("artifact %q contains host path", name)
		}
		if bytes.Contains(left, []byte("spice-distribution-source-")) {
			t.Fatalf("artifact %q contains renderer scratch path", name)
		}
	}
	assertTarDistribution(t, filepath.Join(first, wantFiles[1]), fixture.commit)
	assertZipDistribution(t, filepath.Join(first, wantFiles[4]), fixture.commit)
	assertReleaseMetadata(t, filepath.Join(first, wantFiles[2]), fixture.commit)
	var sbom spdxDocument
	if err := json.Unmarshal(readTestFile(t, filepath.Join(first, wantFiles[3])), &sbom); err != nil || len(sbom.Packages) != 2 || len(sbom.Relationships) != 2 {
		t.Fatalf("distribution SBOM = %#v, %v", sbom, err)
	}
	if _, err := gitBinary(t.Context(), fixture.root, 1, "show", fixture.commit+":LICENSE"); err == nil || !strings.Contains(err.Error(), "exceeds 1 bytes") {
		t.Fatalf("gitBinary(truncated) error = %v", err)
	}
	checksums := readTestFile(t, filepath.Join(first, "checksums.txt"))
	if bytes.Contains(checksums, []byte{'\r'}) || bytes.Count(checksums, []byte{'\n'}) != 4 {
		t.Fatalf("checksums are not canonical LF text: %q", checksums)
	}
	if status := strings.TrimSpace(git(t, fixture.root, "status", "--porcelain=v1", "--untracked-files=all")); status != "" {
		t.Fatalf("render changed source checkout: %q", status)
	}
}

func TestRenderToolchainRepositoryKeyedDistribution(t *testing.T) {
	t.Parallel()
	fixture := newDistributionFixture(t, fixtureOptions{repositoryName: "toolchain"})
	output := filepath.Join(fixture.parent, "toolchain-release")
	result, err := Render(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, output)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []string{
		"checksums.txt",
		"toolchain_0.1.0-preview.6_linux_amd64.tar.gz",
		"toolchain_0.1.0-preview.6_release.json",
		"toolchain_0.1.0-preview.6_sbom.spdx.json",
		"toolchain_0.1.0-preview.6_windows_amd64.zip",
	}
	if result.Commit != fixture.commit || !slices.Equal(result.Files, wantFiles) {
		t.Fatalf("Render(Toolchain) = %#v, want files %v", result, wantFiles)
	}
	if _, err := Verify(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, output); err != nil {
		t.Fatal(err)
	}
	assertToolchainTarDistribution(t, filepath.Join(output, wantFiles[1]), fixture.commit)
	var metadata releaseMetadata
	if err := json.Unmarshal(readTestFile(t, filepath.Join(output, wantFiles[2])), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Repository != "toolchain" || metadata.Module != "github.com/spice-framework/toolchain" ||
		metadata.Version != "v0.1.0-preview.6" || len(metadata.Targets) != 2 || len(metadata.Payloads) != 2 ||
		metadata.Build.Identity.VersionSymbol != "github.com/spice-framework/toolchain/internal/cli.Version" ||
		metadata.Build.Identity.CommitSymbol != "github.com/spice-framework/toolchain/internal/cli.Commit" ||
		metadata.Build.Identity.CommitValue != fixture.commit {
		t.Fatalf("Toolchain release metadata = %#v", metadata)
	}
}

func TestToolchainPreviewSixDistributionRequiresPublishedFoundation(t *testing.T) {
	t.Parallel()
	value, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	repository, err := selectRepository(value, "toolchain", "v0.1.0-preview.6")
	if err != nil {
		t.Fatal(err)
	}
	want := []catalog.ReleaseModule{{
		Path:    "github.com/spice-framework/spice",
		Version: "v0.1.0-preview.4",
	}}
	if !slices.Equal(repository.Release.RequiredModules, want) {
		t.Fatalf("Toolchain preview.6 foundation = %#v, want %#v", repository.Release.RequiredModules, want)
	}
}

func TestRenderRejectsUntrustedDistributionInputs(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		fixture fixtureOptions
		mutate  func(*distributionFixture)
		want    string
	}{
		{name: "module profile", mutate: func(value *distributionFixture) { value.options.Repository = "spice-agent" }, want: "not authorized"},
		{name: "immutable preview one version", mutate: func(value *distributionFixture) { value.options.Version = "v0.1.0-preview.1" }, want: "does not match catalog"},
		{name: "stale preview two version", mutate: func(value *distributionFixture) { value.options.Version = "v0.1.0-preview.2" }, want: "does not match catalog"},
		{name: "stale Toolchain preview three version", fixture: fixtureOptions{repositoryName: "toolchain"}, mutate: func(value *distributionFixture) { value.options.Version = "v0.1.0-preview.3" }, want: "does not match catalog"},
		{name: "stale Toolchain preview four version", fixture: fixtureOptions{repositoryName: "toolchain"}, mutate: func(value *distributionFixture) { value.options.Version = "v0.1.0-preview.4" }, want: "does not match catalog"},
		{name: "stale Toolchain preview five version", fixture: fixtureOptions{repositoryName: "toolchain"}, mutate: func(value *distributionFixture) { value.options.Version = "v0.1.0-preview.5" }, want: "does not match catalog"},
		{name: "dirty checkout", mutate: func(value *distributionFixture) { writeTestFile(t, filepath.Join(value.root, "dirty"), "dirty") }, want: "must be clean"},
		{name: "missing tag", mutate: func(value *distributionFixture) { git(t, value.root, "tag", "-d", value.options.Version) }, want: "resolve distribution tag"},
		{name: "unknown metadata", fixture: fixtureOptions{unknownMetadata: true}, want: "unknown field"},
		{name: "metadata mismatch", fixture: fixtureOptions{metadataModule: "example.invalid/wrong"}, want: "does not match catalog"},
		{name: "replace directive", fixture: fixtureOptions{replaceDirective: true}, want: "must not contain replace"},
		{name: "module mismatch", fixture: fixtureOptions{goModule: "example.invalid/wrong"}, want: "does not match catalog"},
		{name: "wrong Go directive", fixture: fixtureOptions{goDirective: "1.25.0"}, want: "requires go 1.26.0"},
		{name: "missing required module", mutate: func(value *distributionFixture) {
			value.repository.Release.RequiredModules = []catalog.ReleaseModule{{Path: "example.invalid/required", Version: "v1.0.0"}}
			replaceDistributionRepository(value)
		}, want: "must require"},
		{name: "wrong required module version", fixture: fixtureOptions{toolDependency: true}, mutate: func(value *distributionFixture) {
			value.repository.Release.RequiredModules = []catalog.ReleaseModule{{Path: "example.com/tool", Version: "v1.2.4"}}
			replaceDistributionRepository(value)
		}, want: "exact catalog version v1.2.4"},
		{name: "missing payload", fixture: fixtureOptions{omitPayload: true}, want: "required committed distribution file"},
		{name: "symlink payload", fixture: fixtureOptions{symlinkPayload: true}, want: "unsupported tree entry"},
		{name: "missing binary package", mutate: func(value *distributionFixture) {
			value.repository.Release.Binaries[0].Package = "./cmd/missing"
			replaceDistributionRepository(value)
		}, want: "build ./cmd/missing"},
		{name: "unlinked build identity", mutate: func(value *distributionFixture) {
			value.repository.Release.BuildIdentity.CommitSymbol = value.repository.Module + "/internal/buildidentity.Missing"
			replaceDistributionRepository(value)
		}, want: "has 0 exact build identity symbols"},
		{name: "payload binary collision", mutate: func(value *distributionFixture) {
			value.repository.Release.PayloadFiles[0] = "agent"
			replaceDistributionRepository(value)
		}, want: "collide"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newDistributionFixture(t, test.fixture)
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

func TestVerifyRejectsArtifactMutationAndExtras(t *testing.T) {
	t.Parallel()
	fixture := newDistributionFixture(t, fixtureOptions{})
	output := filepath.Join(fixture.parent, "release")
	if _, err := Render(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, output); err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Join(output, "spice-agent-coding_0.1.0-preview.4_release.json")
	if err := os.WriteFile(metadata, []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, output); err == nil || !strings.Contains(err.Error(), "not reproducible") {
		t.Fatalf("Verify(tampered) error = %v", err)
	}
	if err := os.Remove(metadata); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(output, "extra"), "extra")
	if _, err := Verify(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, output); err == nil || !strings.Contains(err.Error(), "do not match contract") {
		t.Fatalf("Verify(extra) error = %v", err)
	}
}

func TestDistributionReleaseBoundaryFailures(t *testing.T) {
	t.Parallel()
	fixture := newDistributionFixture(t, fixtureOptions{})
	if _, err := Render(nil, fixture.options, fixture.catalog, process.ExecRunner{}, filepath.Join(fixture.parent, "nil")); err == nil { //nolint:staticcheck // Fail-closed nil boundary.
		t.Fatal("Render(nil) error = nil")
	}
	if _, err := Render(t.Context(), fixture.options, fixture.catalog, nil, filepath.Join(fixture.parent, "runner")); err == nil {
		t.Fatal("Render(nil runner) error = nil")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Render(canceled, fixture.options, fixture.catalog, process.ExecRunner{}, filepath.Join(fixture.parent, "canceled")); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("Render(canceled) error = %v", err)
	}
	if _, err := Render(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, filepath.Join(fixture.root, "dist")); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("Render(inside) error = %v", err)
	}
	output := filepath.Join(fixture.parent, "release")
	if _, err := Render(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, output); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, output); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Render(existing) error = %v", err)
	}
	if _, err := Verify(t.Context(), fixture.options, fixture.catalog, process.ExecRunner{}, filepath.Join(fixture.parent, "missing")); err == nil || !strings.Contains(err.Error(), "artifact directory") {
		t.Fatalf("Verify(missing) error = %v", err)
	}
}

func TestPrepareOutputRejectsSymlinkAncestorIntoRepository(t *testing.T) {
	t.Parallel()
	fixture := newDistributionFixture(t, fixtureOptions{})
	indirect := filepath.Join(fixture.parent, "indirect")
	if err := os.Symlink(fixture.root, indirect); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, os.ErrPermission) {
			t.Skipf("creating a Windows symlink requires developer mode or privilege: %v", err)
		}
		t.Fatal(err)
	}
	configured := filepath.Join(indirect, "docs", "release")
	if _, _, err := prepareOutput(fixture.root, configured); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("prepareOutput(symlink ancestor) error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(fixture.root, "docs", "release")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe output was created through symlink ancestor: %v", err)
	}
}

func TestBuildUsesMaterializedTaggedSource(t *testing.T) {
	t.Parallel()
	fixture := newDistributionFixture(t, fixtureOptions{})
	tree, err := requirePortableTree(t.Context(), fixture.root, fixture.commit)
	if err != nil {
		t.Fatal(err)
	}
	scratchRoot, sourceRoot, err := materializeTaggedTree(t.Context(), fixture.root, fixture.commit, tree)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(scratchRoot); err != nil {
			t.Errorf("remove materialized source fixture: %v", err)
		}
	})
	goExecutable, err := resolveGoExecutable()
	if err != nil {
		t.Fatal(err)
	}
	if err := requireGoRuntime(t.Context(), sourceRoot, goExecutable, scratchRoot); err != nil {
		t.Fatal(err)
	}
	modules, err := requireModuleGraph(
		t.Context(),
		sourceRoot,
		fixture.repository,
		goExecutable,
		scratchRoot,
	)
	if err != nil {
		t.Fatal(err)
	}
	fixture.repository.Release.Binaries = fixture.repository.Release.Binaries[:1]
	prepared := preparedRelease{
		root: fixture.root, sourceRoot: sourceRoot, scratchRoot: scratchRoot,
		goExecutable: goExecutable, repository: fixture.repository,
		commit: fixture.commit, epoch: 1_700_000_000, modules: modules,
	}
	writeTestFile(t, filepath.Join(fixture.root, "cmd", "agent", "main.go"), "@Application\n")
	binaries, err := buildTarget(t.Context(), prepared, catalogHostTarget())
	if err != nil {
		t.Fatal(err)
	}
	if len(binaries) != 1 {
		t.Fatalf("materialized build binaries = %v", binaries)
	}
	materialized := readTestFile(t, filepath.Join(sourceRoot, "cmd", "agent", "main.go"))
	if bytes.Contains(materialized, []byte("@Application")) {
		t.Fatal("materialized source followed mutable checkout")
	}
}

func TestDistributionPureValidation(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "../x", "/x", `a\\b`, "AUX.txt", "bad?.txt", "x./y"} {
		if err := portablePath(value); err == nil {
			t.Errorf("portablePath(%q) error = nil", value)
		}
	}
	if err := portablePath("docs/security.md"); err != nil {
		t.Fatal(err)
	}
	const objectID = "0123456789abcdef0123456789abcdef01234567"
	validTree := []byte(
		"100644 blob " + objectID + "\tREADME.md\x00" +
			"100755 blob " + objectID + "\tcmd/tool/main.go\x00",
	)
	if tree, err := parsePortableTree(validTree); err != nil || len(tree) != 2 {
		t.Fatalf("parsePortableTree(valid) = %v, %v", tree, err)
	}
	for _, content := range [][]byte{
		nil,
		[]byte("120000 blob " + objectID + "\tlink\x00"),
		[]byte("160000 commit " + objectID + "\tmodule\x00"),
		[]byte("100644 blob " + objectID + "\tDir/a\x00100644 blob " + objectID + "\tdir/b\x00"),
		[]byte("100644 blob " + objectID + "\tbad?.go\x00"),
		[]byte("100644 blob " + objectID + "\tfile\x00100644 blob " + objectID + "\tfile\x00"),
		[]byte("100644 blob " + objectID + "\tdirectory\x00100644 blob " + objectID + "\tdirectory/file\x00"),
	} {
		if _, err := parsePortableTree(content); err == nil {
			t.Fatalf("parsePortableTree(%q) error = nil", content)
		}
	}
	var substituted bytes.Buffer
	substitutedWriter := tar.NewWriter(&substituted)
	substitutedContent := []byte("substituted\n")
	if err := substitutedWriter.WriteHeader(&tar.Header{
		Name: "README.md", Mode: 0o644, Size: int64(len(substitutedContent)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := substitutedWriter.Write(substitutedContent); err != nil {
		t.Fatal(err)
	}
	if err := substitutedWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractMaterializedTree(t.TempDir(), substituted.Bytes(), map[string]treeEntry{
		"README.md": {mode: "100644", object: objectID},
	}); err == nil || !strings.Contains(err.Error(), "Git object") {
		t.Fatalf("extractMaterializedTree(substituted) error = %v", err)
	}
	if _, err := gitBlobHasher("short", 1); err == nil {
		t.Fatal("gitBlobHasher(short) error = nil")
	}
	if sha256Hasher, err := gitBlobHasher(strings.Repeat("0", 64), 1); err != nil || len(sha256Hasher.Sum(nil)) != 32 {
		t.Fatalf("gitBlobHasher(SHA-256) = %v, %v", sha256Hasher, err)
	}
	materializedContent := []byte("exact\n")
	materializedObject := testBlobObject(t, materializedContent)
	materializedTree := map[string]treeEntry{
		"bin/tool": {mode: "100755", object: materializedObject},
	}
	validMaterializedArchive := testTarArchive(t, []testTarEntry{{
		header:  tar.Header{Name: "bin/tool", Mode: 0o755, Size: int64(len(materializedContent)), Typeflag: tar.TypeReg},
		content: materializedContent,
	}})
	materializedRoot := t.TempDir()
	if err := extractMaterializedTree(materializedRoot, validMaterializedArchive, materializedTree); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(materializedRoot, "bin", "tool")); err != nil ||
		(runtime.GOOS != "windows" && info.Mode().Perm() != 0o700) {
		t.Fatalf("materialized executable info = %#v, %v", info, err)
	}
	for name, archive := range map[string][]byte{
		"unexpected directory": testTarArchive(t, []testTarEntry{{
			header: tar.Header{Name: "other/", Typeflag: tar.TypeDir},
		}}),
		"unsupported entry": testTarArchive(t, []testTarEntry{{
			header: tar.Header{Name: "bin/tool", Typeflag: tar.TypeSymlink, Linkname: "elsewhere"},
		}}),
		"missing file":  testTarArchive(t, nil),
		"malformed tar": []byte("not a tar archive"),
		"duplicate file": testTarArchive(t, []testTarEntry{
			{
				header:  tar.Header{Name: "bin/tool", Mode: 0o755, Size: int64(len(materializedContent)), Typeflag: tar.TypeReg},
				content: materializedContent,
			},
			{
				header:  tar.Header{Name: "bin/tool", Mode: 0o755, Size: int64(len(materializedContent)), Typeflag: tar.TypeReg},
				content: materializedContent,
			},
		}),
	} {
		t.Run("materialized "+name, func(t *testing.T) {
			t.Parallel()
			if err := extractMaterializedTree(t.TempDir(), archive, materializedTree); err == nil {
				t.Fatal("extractMaterializedTree() error = nil")
			}
		})
	}
	invalidObjectArchive := testTarArchive(t, []testTarEntry{{
		header:  tar.Header{Name: "bin/tool", Mode: 0o755, Size: int64(len(materializedContent)), Typeflag: tar.TypeReg},
		content: materializedContent,
	}})
	if err := extractMaterializedTree(t.TempDir(), invalidObjectArchive, map[string]treeEntry{
		"bin/tool": {mode: "100755", object: "short"},
	}); err == nil || !strings.Contains(err.Error(), "identity verification") {
		t.Fatalf("extractMaterializedTree(invalid object) error = %v", err)
	}
	for name, content := range map[string]string{
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
	buffer := boundedBuffer{maximum: 2}
	if written, err := buffer.Write([]byte("long")); err != nil || written != 4 || !buffer.truncated || buffer.String() != "lo" {
		t.Fatalf("bounded buffer content=%q truncated=%t, written=%d, error=%v", buffer.String(), buffer.truncated, written, err)
	}
	if written, err := buffer.Write([]byte("more")); err != nil || written != 4 {
		t.Fatalf("bounded full write = %d, %v", written, err)
	}
	concurrent := boundedBuffer{maximum: 32}
	var writers sync.WaitGroup
	for range 8 {
		writers.Go(func() {
			_, _ = concurrent.Write([]byte("12345678"))
		})
	}
	writers.Wait()
	if len(concurrent.Bytes()) != concurrent.maximum || !concurrent.truncated {
		t.Fatalf("concurrent bounded buffer size=%d truncated=%t", len(concurrent.Bytes()), concurrent.truncated)
	}
	scratch := t.TempDir()
	environment := distributionBuildEnvironment([]string{
		"PATH=hostile", "SYSTEMROOT=kept", "CGO_ENABLED=1", "goos=plan9", "GOARCH=386", "GOENV=host.json",
		"GOFLAGS=-race", "GOPRIVATE=secret.example", "GONOPROXY=secret.example",
		"GONOSUMDB=secret.example", "GOPROXY=https://proxy.example", "GOSUMDB=sum.example",
		"GOTOOLCHAIN=auto", "GOWORK=host.work", "GOCACHE=host-cache", "GOMODCACHE=host-mod",
		"GOPATH=host-path", "GOTMPDIR=host-tmp", "GOAMD64=v4", "GOARM64=v9.5",
		"GOEXPERIMENT=host", "GOTELEMETRY=on", "GOVCS=all", "CC=host-cc", "CXX=host-cxx",
		"AR=host-ar", "PKG_CONFIG=host-pkg-config",
	}, scratch, catalog.ReleaseTarget{GOOS: "linux", GOARCH: "arm64"})
	wantEnvironment := map[string]string{
		"SYSTEMROOT": "kept", "CGO_ENABLED": "0", "GO111MODULE": "on",
		"GOARCH": "arm64", "GOENV": "off", "GOEXPERIMENT": "", "GOFLAGS": "",
		"GOCACHE": filepath.Join(scratch, "gocache"), "GOMODCACHE": filepath.Join(scratch, "gomodcache"),
		"GONOPROXY": "", "GONOSUMDB": "", "GOOS": "linux", "GOPRIVATE": "",
		"GOPATH": filepath.Join(scratch, "gopath"), "GOPROXY": "off", "GOSUMDB": "off",
		"GOTELEMETRY": "off", "GOTOOLCHAIN": "local", "GOTMPDIR": filepath.Join(scratch, "gotmp"),
		"GOVCS": "off", "GOWORK": "off", "GOARM64": "v8.0",
	}
	counts := make(map[string]int)
	for _, entry := range environment {
		name, value, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		if wanted, found := wantEnvironment[upper]; found {
			counts[upper]++
			if value != wanted {
				t.Errorf("environment %s = %q, want %q", upper, value, wanted)
			}
		}
	}
	for name := range wantEnvironment {
		if counts[name] != 1 {
			t.Errorf("environment %s count = %d", name, counts[name])
		}
	}
	if len(environment) != len(wantEnvironment) {
		t.Errorf("closed environment contains unexpected values: %v", environment)
	}
	amd64Environment := distributionBuildEnvironment(nil, scratch, catalog.ReleaseTarget{GOOS: "linux", GOARCH: "amd64"})
	if !slices.Contains(amd64Environment, "GOAMD64=v1") || slices.Contains(amd64Environment, "GOARM64=v8.0") {
		t.Errorf("amd64 architecture baseline = %v", amd64Environment)
	}
	if err := validateGoVersionOutput("go version go1.26.4 " + runtime.GOOS + "/" + runtime.GOARCH + "\n"); err == nil || !strings.Contains(err.Error(), "1.26.5") {
		t.Fatalf("validateGoVersionOutput(old) error = %v", err)
	}
	if err := validateGoVersionOutput("prefix go version go1.26.5 " + runtime.GOOS + "/" + runtime.GOARCH + "\n"); err == nil {
		t.Fatal("validateGoVersionOutput(prefixed) error = nil")
	}
	if err := validateGoVersionOutput("go version go1.26.5 " + runtime.GOOS + "/" + runtime.GOARCH + "\n"); err != nil {
		t.Fatal(err)
	}
	if executable, err := resolveGoExecutable(); err != nil || !filepath.IsAbs(executable) {
		t.Fatalf("resolveGoExecutable() = %q, %v", executable, err)
	}
	for _, value := range []string{"file:///tmp/repo", "https://user@example.com/repo", "ssh://user@example.com/repo", "https://example.com/../repo", "https://example.com:443/repo", "ssh://git@example.com:22/repo"} {
		if _, err := remoteIdentity(value); err == nil {
			t.Errorf("remoteIdentity(%q) error = nil", value)
		}
	}
	if identity, err := remoteIdentity("git@github.com:spice-framework/spice-agent-coding.git"); err != nil || identity != "github.com/spice-framework/spice-agent-coding" {
		t.Fatalf("remoteIdentity(SCP) = %q, %v", identity, err)
	}
	directory := t.TempDir()
	if _, err := realDirectory("", "test"); err == nil {
		t.Fatal("realDirectory(empty) error = nil")
	}
	file := filepath.Join(directory, "file")
	writeTestFile(t, file, "content")
	if _, err := realDirectory(file, "test"); err == nil {
		t.Fatal("realDirectory(file) error = nil")
	}
	if _, err := readBounded(file, 1); err == nil {
		t.Fatal("readBounded(oversized) error = nil")
	}
	if err := writeArtifact(directory, "../unsafe", []byte("x")); err == nil {
		t.Fatal("writeArtifact(unsafe) error = nil")
	}
	if err := writeArtifact(directory, "artifact", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := writeArtifact(directory, "artifact", []byte("x")); err == nil {
		t.Fatal("writeArtifact(existing) error = nil")
	}
	if err := requireArtifactSize("large.tar.gz", int(maximumArtifactBytes)+1); err == nil {
		t.Fatal("requireArtifactSize(oversized) error = nil")
	}
	archiveBuffer := limitedArchiveBuffer{maximum: 2}
	if written, err := archiveBuffer.Write([]byte("long")); written != 2 || err == nil || string(archiveBuffer.Bytes()) != "lo" {
		t.Fatalf("limited archive write = %d, %v, %q", written, err, archiveBuffer.Bytes())
	}
	if written, err := archiveBuffer.Write([]byte("x")); written != 0 || err == nil {
		t.Fatalf("full limited archive write = %d, %v", written, err)
	}
	var decoded intent
	if err := decodeStrict([]byte(`{"schema":1} trailing`), &decoded); err == nil {
		t.Fatal("decodeStrict(trailing) error = nil")
	}
	value, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "unknown", "starter-smtp"} {
		if _, err := selectRepository(value, name, "v0.1.0-preview.4"); err == nil {
			t.Errorf("selectRepository(%q) error = nil", name)
		}
	}
}

type fixtureOptions struct {
	repositoryName   string
	unknownMetadata  bool
	metadataModule   string
	replaceDirective bool
	omitPayload      bool
	symlinkPayload   bool
	toolDependency   bool
	goModule         string
	goDirective      string
}

type distributionFixture struct {
	parent     string
	root       string
	commit     string
	options    Options
	catalog    catalog.Catalog
	repository catalog.Repository
}

func newDistributionFixture(t *testing.T, options fixtureOptions) distributionFixture {
	t.Helper()
	value, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	repositoryName := options.repositoryName
	if repositoryName == "" {
		repositoryName = "spice-agent-coding"
	}
	index := slices.IndexFunc(value.Repositories, func(repository catalog.Repository) bool {
		return repository.Name == repositoryName
	})
	if index < 0 {
		t.Fatalf("fixture repository %q is absent", repositoryName)
	}
	repository := value.Repositories[index]
	repository.Release.RequiredModules = nil
	if options.toolDependency {
		repository.Release.RequiredModules = []catalog.ReleaseModule{{Path: "example.com/tool", Version: "v1.2.3"}}
	}
	if repositoryName == "toolchain" {
		repository.Release.Binaries = []catalog.ReleaseBinary{{Name: "spice", Package: "./cmd/spice"}}
	} else {
		repository.Release.Binaries = []catalog.ReleaseBinary{
			{Name: "agent", Package: "./cmd/agent"},
			{Name: "agentd", Package: "./cmd/agentd"},
		}
	}
	repository.Release.Targets = []catalog.ReleaseTarget{
		{GOOS: "linux", GOARCH: "amd64"},
		{GOOS: "windows", GOARCH: "amd64"},
	}
	if repositoryName == "toolchain" {
		repository.Release.PayloadFiles = []string{"LICENSE", "README.md"}
		repository.Release.BuildIdentity = &catalog.ReleaseBuildIdentity{
			VersionSymbol: repository.Module + "/internal/cli.Version",
			CommitSymbol:  repository.Module + "/internal/cli.Commit",
		}
	} else {
		repository.Release.PayloadFiles = []string{"LICENSE", "docs/configuration.md", "protocol-descriptors.pb"}
		repository.Release.BuildIdentity = &catalog.ReleaseBuildIdentity{
			VersionSymbol: repository.Module + "/internal/buildidentity.Version",
			CommitSymbol:  repository.Module + "/internal/buildidentity.Commit",
		}
	}
	if options.symlinkPayload {
		repository.Release.PayloadFiles = []string{"link"}
	}
	value.Repositories[index] = repository
	parent := canonicalTestDirectory(t, t.TempDir())
	root := filepath.Join(parent, "repository")
	metadataModule := repository.Module
	if options.metadataModule != "" {
		metadataModule = options.metadataModule
	}
	metadata := `{"schema":1,"profile":"go-distribution-v1","repository":"` + repository.Name + `","module":"` + metadataModule + `","version":"` + repository.Release.Version + `"}`
	if options.unknownMetadata {
		metadata = strings.TrimSuffix(metadata, "}") + `,"unknown":true}`
	}
	goModule := repository.Module
	if options.goModule != "" {
		goModule = options.goModule
	}
	goDirective := "1.26.0"
	if options.goDirective != "" {
		goDirective = options.goDirective
	}
	modfile := "module " + goModule + "\n\ngo " + goDirective + "\n\ntoolchain go1.26.5\n"
	if options.toolDependency {
		modfile += "\nrequire example.com/tool v1.2.3 // indirect\n\ntool example.com/tool/cmd/tool\n"
	}
	if options.replaceDirective {
		modfile += "\nreplace example.invalid/old => example.invalid/new v1.0.0\n"
	}
	files := map[string][]byte{
		"LICENSE":                            []byte("Apache-2.0 fixture\n"),
		"README.md":                          []byte("# fixture\n"),
		"THIRD_PARTY_NOTICES.md":             []byte("none\n"),
		"agent":                              []byte("collision fixture\n"),
		"cmd/agent/main.go":                  []byte("package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\n\t\"" + repository.Module + "/internal/buildidentity\"\n)\n\nfunc main() { if len(os.Args) == 2 && os.Args[1] == \"--version\" { fmt.Printf(\"agent %s (%s)\\n\", buildidentity.Version, buildidentity.Commit) } }\n"),
		"cmd/agentd/main.go":                 []byte("package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\n\t\"" + repository.Module + "/internal/buildidentity\"\n)\n\nfunc main() { if len(os.Args) == 2 && os.Args[1] == \"--version\" { fmt.Printf(\"agentd %s (%s)\\n\", buildidentity.Version, buildidentity.Commit) } }\n"),
		"docs/configuration.md":              []byte("# config\n"),
		"docs/installation.md":               []byte("# install\n"),
		"docs/security.md":                   []byte("# security\n"),
		"go.mod":                             []byte(modfile),
		"go.sum":                             nil,
		"internal/buildidentity/identity.go": []byte("package buildidentity\n\nvar Version = \"development\"\nvar Commit = \"development\"\n"),
		"link":                               []byte("../outside"),
		"protocol-descriptors.pb":            {0x00, 0x01, 0xff, 0x0a},
		"spice-release.json":                 []byte(metadata + "\n"),
		"vendor/modules.txt":                 nil,
	}
	if repositoryName == "toolchain" {
		files["cmd/spice/main.go"] = []byte("package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\n\t\"" + repository.Module + "/internal/cli\"\n)\n\nfunc main() { if len(os.Args) == 2 && os.Args[1] == \"--version\" { fmt.Printf(\"spice %s (%s)\\n\", cli.Version, cli.Commit) } }\n")
		files["internal/cli/identity.go"] = []byte("package cli\n\nvar Version = \"development\"\nvar Commit = \"development\"\n")
	}
	if options.toolDependency {
		files["vendor/modules.txt"] = []byte("# example.com/tool v1.2.3\n## explicit; go 1.26.0\nexample.com/tool/cmd/tool\n")
		files["vendor/example.com/tool/cmd/tool/main.go"] = []byte("package main\n\nfunc main() {}\n")
	}
	if options.omitPayload {
		delete(files, "docs/configuration.md")
	}
	for name, content := range files {
		writeTestBytes(t, filepath.Join(root, filepath.FromSlash(name)), content)
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
	if options.symlinkPayload {
		hash := strings.TrimSpace(git(t, root, "hash-object", "-w", "link"))
		git(t, root, "update-index", "--add", "--cacheinfo", "120000,"+hash+",link")
	}
	command := exec.Command("git", "commit", "-m", "fixture")
	command.Dir = root
	date := time.Unix(1_700_000_000, 0).UTC().Format(time.RFC3339)
	command.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if output, commitErr := command.CombinedOutput(); commitErr != nil {
		t.Fatalf("git commit: %v\n%s", commitErr, output)
	}
	if options.symlinkPayload {
		if err := os.Remove(filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
		git(t, root, "checkout", "--", "link")
	}
	commit := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	git(t, root, "tag", repository.Release.Version)
	return distributionFixture{
		parent: parent, root: root, commit: commit, catalog: value, repository: repository,
		options: Options{Root: root, Repository: repository.Name, Version: repository.Release.Version},
	}
}

func replaceDistributionRepository(fixture *distributionFixture) {
	index := slices.IndexFunc(fixture.catalog.Repositories, func(repository catalog.Repository) bool {
		return repository.Name == fixture.repository.Name
	})
	fixture.catalog.Repositories[index] = fixture.repository
}

func writeTestFile(t *testing.T, name string, content string) {
	t.Helper()
	writeTestBytes(t, name, []byte(content))
}

func writeTestBytes(t *testing.T, name string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return content
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

func canonicalTestDirectory(t *testing.T, directory string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatalf("resolve test directory: %v", err)
	}
	return resolved
}

func assertTarDistribution(t *testing.T, name string, commit string) {
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
	reader := tar.NewReader(gzipReader)
	want := map[string]bool{
		"spice-agent-coding_0.1.0-preview.4_linux_amd64/agent":                   false,
		"spice-agent-coding_0.1.0-preview.4_linux_amd64/agentd":                  false,
		"spice-agent-coding_0.1.0-preview.4_linux_amd64/LICENSE":                 false,
		"spice-agent-coding_0.1.0-preview.4_linux_amd64/docs/configuration.md":   false,
		"spice-agent-coding_0.1.0-preview.4_linux_amd64/protocol-descriptors.pb": false,
	}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, found := want[header.Name]; !found || header.Typeflag != tar.TypeReg || strings.Contains(header.Name, "\\") {
			t.Fatalf("unexpected tar entry %#v", header)
		}
		want[header.Name] = true
		if strings.HasSuffix(header.Name, "/agent") || strings.HasSuffix(header.Name, "/agentd") {
			if header.Mode != 0o755 {
				t.Fatalf("binary mode = %o", header.Mode)
			}
			content, err := io.ReadAll(reader)
			if err != nil || len(content) < 4 || !bytes.Equal(content[:4], []byte{0x7f, 'E', 'L', 'F'}) {
				t.Fatalf("Unix binary is not ELF: %x, %v", content[:min(4, len(content))], err)
			}
			assertBinaryIdentity(t, content, commit)
		}
	}
	for entry, found := range want {
		if !found {
			t.Errorf("tar missing %q", entry)
		}
	}
}

func assertToolchainTarDistribution(t *testing.T, name string, commit string) {
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
	reader := tar.NewReader(gzipReader)
	want := map[string]bool{
		"toolchain_0.1.0-preview.6_linux_amd64/spice":     false,
		"toolchain_0.1.0-preview.6_linux_amd64/LICENSE":   false,
		"toolchain_0.1.0-preview.6_linux_amd64/README.md": false,
	}
	for {
		header, readErr := reader.Next()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, found := want[header.Name]; !found || header.Typeflag != tar.TypeReg {
			t.Fatalf("unexpected Toolchain tar entry %#v", header)
		}
		want[header.Name] = true
		if strings.HasSuffix(header.Name, "/spice") {
			content, readErr := io.ReadAll(reader)
			if readErr != nil || header.Mode != 0o755 || len(content) < 4 ||
				!bytes.Equal(content[:4], []byte{0x7f, 'E', 'L', 'F'}) ||
				!bytes.Contains(content, []byte("0.1.0-preview.6")) ||
				!bytes.Contains(content, []byte(commit)) {
				t.Fatalf("Toolchain binary identity or format is invalid: mode=%o error=%v", header.Mode, readErr)
			}
		}
	}
	for entry, found := range want {
		if !found {
			t.Errorf("Toolchain tar missing %q", entry)
		}
	}
}

func assertZipDistribution(t *testing.T, name string, commit string) {
	t.Helper()
	reader, err := zip.OpenReader(name)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	want := map[string]bool{
		"spice-agent-coding_0.1.0-preview.4_windows_amd64/agent.exe":               false,
		"spice-agent-coding_0.1.0-preview.4_windows_amd64/agentd.exe":              false,
		"spice-agent-coding_0.1.0-preview.4_windows_amd64/LICENSE":                 false,
		"spice-agent-coding_0.1.0-preview.4_windows_amd64/docs/configuration.md":   false,
		"spice-agent-coding_0.1.0-preview.4_windows_amd64/protocol-descriptors.pb": false,
	}
	for _, file := range reader.File {
		if _, found := want[file.Name]; !found || file.FileInfo().IsDir() || strings.Contains(file.Name, "\\") {
			t.Fatalf("unexpected zip entry %#v", file.FileHeader)
		}
		want[file.Name] = true
		if strings.HasSuffix(file.Name, ".exe") {
			content := readZipFile(t, file)
			if len(content) < 2 || !bytes.Equal(content[:2], []byte{'M', 'Z'}) || file.Mode().Perm() != 0o755 {
				t.Fatalf("Windows binary is invalid: mode=%o content=%x", file.Mode().Perm(), content[:min(2, len(content))])
			}
			assertBinaryIdentity(t, content, commit)
		}
	}
	for entry, found := range want {
		if !found {
			t.Errorf("zip missing %q", entry)
		}
	}
}

func assertBinaryIdentity(t *testing.T, content []byte, commit string) {
	t.Helper()
	if !bytes.Contains(content, []byte("0.1.0-preview.4")) || !bytes.Contains(content, []byte(commit)) {
		t.Fatal("built binary does not contain the injected version and commit identity")
	}
}

func readZipFile(t *testing.T, file *zip.File) []byte {
	t.Helper()
	reader, err := file.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

type testTarEntry struct {
	header  tar.Header
	content []byte
}

func testTarArchive(t *testing.T, entries []testTarEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := tar.NewWriter(&output)
	for index := range entries {
		if err := writer.WriteHeader(&entries[index].header); err != nil {
			t.Fatal(err)
		}
		if len(entries[index].content) != 0 {
			if _, err := writer.Write(entries[index].content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func testBlobObject(t *testing.T, content []byte) string {
	t.Helper()
	hasher, err := gitBlobHasher(strings.Repeat("0", 40), int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hasher.Write(content); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func assertReleaseMetadata(t *testing.T, name string, commit string) {
	t.Helper()
	var metadata releaseMetadata
	if err := json.Unmarshal(readTestFile(t, name), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Commit != commit || metadata.Go != "1.26.5" || metadata.Toolchain != "go1.26.5" ||
		metadata.Build.ModuleMode != "vendor" || metadata.Build.CGOEnabled || !metadata.Build.Trimpath ||
		metadata.Build.Environment != "closed" || !metadata.Build.CacheIsolation ||
		metadata.Build.Source != "materialized-tagged-commit" ||
		metadata.Build.GOAMD64 != "v1" || metadata.Build.GOARM64 != "v8.0" ||
		metadata.Build.Identity.VersionSymbol != "github.com/spice-framework/spice-agent-coding/internal/buildidentity.Version" ||
		metadata.Build.Identity.VersionValue != "0.1.0-preview.4" ||
		metadata.Build.Identity.CommitSymbol != "github.com/spice-framework/spice-agent-coding/internal/buildidentity.Commit" ||
		metadata.Build.Identity.CommitValue != commit ||
		len(metadata.Targets) != 2 || len(metadata.Payloads) != 3 || len(metadata.Artifacts) != 3 {
		t.Fatalf("release metadata = %#v", metadata)
	}
}
