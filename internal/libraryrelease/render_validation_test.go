package libraryrelease

import (
	"archive/tar"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestValidateRenderPlanRejectsContractDrift(t *testing.T) {
	t.Parallel()
	base := loadParityFixture(t, "newer").Plan
	for name, mutate := range map[string]func(*Plan){
		"schema":        func(plan *Plan) { plan.Schema++ },
		"mode":          func(plan *Plan) { plan.Mode = "production" },
		"repository":    func(plan *Plan) { plan.Repository = "../unsafe" },
		"module":        func(plan *Plan) { plan.Module = "" },
		"source scheme": func(plan *Plan) { plan.Source = "http://example.com/repository" },
		"source URL":    func(plan *Plan) { plan.Source = "https://user@example.com/repository" },
		"version":       func(plan *Plan) { plan.Version = "1.2.3" },
		"commit":        func(plan *Plan) { plan.Commit = "short" },
		"epoch":         func(plan *Plan) { plan.SourceDateEpoch = 0 },
		"compatibility": func(plan *Plan) {
			plan.CompatibilityMinimum = "bad"
		},
		"equal compatibility": func(plan *Plan) {
			plan.CompatibilityMinimum = plan.CompatibilityCurrent
		},
		"no required files": func(plan *Plan) { plan.RequiredFiles = nil },
		"unsorted required files": func(plan *Plan) {
			slices.Reverse(plan.RequiredFiles)
		},
		"unsafe required file": func(plan *Plan) {
			plan.RequiredFiles[0] = "../LICENSE"
		},
		"duplicate required file": func(plan *Plan) {
			plan.RequiredFiles[1] = plan.RequiredFiles[0]
		},
		"wrong required contract": func(plan *Plan) {
			plan.RequiredFiles = plan.RequiredFiles[1:]
		},
		"artifacts": func(plan *Plan) { plan.Artifacts = plan.Artifacts[1:] },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			plan := base
			plan.RequiredFiles = slices.Clone(base.RequiredFiles)
			plan.Artifacts = slices.Clone(base.Artifacts)
			mutate(&plan)
			if err := validateRenderPlan(plan); err == nil {
				t.Fatal("validateRenderPlan() error = nil")
			}
		})
	}
}

func TestArchiveEntryValidatesModesPathsAndSymlinks(t *testing.T) {
	t.Parallel()
	epoch := time.Unix(1_700_000_000, 0).UTC()
	for _, test := range []struct {
		mode     string
		name     string
		content  string
		typeflag byte
		wantErr  bool
	}{
		{mode: "100644", name: "file.go", content: "package fixture", typeflag: tar.TypeReg},
		{mode: "100755", name: "script", content: "run", typeflag: tar.TypeReg},
		{mode: "120000", name: "link", content: "file.go", typeflag: tar.TypeSymlink},
		{mode: "120000", name: "link", content: "../escape", wantErr: true},
		{mode: "120000", name: "link", content: "/absolute", wantErr: true},
		{mode: "120000", name: "link", content: `folder\file`, wantErr: true},
		{mode: "160000", name: "submodule", wantErr: true},
		{mode: "100644", name: "../escape", wantErr: true},
	} {
		header, _, err := archiveEntry("fixture/", epoch, gitTreeEntry{
			mode: test.mode, name: test.name, data: []byte(test.content),
		})
		if (err != nil) != test.wantErr {
			t.Fatalf("archiveEntry(%#v) error = %v", test, err)
		}
		if err == nil && header.Typeflag != test.typeflag {
			t.Fatalf("archiveEntry(%#v) type = %d", test, header.Typeflag)
		}
	}
}

func TestArchivePathsRejectCrossPlatformExtractionHazards(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"C:/Windows/system.ini",
		"C:relative",
		"folder/CON",
		"folder/nul.txt",
		"folder/trailing.",
		"folder/trailing ",
		"folder/control\x01.go",
		"folder/name?.go",
	} {
		if err := validateArchivePath(name); err == nil {
			t.Errorf("validateArchivePath(%q) error = nil", name)
		}
	}
	for _, target := range []string{
		"C:/Windows/system.ini",
		"C:relative",
		"../NUL",
		"folder/trailing.",
		"folder/name?.go",
	} {
		if err := validateSymlink("nested/link", target); err == nil {
			t.Errorf("validateSymlink(%q) error = nil", target)
		}
	}
	if err := validateSymlink("nested/link", "../portable.go"); err != nil {
		t.Fatalf("validateSymlink(portable) = %v", err)
	}
}

