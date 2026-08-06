package libraryrelease

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spice-framework/development/internal/catalog"
)

type parityFixture struct {
	FixtureSchema     int               `json:"fixture_schema"`
	Generation        string            `json:"generation"`
	LegacyRepository  string            `json:"legacy_repository"`
	ExactLegacyParity parityExpectation `json:"exact_legacy_parity"`
	KnownDifferences  []string          `json:"known_differences"`
	Plan              Plan              `json:"plan"`
	Tree              []fixtureEntry    `json:"tree"`
	ExpectedSHA256    map[string]string `json:"expected_sha256"`
}

type parityExpectation struct {
	Archive   bool `json:"archive"`
	SBOM      bool `json:"sbom"`
	Checksums bool `json:"checksums"`
}

type fixtureEntry struct {
	Mode    string `json:"mode"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type legacyParityOracle struct {
	Generation         string            `json:"generation"`
	BuilderRepository  string            `json:"builder_repository"`
	BuilderCommit      string            `json:"builder_commit"`
	BuilderReleaseTree string            `json:"builder_release_tree"`
	BuilderPackage     string            `json:"builder_package"`
	GoVersion          string            `json:"go_version"`
	Platform           string            `json:"platform"`
	HarnessSHA256      string            `json:"harness_sha256"`
	FixtureCommit      string            `json:"fixture_commit"`
	Version            string            `json:"version"`
	SourceDateEpoch    int64             `json:"source_date_epoch"`
	OutputSHA256       map[string]string `json:"legacy_output_sha256"`
}

func TestRendererGoldenParityContracts(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"older", "newer"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := loadParityFixture(t, name)
			oracle := loadLegacyParityOracle(t, name)
			if fixture.FixtureSchema != 2 || fixture.Generation == "" ||
				fixture.LegacyRepository != fixture.Plan.Repository ||
				len(fixture.KnownDifferences) == 0 {
				t.Fatalf("invalid parity fixture metadata: %#v", fixture)
			}
			assertLegacyOracleProvenance(t, fixture, oracle)
			tree := fixtureTree(t, fixture.Tree)
			first, err := renderArtifacts(t.Context(), fixture.Plan, tree)
			if err != nil {
				t.Fatal(err)
			}
			second, err := renderArtifacts(t.Context(), fixture.Plan, tree)
			if err != nil {
				t.Fatal(err)
			}
			if !equalArtifactBytes(first, second) {
				t.Fatal("repeated pure rendering produced different bytes")
			}
			actualHashes := artifactHashes(first)
			if !equalStringMap(actualHashes, fixture.ExpectedSHA256) {
				t.Fatalf("golden hashes = %#v", actualHashes)
			}
			for artifact, centralHash := range actualHashes {
				legacyHash, found := oracle.OutputSHA256[artifact]
				if !found {
					t.Fatalf("legacy oracle has no hash for %q", artifact)
				}
				wantExact := exactLegacyParityForArtifact(t, fixture.ExactLegacyParity, artifact)
				if exact := centralHash == legacyHash; exact != wantExact {
					t.Fatalf(
						"%s centralized/legacy hash parity = %t, require %t (central %s, legacy %s)",
						artifact,
						exact,
						wantExact,
						centralHash,
						legacyHash,
					)
				}
			}
			archiveName := fixture.Plan.Repository + "_1.2.3_source.tar.gz"
			assertArchiveContract(t, first[archiveName], fixture.Plan, len(tree.entries))
		})
	}
}

func TestRenderUsesCommittedObjectsAndGuardsOutput(t *testing.T) {
	t.Parallel()
	fixture := loadParityFixture(t, "newer")
	repository := t.TempDir()
	writeFixtureRepository(t, repository, fixture.Tree)
	gitCommand(t, repository, "init")
	gitCommand(t, repository, "config", "user.name", "Spice Test")
	gitCommand(t, repository, "config", "user.email", "spice@example.invalid")
	gitCommand(t, repository, "remote", "add", "origin", "https://github.com/spice-framework/starter-oidc.git")
	gitCommand(t, repository, "add", ".")
	command := exec.Command("git", "commit", "-m", "fixture")
	command.Dir = repository
	command.Env = append(
		os.Environ(),
		"GIT_AUTHOR_DATE=2023-11-14T22:13:20Z",
		"GIT_COMMITTER_DATE=2023-11-14T22:13:20Z",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, output)
	}
	fixture.Plan.Commit = strings.TrimSpace(gitCommand(t, repository, "rev-parse", "HEAD"))
	if err := os.WriteFile(
		filepath.Join(repository, "oidc.go"),
		[]byte("package oidc\n\n// dirty working tree content\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	firstPath, secondPath := filepath.Join(parent, "first"), filepath.Join(parent, "second")
	value := catalogForRenderFixture(t, fixture.Plan.CompatibilityCurrent)
	first, err := Render(t.Context(), repository, firstPath, fixture.Plan, value)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(t.Context(), repository, secondPath, fixture.Plan, value)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(first.Files, fixture.Plan.Artifacts) || !slices.Equal(second.Files, fixture.Plan.Artifacts) {
		t.Fatalf("render results = %#v, %#v", first, second)
	}
	if !equalStringMap(directoryHashes(t, firstPath), directoryHashes(t, secondPath)) {
		t.Fatal("repeated committed-object renders differ")
	}
	archive, err := os.ReadFile(filepath.Join(firstPath, "starter-oidc_1.2.3_source.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	archivedSource := archiveFile(t, archive, "starter-oidc_1.2.3/oidc.go")
	if !bytes.Contains(archivedSource, []byte("func Validate")) ||
		bytes.Contains(archivedSource, []byte("dirty working tree")) {
		t.Fatalf("archived source = %q", archivedSource)
	}
	if _, err := Render(t.Context(), repository, firstPath, fixture.Plan, value); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Render(existing output) error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Render(canceled, repository, filepath.Join(parent, "canceled"), fixture.Plan, value); err == nil {
		t.Fatal("Render(canceled) error = nil")
	}
	wrongEpoch := fixture.Plan
	wrongEpoch.SourceDateEpoch++
	if _, err := Render(t.Context(), repository, filepath.Join(parent, "wrong-epoch"), wrongEpoch, value); err == nil ||
		!strings.Contains(err.Error(), "commit epoch") {
		t.Fatalf("Render(wrong epoch) error = %v", err)
	}
}

func TestParsePlanRejectsUnknownTrailingAndProductionInput(t *testing.T) {
	t.Parallel()
	fixture := loadParityFixture(t, "newer")
	content, err := json.Marshal(fixture.Plan)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ParsePlan(content)
	if err != nil || plan.Commit != fixture.Plan.Commit {
		t.Fatalf("ParsePlan() = %#v, %v", plan, err)
	}
	for name, invalid := range map[string][]byte{
		"unknown":    append(bytes.TrimSuffix(slices.Clone(content), []byte("}")), []byte(`,"unknown":true}`)...),
		"trailing":   append(slices.Clone(content), []byte(` {}`)...),
		"oversized":  make([]byte, maximumPlanBytes+1),
		"production": []byte(strings.Replace(string(content), `"mode":"rehearsal"`, `"mode":"production"`, 1)),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParsePlan(invalid); err == nil {
				t.Fatal("ParsePlan() error = nil")
			}
		})
	}
	directory := t.TempDir()
	filename := filepath.Join(directory, "plan.json")
	if err := os.WriteFile(filename, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadPlan(filename); err != nil || loaded.Commit != fixture.Plan.Commit {
		t.Fatalf("LoadPlan() = %#v, %v", loaded, err)
	}
	if _, err := LoadPlan(directory); err == nil {
		t.Fatal("LoadPlan(directory) error = nil")
	}
}

func loadParityFixture(t *testing.T, name string) parityFixture {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", "parity", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var fixture parityFixture
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func loadLegacyParityOracle(t *testing.T, name string) legacyParityOracle {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", "parity", name+"-legacy.json"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var oracle legacyParityOracle
	if err := decoder.Decode(&oracle); err != nil {
		t.Fatal(err)
	}
	return oracle
}

func assertLegacyOracleProvenance(t *testing.T, fixture parityFixture, oracle legacyParityOracle) {
	t.Helper()
	harness, err := os.ReadFile(filepath.Join("testdata", "parity", "legacy_builder_oracle_test.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	harnessSum := sha256.Sum256(harness)
	remoteMatches, err := sameGitRemote(oracle.BuilderRepository, fixture.Plan.Source)
	if err != nil {
		t.Fatal(err)
	}
	if oracle.Generation != fixture.Generation || !remoteMatches ||
		oracle.BuilderPackage != "internal/release" ||
		oracle.GoVersion != "go1.26.5" || oracle.Platform != "windows/amd64" ||
		oracle.HarnessSHA256 != hex.EncodeToString(harnessSum[:]) ||
		!commitPattern.MatchString(oracle.BuilderCommit) ||
		!commitPattern.MatchString(oracle.BuilderReleaseTree) ||
		oracle.FixtureCommit != fixture.Plan.Commit ||
		oracle.Version != fixture.Plan.Version ||
		oracle.SourceDateEpoch != fixture.Plan.SourceDateEpoch ||
		len(oracle.OutputSHA256) != len(fixture.Plan.Artifacts) {
		t.Fatalf("legacy oracle provenance does not match fixture: %#v", oracle)
	}
	for _, artifact := range fixture.Plan.Artifacts {
		hash := oracle.OutputSHA256[artifact]
		decoded, decodeErr := hex.DecodeString(hash)
		if decodeErr != nil || len(decoded) != sha256.Size {
			t.Fatalf("legacy oracle hash for %q is invalid: %q", artifact, hash)
		}
	}
}

func exactLegacyParityForArtifact(
	t *testing.T,
	expectation parityExpectation,
	artifact string,
) bool {
	t.Helper()
	switch {
	case artifact == "checksums.txt":
		return expectation.Checksums
	case strings.HasSuffix(artifact, "_sbom.spdx.json"):
		return expectation.SBOM
	case strings.HasSuffix(artifact, "_source.tar.gz"):
		return expectation.Archive
	default:
		t.Fatalf("parity fixture contains unknown artifact %q", artifact)
		return false
	}
}

func catalogForRenderFixture(t *testing.T, current string) catalog.Catalog {
	t.Helper()
	value, err := catalog.Default()
	if err != nil {
		t.Fatal(err)
	}
	value.StarterCompatibility.CurrentCore = current
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	return value
}

func fixtureTree(t *testing.T, values []fixtureEntry) committedTree {
	t.Helper()
	entries := make([]gitTreeEntry, 0, len(values))
	files := make(map[string][]byte, len(values))
	for _, value := range values {
		if err := validateArchivePath(value.Name); err != nil {
			t.Fatal(err)
		}
		content := []byte(value.Content)
		entries = append(entries, gitTreeEntry{mode: value.Mode, name: value.Name, data: content})
		files[value.Name] = content
	}
	slices.SortFunc(entries, func(left, right gitTreeEntry) int {
		return strings.Compare(left.name, right.name)
	})
	return committedTree{entries: entries, files: files}
}

func artifactHashes(artifacts map[string][]byte) map[string]string {
	result := make(map[string]string, len(artifacts))
	for name, content := range artifacts {
		sum := sha256.Sum256(content)
		result[name] = hex.EncodeToString(sum[:])
	}
	return result
}

func directoryHashes(t *testing.T, directory string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		content, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		result[entry.Name()] = hex.EncodeToString(sum[:])
	}
	return result
}

func equalArtifactBytes(left map[string][]byte, right map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for name, content := range left {
		if !bytes.Equal(content, right[name]) {
			return false
		}
	}
	return true
}

func equalStringMap(left map[string]string, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for name, value := range left {
		if right[name] != value {
			return false
		}
	}
	return true
}

func assertArchiveContract(t *testing.T, content []byte, plan Plan, wantEntries int) {
	t.Helper()
	gzipReader, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if !gzipReader.ModTime.Equal(time.Unix(plan.SourceDateEpoch, 0).UTC()) || gzipReader.OS != 255 {
		t.Fatalf("gzip header = %#v", gzipReader.Header)
	}
	tarReader := tar.NewReader(gzipReader)
	prefix := plan.Repository + "_" + strings.TrimPrefix(plan.Version, "v") + "/"
	count := 0
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(header.Name, prefix) ||
			!header.ModTime.Equal(time.Unix(plan.SourceDateEpoch, 0).UTC()) {
			t.Fatalf("archive header = %#v", header)
		}
		count++
	}
	if err := gzipReader.Close(); err != nil {
		t.Fatal(err)
	}
	if count != wantEntries {
		t.Fatalf("archive entries = %d, want %d", count, wantEntries)
	}
}

func archiveFile(t *testing.T, content []byte, wanted string) []byte {
	t.Helper()
	gzipReader, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := gzipReader.Close(); err != nil {
			t.Error(err)
		}
	}()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			t.Fatalf("archive has no %s", wanted)
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name != wanted {
			continue
		}
		data, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
}

func writeFixtureRepository(t *testing.T, root string, entries []fixtureEntry) {
	t.Helper()
	for _, entry := range entries {
		name := filepath.Join(root, filepath.FromSlash(entry.Name))
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
}

func gitCommand(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}
