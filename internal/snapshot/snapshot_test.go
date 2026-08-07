package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spice-framework/development/internal/catalog"
)

const (
	developmentCommit = "0123456789abcdef0123456789abcdef01234567"
	spiceCommit       = "89abcdef0123456789abcdef0123456789abcdef"
)

func TestMaterializeAndVerifyDeterministicSnapshot(t *testing.T) {
	t.Parallel()
	value := snapshotCatalog()
	lock := snapshotLock()
	runner := &snapshotRunner{catalog: value, commits: map[string]string{
		"development": developmentCommit,
		"spice":       spiceCommit,
	}}
	root := filepath.Join(t.TempDir(), "sources")
	manifest, err := Materialize(t.Context(), lock, root, value, runner)
	if err != nil {
		t.Fatal(err)
	}
	if got := manifest.Sources[0].Repository; got != "development" {
		t.Fatalf("first materialized source = %q", got)
	}
	verified, err := Verify(t.Context(), lock, root, value, runner)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(manifest.Sources, verified.Sources) {
		t.Fatalf("verified sources = %#v, want %#v", verified.Sources, manifest.Sources)
	}
	first, err := os.ReadFile(filepath.Join(root, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	secondRoot := filepath.Join(t.TempDir(), "sources")
	if _, err := Materialize(t.Context(), lock, secondRoot, value, runner); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(secondRoot, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) || strings.Contains(string(first), filepath.Dir(root)) {
		t.Fatalf("snapshot manifest is not deterministic:\n%s\n%s", first, second)
	}
	wantCommands := 8 + 6 + 8
	if len(runner.calls) != wantCommands {
		t.Fatalf("runner calls = %d, want %d: %#v", len(runner.calls), wantCommands, runner.calls)
	}
}

func TestLoadAcceptsConsumerFieldsAndRejectsInvalidLocks(t *testing.T) {
	t.Parallel()
	value := snapshotCatalog()
	name := filepath.Join(t.TempDir(), "ecosystem.lock.json")
	content := `{
  "schema": 1,
  "snapshot": "preview-current",
  "generatedAt": "not-part-of-deterministic-output",
  "catalog": {
    "repository": "development",
    "commit": "` + developmentCommit + `",
    "path": "internal/catalog/compatibility.json",
    "sha256": "ignored-by-generic-snapshot"
  },
  "sources": [{
    "repository": "spice",
    "commit": "` + spiceCommit + `",
    "manifestSha256": "owned-by-docs"
  }]
}`
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := Load(name, value)
	if err != nil || lock.Snapshot != "preview-current" || len(lock.Sources) != 1 {
		t.Fatalf("Load() = %#v, %v", lock, err)
	}

	tests := []struct {
		name string
		lock Lock
		want string
	}{
		{name: "schema", lock: Lock{Sources: []LockedRepository{{Repository: "spice", Commit: spiceCommit}}}, want: "schema"},
		{name: "empty", lock: Lock{Schema: 1}, want: "no repositories"},
		{name: "unknown", lock: Lock{Schema: 1, Sources: []LockedRepository{{Repository: "missing", Commit: spiceCommit}}}, want: "not active"},
		{name: "planned", lock: Lock{Schema: 1, Sources: []LockedRepository{{Repository: "future", Commit: spiceCommit}}}, want: "not active"},
		{name: "duplicate", lock: Lock{Schema: 1, Catalog: LockedRepository{Repository: "development", Commit: developmentCommit}, Sources: []LockedRepository{{Repository: "development", Commit: developmentCommit}}}, want: "duplicated"},
		{name: "short commit", lock: Lock{Schema: 1, Sources: []LockedRepository{{Repository: "spice", Commit: "abc"}}}, want: "40 lowercase"},
		{name: "uppercase commit", lock: Lock{Schema: 1, Sources: []LockedRepository{{Repository: "spice", Commit: strings.ToUpper(spiceCommit)}}}, want: "40 lowercase"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.lock.Validate(value)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.want)
			}
		})
	}
	if _, err := Load("", value); err == nil {
		t.Fatal("Load(empty) error = nil")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad, value); err == nil {
		t.Fatal("Load(malformed) error = nil")
	}
}

func TestMaterializeRollsBackAndRefusesExistingTargets(t *testing.T) {
	t.Parallel()
	value := snapshotCatalog()
	lock := snapshotLock()
	parent := t.TempDir()
	root := filepath.Join(parent, "sources")
	runner := &snapshotRunner{catalog: value, failAt: 3}
	if _, err := Materialize(t.Context(), lock, root, value, runner); err == nil {
		t.Fatal("Materialize(failing runner) error = nil")
	}
	if _, err := os.Lstat(root + ".spice-staging"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging root remains after rollback: %v", err)
	}
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := Materialize(t.Context(), lock, root, value, runner); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Materialize(existing) error = %v", err)
	}
	if _, err := Materialize(nil, lock, filepath.Join(parent, "nil"), value, runner); err == nil {
		t.Fatal("Materialize(nil context) error = nil")
	}
	if _, err := Materialize(t.Context(), lock, filepath.Join(parent, "nil-runner"), value, nil); err == nil {
		t.Fatal("Materialize(nil runner) error = nil")
	}
	if _, err := Materialize(t.Context(), lock, string(filepath.Separator), value, runner); err == nil {
		t.Fatal("Materialize(filesystem root) error = nil")
	}
}