func TestGitTreeAndBlobParsersRejectMalformedData(t *testing.T) {
	t.Parallel()
	hash := strings.Repeat("a", 40)
	valid := []byte("100644 blob " + hash + "\tfile.go\x00")
	entries, err := parseGitTree(valid)
	if err != nil || len(entries) != 1 {
		t.Fatalf("parseGitTree(valid) = %#v, %v", entries, err)
	}
	for _, content := range [][]byte{
		nil,
		make([]byte, maximumSourceTreeBytes+1),
		[]byte("100644 tree " + hash + "\tdirectory\x00"),
		[]byte("100644 blob short\tfile.go\x00"),
		[]byte("100644 blob " + hash + "\t../escape\x00"),
		append(slices.Clone(valid), valid...),
	} {
		if _, err := parseGitTree(content); err == nil {
			t.Fatalf("parseGitTree(%q) error = nil", content)
		}
	}
	entry := entries[0]
	for _, content := range [][]byte{
		[]byte("bad header\n"),
		[]byte(hash + " blob -1\n"),
		[]byte(hash + " blob 5\nshort"),
		[]byte(hash + " blob 1\nx!"),
		[]byte(hash + " blob 1\nx\ntrailing"),
	} {
		if _, err := parseGitBlobs(content, []gitTreeEntry{entry}); err == nil {
			t.Fatalf("parseGitBlobs(%q) error = nil", content)
		}
	}
	var buffer limitedBuffer
	buffer.maximum = 3
	if written, err := buffer.Write([]byte("abcdef")); err != nil || written != 6 ||
		buffer.String() != "abc" || !buffer.truncated {
		t.Fatalf("limitedBuffer = %q, %d, %v", buffer.String(), written, err)
	}
}

