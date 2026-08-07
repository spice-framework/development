// Package snapshot materializes and verifies exact catalog repository commits.
package snapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spice-framework/development/internal/catalog"
	"github.com/spice-framework/development/internal/process"
)

const manifestName = ".spice-snapshot.json"

type Lock struct {
	Schema   int                `json:"schema"`
	Snapshot string             `json:"snapshot,omitempty"`
	Catalog  LockedRepository   `json:"catalog,omitempty"`
	Sources  []LockedRepository `json:"sources"`
}

type LockedRepository struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
}

type Manifest struct {
	Schema   int                      `json:"schema"`
	Snapshot string                   `json:"snapshot,omitempty"`
	Sources  []MaterializedRepository `json:"sources"`
}

type MaterializedRepository struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Directory  string `json:"directory"`
}

// Load reads and validates the generic exact-repository fields in a snapshot lock.
// Documentation-specific lock fields remain owned by the consuming repository.
func Load(name string, value catalog.Catalog) (Lock, error) {
	if strings.TrimSpace(name) == "" {
		return Lock{}, errors.New("snapshot lock path must not be empty")
	}
	content, err := os.ReadFile(name)
	if err != nil {
		return Lock{}, fmt.Errorf("read snapshot lock: %w", err)
	}
	var result Lock
	if err := json.Unmarshal(content, &result); err != nil {
		return Lock{}, fmt.Errorf("decode snapshot lock: %w", err)
	}
	if err := result.Validate(value); err != nil {
		return Lock{}, err
	}
	return result, nil
}

func (lock Lock) Validate(value catalog.Catalog) error {
	if lock.Schema < 1 {
		return errors.New("snapshot lock schema must be positive")
	}
	if len(lock.Sources) == 0 && lock.Catalog.Repository == "" {
		return errors.New("snapshot lock has no repositories")
	}
	known := make(map[string]catalog.Repository, len(value.Repositories))
	for _, repository := range value.Repositories {
		known[repository.Name] = repository
	}
	seen := make(map[string]struct{}, len(lock.Sources)+1)
	for _, locked := range lock.repositories() {
		repository, exists := known[locked.Repository]
		if !exists || (repository.Status != "active" && repository.Status != "migrating") {
			return fmt.Errorf("snapshot repository %q is not active in the catalog", locked.Repository)
		}
		if _, duplicate := seen[locked.Repository]; duplicate {
			return fmt.Errorf("snapshot repository %q is duplicated", locked.Repository)
		}
		seen[locked.Repository] = struct{}{}
		if !validCommit(locked.Commit) {
			return fmt.Errorf(
				"snapshot repository %q commit must be 40 lowercase hexadecimal characters",
				locked.Repository,
			)
		}
	}
	return nil
}

// Materialize performs the command's explicit online phase into a new root.
// It never replaces an existing root and rolls back its staging tree on failure.
func Materialize(
	ctx context.Context,
	lock Lock,
	root string,
	value catalog.Catalog,
	runner process.Runner,
) (Manifest, error) {
	if ctx == nil {
		return Manifest{}, errors.New("snapshot context must not be nil")
	}
	if runner == nil {
		return Manifest{}, errors.New("snapshot process runner must not be nil")
	}
	if err := lock.Validate(value); err != nil {
		return Manifest{}, err
	}
	absolute, err := newSnapshotRoot(root)
	if err != nil {
		return Manifest{}, err
	}
	staging := absolute + ".spice-staging"
	if _, err := os.Lstat(staging); err == nil {
		return Manifest{}, fmt.Errorf("snapshot staging path already exists: %s", staging)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, fmt.Errorf("inspect snapshot staging path: %w", err)
	}
	if err := os.MkdirAll(staging, 0o750); err != nil {
		return Manifest{}, fmt.Errorf("create snapshot staging root: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(staging)
		}
	}()

	manifest, err := expectedManifest(lock, value)
	if err != nil {
		return Manifest{}, err
	}
	byName := catalogByName(value)
	for _, source := range manifest.Sources {
		if err := ctx.Err(); err != nil {
			return Manifest{}, err
		}
		repository := byName[source.Repository]
		directory := filepath.Join(staging, source.Directory)
		if err := os.Mkdir(directory, 0o750); err != nil {
			return Manifest{}, fmt.Errorf("create snapshot repository %q: %w", source.Repository, err)
		}
		commands := [][]string{
			{"git", "init", "--quiet"},
			{"git", "remote", "add", "origin", repository.CloneURL},
			{"git", "fetch", "--no-tags", "--depth=1", "origin", source.Commit},
			{"git", "checkout", "--quiet", "--detach", source.Commit},
		}
		for _, command := range commands {
			if _, err := runner.Run(ctx, directory, command...); err != nil {
				return Manifest{}, fmt.Errorf("materialize snapshot repository %q: %w", source.Repository, err)
			}
		}
		if err := rejectSymlinks(directory); err != nil {
			return Manifest{}, fmt.Errorf("materialized snapshot repository %q: %w", source.Repository, err)
		}
	}
	content, err := marshalManifest(manifest)
	if err != nil {
		return Manifest{}, err
	}
	if err := os.WriteFile(filepath.Join(staging, manifestName), content, 0o600); err != nil {
		return Manifest{}, fmt.Errorf("write snapshot manifest: %w", err)
	}
	if err := os.Rename(staging, absolute); err != nil {
		return Manifest{}, fmt.Errorf("publish snapshot root: %w", err)
	}
	committed = true
	return manifest, nil
}