func TestVerifyRejectsManifestRemoteCommitStatusAndSymlinks(t *testing.T) {
	value := snapshotCatalog()
	lock := snapshotLock()
	newFixture := func(t *testing.T) (string, *snapshotRunner) {
		t.Helper()
		runner := &snapshotRunner{catalog: value, commits: map[string]string{
			"development": developmentCommit,
			"spice":       spiceCommit,
		}}
		root := filepath.Join(t.TempDir(), "sources")
		if _, err := Materialize(t.Context(), lock, root, value, runner); err != nil {
			t.Fatal(err)
		}
		runner.calls = nil
		return root, runner
	}

	t.Run("manifest", func(t *testing.T) {
		root, runner := newFixture(t)
		if err := os.WriteFile(filepath.Join(root, manifestName), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Verify(t.Context(), lock, root, value, runner); err == nil ||
			!strings.Contains(err.Error(), "manifest") {
			t.Fatalf("Verify(manifest) error = %v", err)
		}
	})
	t.Run("remote", func(t *testing.T) {
		root, runner := newFixture(t)
		runner.badRemote = "spice"
		if _, err := Verify(t.Context(), lock, root, value, runner); err == nil ||
			!strings.Contains(err.Error(), "remote is not canonical") {
			t.Fatalf("Verify(remote) error = %v", err)
		}
	})
	t.Run("commit", func(t *testing.T) {
		root, runner := newFixture(t)
		runner.commits["spice"] = developmentCommit
		if _, err := Verify(t.Context(), lock, root, value, runner); err == nil ||
			!strings.Contains(err.Error(), "not at locked commit") {
			t.Fatalf("Verify(commit) error = %v", err)
		}
	})
	t.Run("status", func(t *testing.T) {
		root, runner := newFixture(t)
		runner.dirty = "spice"
		if _, err := Verify(t.Context(), lock, root, value, runner); err == nil ||
			!strings.Contains(err.Error(), "local changes") {
			t.Fatalf("Verify(status) error = %v", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		root, runner := newFixture(t)
		link := filepath.Join(root, "spice", "escape")
		if err := os.Symlink(filepath.Dir(root), link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := Verify(t.Context(), lock, root, value, runner); err == nil ||
			!strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("Verify(symlink) error = %v", err)
		}
	})
	if _, err := Verify(nil, lock, t.TempDir(), value, &snapshotRunner{}); err == nil {
		t.Fatal("Verify(nil context) error = nil")
	}
	if _, err := Verify(t.Context(), lock, t.TempDir(), value, nil); err == nil {
		t.Fatal("Verify(nil runner) error = nil")
	}
}

func TestMaterializeHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	root := filepath.Join(t.TempDir(), "sources")
	if _, err := Materialize(ctx, snapshotLock(), root, snapshotCatalog(), &snapshotRunner{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Materialize(cancelled) error = %v", err)
	}
}

func TestMaterializeRejectsSymlinkAncestor(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	realDirectory := filepath.Join(parent, "real")
	if err := os.Mkdir(realDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "linked")
	if err := os.Symlink(realDirectory, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	root := filepath.Join(link, "nested", "sources")
	if _, err := Materialize(t.Context(), snapshotLock(), root, snapshotCatalog(), &snapshotRunner{}); err == nil ||
		!strings.Contains(err.Error(), "real directory") {
		t.Fatalf("Materialize(symlink ancestor) error = %v", err)
	}
}

type snapshotRunner struct {
	catalog   catalog.Catalog
	commits   map[string]string
	calls     [][]string
	failAt    int
	badRemote string
	dirty     string
}

func (runner *snapshotRunner) Run(
	ctx context.Context,
	directory string,
	arguments ...string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	runner.calls = append(runner.calls, slices.Clone(arguments))
	if runner.failAt > 0 && len(runner.calls) == runner.failAt {
		return "", errors.New("injected process failure")
	}
	name := filepath.Base(directory)
	switch {
	case slices.Equal(arguments, []string{"git", "remote", "get-url", "origin"}):
		if runner.badRemote == name {
			return "https://github.com/example/wrong.git", nil
		}
		for _, repository := range runner.catalog.Repositories {
			if repository.Directory == name {
				return repository.CloneURL, nil
			}
		}
	case slices.Equal(arguments, []string{"git", "rev-parse", "--verify", "HEAD^{commit}"}):
		return runner.commits[name], nil
	case slices.Equal(arguments, []string{"git", "status", "--porcelain=v1", "--untracked-files=all"}):
		if runner.dirty == name {
			return " M README.md", nil
		}
		return "", nil
	}
	return "", nil
}

func snapshotLock() Lock {
	return Lock{
		Schema:   1,
		Snapshot: "preview-current",
		Catalog: LockedRepository{
			Repository: "development",
			Commit:     developmentCommit,
		},
		Sources: []LockedRepository{{Repository: "spice", Commit: spiceCommit}},
	}
}

func snapshotCatalog() catalog.Catalog {
	return catalog.Catalog{Repositories: []catalog.Repository{
		{
			Name: "development", Directory: "development", Status: "active",
			CloneURL: "https://github.com/spice-framework/development.git",
		},
		{
			Name: "spice", Directory: "spice", Status: "active",
			CloneURL: "https://github.com/spice-framework/spice.git",
		},
		{
			Name: "future", Directory: "future", Status: "planned",
			CloneURL: "https://github.com/spice-framework/future.git",
		},
	}}
}

func TestManifestJSONShape(t *testing.T) {
	t.Parallel()
	manifest, err := expectedManifest(snapshotLock(), snapshotCatalog())
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(manifest)
	if err != nil || !strings.Contains(string(content), `"directory":"development"`) {
		t.Fatalf("manifest JSON = %s, %v", content, err)
	}
}
