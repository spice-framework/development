package libraryrelease

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitStagingNeverReplacesExistingOutput(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	staging, err := os.MkdirTemp(parent, ".staging-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "artifact"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(parent, "release")
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := commitStaging(staging, output); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("commitStaging(existing empty directory) error = %v", err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 0 {
		t.Fatalf("existing output was replaced: entries=%v error=%v", entries, err)
	}
	if content, err := os.ReadFile(filepath.Join(staging, "artifact")); err != nil || string(content) != "new" {
		t.Fatalf("failed commit staging state = %q, %v", content, err)
	}
}

func TestCommitStagingPublishesOneCompleteDirectory(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	staging, err := os.MkdirTemp(parent, ".staging-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "artifact"), []byte("complete"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(parent, "release")
	if err := commitStaging(staging, output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(staging); !os.IsNotExist(err) {
		t.Fatalf("staging still exists after commit: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(output, "artifact")); err != nil ||
		string(content) != "complete" {
		t.Fatalf("committed output = %q, %v", content, err)
	}
}

func TestPrepareStagingRejectsSymlinkedAncestor(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := t.TempDir()
	link := filepath.Join(root, "redirect")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	if _, _, err := prepareStaging(filepath.Join(link, "nested", "release")); err == nil ||
		!strings.Contains(err.Error(), "not a real directory") {
		t.Fatalf("prepareStaging(symlink ancestor) error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(target, "nested")); !os.IsNotExist(err) {
		t.Fatalf("untrusted symlink target was mutated: %v", err)
	}
}

func TestPrepareStagingCreatesMissingRealParentTree(t *testing.T) {
	t.Parallel()
	parent := filepath.Join(t.TempDir(), "first", "second")
	output, staging, err := prepareStaging(filepath.Join(parent, "release"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(staging); err != nil {
			t.Error(err)
		}
	})
	if output != filepath.Join(parent, "release") {
		t.Fatalf("output = %q", output)
	}
	if info, err := os.Lstat(parent); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("created parent = %#v, %v", info, err)
	}
}