// Verify performs strict offline verification of an existing materialized root.
func Verify(
	ctx context.Context,
	lock Lock,
	root string,
	value catalog.Catalog,
	runner process.Runner,
) (Manifest, error) {
	if ctx == nil {
		return Manifest{}, errors.New("snapshot context must not be nil")
	}
	if runner == nil {
		return Manifest{}, errors.New("snapshot process runner must not be nil")
	}
	if err := lock.Validate(value); err != nil {
		return Manifest{}, err
	}
	absolute, err := existingSnapshotRoot(root)
	if err != nil {
		return Manifest{}, err
	}
	expected, err := expectedManifest(lock, value)
	if err != nil {
		return Manifest{}, err
	}
	expectedContent, err := marshalManifest(expected)
	if err != nil {
		return Manifest{}, err
	}
	actualContent, err := os.ReadFile(filepath.Join(absolute, manifestName))
	if err != nil {
		return Manifest{}, fmt.Errorf("read snapshot manifest: %w", err)
	}
	if !bytes.Equal(actualContent, expectedContent) {
		return Manifest{}, errors.New("snapshot manifest does not match the lock and catalog")
	}
	byName := catalogByName(value)
	for _, source := range expected.Sources {
		if err := ctx.Err(); err != nil {
			return Manifest{}, err
		}
		repository := byName[source.Repository]
		directory := filepath.Join(absolute, source.Directory)
		if err := rejectSymlinks(directory); err != nil {
			return Manifest{}, fmt.Errorf("verify snapshot repository %q: %w", source.Repository, err)
		}
		remote, err := runner.Run(ctx, directory, "git", "remote", "get-url", "origin")
		if err != nil {
			return Manifest{}, fmt.Errorf("verify snapshot repository %q remote: %w", source.Repository, err)
		}
		if strings.TrimSpace(remote) != repository.CloneURL {
			return Manifest{}, fmt.Errorf("snapshot repository %q remote is not canonical", source.Repository)
		}
		commit, err := runner.Run(ctx, directory, "git", "rev-parse", "--verify", "HEAD^{commit}")
		if err != nil {
			return Manifest{}, fmt.Errorf("verify snapshot repository %q commit: %w", source.Repository, err)
		}
		if strings.TrimSpace(commit) != source.Commit {
			return Manifest{}, fmt.Errorf("snapshot repository %q is not at locked commit", source.Repository)
		}
		status, err := runner.Run(
			ctx,
			directory,
			"git", "status", "--porcelain=v1", "--untracked-files=all",
		)
		if err != nil {
			return Manifest{}, fmt.Errorf("verify snapshot repository %q status: %w", source.Repository, err)
		}
		if strings.TrimSpace(status) != "" {
			return Manifest{}, fmt.Errorf("snapshot repository %q has local changes", source.Repository)
		}
	}
	return expected, nil
}

func expectedManifest(lock Lock, value catalog.Catalog) (Manifest, error) {
	byName := catalogByName(value)
	sources := make([]MaterializedRepository, 0, len(lock.Sources)+1)
	for _, locked := range lock.repositories() {
		repository, exists := byName[locked.Repository]
		if !exists {
			return Manifest{}, fmt.Errorf("snapshot repository %q is not in the catalog", locked.Repository)
		}
		sources = append(sources, MaterializedRepository{
			Repository: locked.Repository,
			Commit:     locked.Commit,
			Directory:  repository.Directory,
		})
	}
	slices.SortFunc(sources, func(left, right MaterializedRepository) int {
		return strings.Compare(left.Repository, right.Repository)
	})
	return Manifest{Schema: 1, Snapshot: lock.Snapshot, Sources: sources}, nil
}

func (lock Lock) repositories() []LockedRepository {
	result := make([]LockedRepository, 0, len(lock.Sources)+1)
	if lock.Catalog.Repository != "" {
		result = append(result, lock.Catalog)
	}
	return append(result, lock.Sources...)
}

func catalogByName(value catalog.Catalog) map[string]catalog.Repository {
	result := make(map[string]catalog.Repository, len(value.Repositories))
	for _, repository := range value.Repositories {
		result[repository.Name] = repository
	}
	return result
}

func marshalManifest(manifest Manifest) ([]byte, error) {
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode snapshot manifest: %w", err)
	}
	return append(content, '\n'), nil
}

func newSnapshotRoot(root string) (string, error) {
	absolute, err := cleanAbsoluteRoot(root)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(absolute); err == nil {
		return "", fmt.Errorf("snapshot root already exists: %s", absolute)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("inspect snapshot root: %w", err)
	}
	if err := ensureExistingAncestorsAreDirectories(absolute); err != nil {
		return "", err
	}
	return absolute, nil
}

func existingSnapshotRoot(root string) (string, error) {
	absolute, err := cleanAbsoluteRoot(root)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", fmt.Errorf("inspect snapshot root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("snapshot root must be a real directory")
	}
	return absolute, nil
}

func cleanAbsoluteRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("snapshot root must not be empty")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve snapshot root: %w", err)
	}
	if filepath.Clean(absolute) != absolute || filepath.Dir(absolute) == absolute {
		return "", errors.New("snapshot root must be a clean non-filesystem-root path")
	}
	return absolute, nil
}

func ensureExistingAncestorsAreDirectories(name string) error {
	current := filepath.Dir(name)
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("snapshot ancestor %q must be a real directory", current)
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("inspect snapshot ancestor %q: %w", current, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func rejectSymlinks(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("inspect snapshot tree: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("snapshot repository must be a real directory")
	}
	return filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("snapshot tree contains symbolic link %q", name)
		}
		return nil
	})
}

func validCommit(value string) bool {
	if len(value) != 40 || strings.ToLower(value) != value {
		return false
	}
	decoded := make([]byte, sha256.Size)
	_, err := hex.Decode(decoded, []byte(value))
	return err == nil
}