func TestModuleGraphRejectsInconsistentCommittedMetadata(t *testing.T) {
	t.Parallel()
	decode := func(content string) moduleMetadata {
		var metadata moduleMetadata
		if err := json.Unmarshal([]byte(content), &metadata); err != nil {
			t.Fatal(err)
		}
		return metadata
	}
	valid := decode(`{"Module":{"Path":"example.com/root"},"Require":[{"Path":"example.com/dependency","Version":"v1.0.0"}]}`)
	modules, err := selectedModules(valid)
	if err != nil || len(modules) != 1 {
		t.Fatalf("selectedModules(valid) = %#v, %v", modules, err)
	}
	for _, metadata := range []moduleMetadata{
		decode(`{"Replace":[{"Old":{"Path":"a"},"New":{"Path":"../local"}}]}`),
		decode(`{"Replace":[{"Old":{"Path":"a"},"New":{"Path":"b","Version":"v1.0.0"}},{"Old":{"Path":"a"},"New":{"Path":"c","Version":"v1.0.0"}}]}`),
		decode(`{"Require":[{"Path":"a","Version":"v1.0.0"},{"Path":"a","Version":"v1.0.0"}]}`),
	} {
		if _, err := selectedModules(metadata); err == nil {
			t.Fatal("selectedModules(invalid) error = nil")
		}
	}
	validSum := []byte("example.com/dependency v1.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n")
	if err := validateModuleSums(modules, validSum); err != nil {
		t.Fatalf("validateModuleSums(valid) = %v", err)
	}
	for _, sums := range [][]byte{
		[]byte("other v1.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n"),
		[]byte("example.com/dependency v1.0.0 h1:value\n"),
		[]byte("example.com/dependency v1.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= trailing\n"),
	} {
		if err := validateModuleSums(modules, sums); err == nil {
			t.Fatalf("validateModuleSums(%q) error = nil", sums)
		}
	}
	if _, err := parseVendorModules([]byte("# example.com/dependency v1.0.0 => ../local\n")); err == nil {
		t.Fatal("parseVendorModules(local replacement) error = nil")
	}
	for _, vendor := range [][]byte{
		[]byte("# malformed\n"),
		[]byte("# example.com/dependency not-a-version\n"),
		[]byte("# example.com/dependency => example.com/fork v1.1.0\n"),
		[]byte("# example.com/dependency v1.0.0 => example.com/fork v1.1.0\n"),
	} {
		if _, err := parseVendorModules(vendor); err == nil {
			t.Fatalf("parseVendorModules(%q) error = nil", vendor)
		}
	}
	replacedVendor := []byte(
		"# example.com/dependency v1.0.0 => example.com/fork v1.1.0\n" +
			"## explicit; go 1.26.0\n" +
			"example.com/fork/package\n" +
			"# example.com/dependency => example.com/fork v1.1.0\n",
	)
	if parsed, err := parseVendorModules(replacedVendor); err != nil || len(parsed) != 1 ||
		parsed[0].Replace != "example.com/fork v1.1.0" {
		t.Fatalf("parseVendorModules(replaced) = %#v, %v", parsed, err)
	}
	for _, actual := range [][]listedModule{
		{{Path: "example.com/dependency", Version: "v1.0.0"}, {Path: "example.com/dependency", Version: "v1.0.0"}},
		{{Path: "example.com/other", Version: "v1.0.0"}},
		{{Path: "example.com/dependency", Version: "v2.0.0"}},
		nil,
	} {
		if err := validateVendorGraph(modules, actual); err == nil {
			t.Fatal("validateVendorGraph(invalid) error = nil")
		}
	}
	item := newSPDXPackage("example.com/dependency", "v1.0.0", "example.com/fork v1.1.0")
	if len(item.ExternalRefs) != 1 {
		t.Fatalf("replacement SPDX package = %#v", item)
	}
	plan := loadParityFixture(t, "newer").Plan
	core := []listedModule{{Path: "github.com/spice-framework/spice", Version: plan.CompatibilityMinimum}}
	if err := validateCoreCompatibility(core, plan); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]listedModule{
		nil,
		{{Path: "github.com/spice-framework/spice", Version: "v9.9.9"}},
		{{Path: "github.com/spice-framework/spice", Version: plan.CompatibilityMinimum, Replace: "example.com/fork v1.0.0"}},
	} {
		if err := validateCoreCompatibility(invalid, plan); err == nil {
			t.Fatal("validateCoreCompatibility(invalid) error = nil")
		}
	}
	if err := validateCommittedCompatibility(
		plan,
		[]byte(`{"schema":1,"minimum":"v9.9.9","current":"v0.2.0"}`),
	); err == nil {
		t.Fatal("validateCommittedCompatibility(mismatch) error = nil")
	}
}

func TestRenderHelpersRejectUnsafeOrConflictingOutputs(t *testing.T) {
	t.Parallel()
	if _, err := artifactChecksums(map[string][]byte{"../unsafe": []byte("value")}); err == nil {
		t.Fatal("artifactChecksums(unsafe) error = nil")
	}
	if _, err := artifactChecksums(map[string][]byte{"checksums.txt": []byte("value")}); err == nil {
		t.Fatal("artifactChecksums(recursive) error = nil")
	}
	if _, _, err := prepareStaging(""); err == nil {
		t.Fatal("prepareStaging(empty) error = nil")
	}
	existing := filepath.Join(t.TempDir(), "existing")
	if err := os.Mkdir(existing, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareStaging(existing); err == nil {
		t.Fatal("prepareStaging(existing) error = nil")
	}
	root := t.TempDir()
	if err := writeNewArtifact(root, "artifact", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := writeNewArtifact(root, "artifact", []byte("second")); err == nil {
		t.Fatal("writeNewArtifact(existing) error = nil")
	}
	fixture := loadParityFixture(t, "newer")
	tree := fixtureTree(t, fixture.Tree)
	delete(tree.files, "LICENSE")
	if _, err := renderArtifacts(t.Context(), fixture.Plan, tree); err == nil {
		t.Fatal("renderArtifacts(missing required file) error = nil")
	}
}
