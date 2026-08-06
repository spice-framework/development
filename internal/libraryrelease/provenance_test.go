package libraryrelease

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spice-framework/development/internal/catalog"
)

type provenanceFixture struct {
	Plan Plan              `json:"plan"`
	Tree []provenanceEntry `json:"tree"`
}

type provenanceEntry struct {
	Mode    string `json:"mode"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func TestRenderRebindsPersistedPlanProvenance(t *testing.T) {
	t.Parallel()
	fixture := loadProvenanceFixture(t)
	repository := committedProvenanceFixture(t, fixture, "starter-oidc")
	fixture.Plan.Commit = strings.TrimSpace(provenanceGitCommand(t, repository, "rev-parse", "HEAD"))
	value := catalogForProvenanceFixture(t, fixture.Plan.CompatibilityCurrent)

	persisted := fixture.Plan
	persisted.Repository = "attacker-controlled"
	persisted.Module = "example.invalid/attacker"
	persisted.Source = "https://example.invalid/attacker"
	persisted.CompatibilityMinimum = "v9.8.7"
	persisted.CompatibilityCurrent = "v9.9.9"
	persisted.RequiredFiles = []string{"../untrusted"}
	persisted.Artifacts = []string{"../untrusted"}
	content, err := json.Marshal(persisted)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := ParsePlan(content)
	if err != nil {
		t.Fatalf("ParsePlan(untrusted provenance): %v", err)
	}
	tree, err := readCommittedTree(t.Context(), repository, loaded.Commit)
	if err != nil {
		t.Fatal(err)
	}
	rebound, err := rebindPlanProvenance(t.Context(), repository, loaded, value, tree)
	if err != nil {
		t.Fatal(err)
	}
	if rebound.Repository != fixture.Plan.Repository || rebound.Module != fixture.Plan.Module ||
		rebound.Source != fixture.Plan.Source ||
		rebound.CompatibilityMinimum != fixture.Plan.CompatibilityMinimum ||
		rebound.CompatibilityCurrent != fixture.Plan.CompatibilityCurrent {
		t.Fatalf("rebound provenance = %#v", rebound)
	}
	if !slices.Equal(rebound.RequiredFiles, fixture.Plan.RequiredFiles) ||
		!slices.Equal(rebound.Artifacts, fixture.Plan.Artifacts) {
		t.Fatalf("rebound derived contract = %#v", rebound)
	}

	output := filepath.Join(t.TempDir(), "release")
	result, err := Render(t.Context(), repository, output, loaded, value)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Files, fixture.Plan.Artifacts) {
		t.Fatalf("Render() files = %v", result.Files)
	}
	sbomContent, err := os.ReadFile(filepath.Join(output, "starter-oidc_1.2.3_sbom.spdx.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sbom spdxDocument
	if err := json.Unmarshal(sbomContent, &sbom); err != nil {
		t.Fatal(err)
	}
	if sbom.Name != "starter-oidc v1.2.3" ||
		!strings.HasPrefix(sbom.DocumentNamespace, fixture.Plan.Source+"/releases/") ||
		len(sbom.Packages) == 0 || sbom.Packages[0].Name != fixture.Plan.Module {
		t.Fatalf("rendered SBOM provenance = %#v", sbom)
	}
}

func TestRenderRejectsOriginAndCommittedIdentityMismatch(t *testing.T) {
	t.Parallel()
	fixture := loadProvenanceFixture(t)
	value := catalogForProvenanceFixture(t, fixture.Plan.CompatibilityCurrent)
	for _, test := range []struct {
		name       string
		originRepo string
		want       string
	}{
		{name: "unknown origin", originRepo: "not-cataloged", want: "does not identify a catalog repository"},
		{name: "catalog origin module mismatch", originRepo: "starter-smtp", want: "catalog origin requires"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository := committedProvenanceFixture(t, fixture, test.originRepo)
			plan := fixture.Plan
			plan.Commit = strings.TrimSpace(provenanceGitCommand(t, repository, "rev-parse", "HEAD"))
			output := filepath.Join(t.TempDir(), "release")
			if _, err := Render(t.Context(), repository, output, plan, value); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("Render() error = %v", err)
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("failed render output state: %v", err)
			}
		})
	}
}

func TestRenderRequiresCatalogCompatibilityAtSelectedCommit(t *testing.T) {
	t.Parallel()
	fixture := loadProvenanceFixture(t)
	repository := committedProvenanceFixture(t, fixture, "starter-oidc")
	plan := fixture.Plan
	plan.Commit = strings.TrimSpace(provenanceGitCommand(t, repository, "rev-parse", "HEAD"))
	value := catalogForProvenanceFixture(t, "v0.3.0")
	output := filepath.Join(t.TempDir(), "release")
	if _, err := Render(t.Context(), repository, output, plan, value); err == nil ||
		!strings.Contains(err.Error(), "catalog requires") {
		t.Fatalf("Render(stale committed compatibility) error = %v", err)
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatalf("failed render output state: %v", err)
	}
}

func catalogForProvenanceFixture(t *testing.T, current string) catalog.Catalog {
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

func committedProvenanceFixture(
	t *testing.T,
	fixture provenanceFixture,
	originRepository string,
) string {
	t.Helper()
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
	provenanceGitCommand(t, repository, "init")
	provenanceGitCommand(t, repository, "config", "user.name", "Spice Test")
	provenanceGitCommand(t, repository, "config", "user.email", "spice@example.invalid")
	provenanceGitCommand(
		t,
		repository,
		"remote",
		"add",
		"origin",
		"https://github.com/spice-framework/"+originRepository+".git",
	)
	provenanceGitCommand(t, repository, "add", ".")
	command := exec.Command("git", "commit", "-m", "fixture")
	command.Dir = repository
	date := time.Unix(fixture.Plan.SourceDateEpoch, 0).UTC().Format(time.RFC3339)
	command.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, output)
	}
	return repository
}

func loadProvenanceFixture(t *testing.T) provenanceFixture {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", "parity", "newer.json"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	var fixture provenanceFixture
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func provenanceGitCommand(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}
