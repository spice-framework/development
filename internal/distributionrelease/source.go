package distributionrelease

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha1" // #nosec G505 -- SHA-1 is required to verify SHA-1 Git object identities.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	maximumGitTreeBytes      = 32 << 20
	maximumSourceArchiveSize = 1 << 30
)

type treeEntry struct {
	mode   string
	object string
}

func requirePortableTree(
	ctx context.Context,
	root string,
	commit string,
) (map[string]treeEntry, error) {
	content, err := gitBinary(
		ctx,
		root,
		maximumGitTreeBytes,
		"ls-tree",
		"-rz",
		"--full-tree",
		commit,
	)
	if err != nil {
		return nil, fmt.Errorf("list distribution release tree: %w", err)
	}
	return parsePortableTree(content)
}

func parsePortableTree(content []byte) (map[string]treeEntry, error) {
	if len(content) == 0 || content[len(content)-1] != 0 {
		return nil, errors.New("distribution release commit has no complete tracked tree")
	}
	result := make(map[string]treeEntry)
	casePaths := make(map[string]string)
	pathKinds := make(map[string]string)
	for _, item := range bytes.Split(content[:len(content)-1], []byte{0}) {
		metadata, nameBytes, found := bytes.Cut(item, []byte{'\t'})
		fields := strings.Fields(string(metadata))
		name := string(nameBytes)
		if !found || len(fields) != 3 || fields[1] != "blob" ||
			(fields[0] != "100644" && fields[0] != "100755") ||
			!commitPattern.MatchString(fields[2]) {
			return nil, fmt.Errorf("distribution release source has unsupported tree entry %q", item)
		}
		if err := portablePath(name); err != nil {
			return nil, fmt.Errorf("distribution release source path %q: %w", name, err)
		}
		components := strings.Split(name, "/")
		for index := range components {
			candidate := strings.Join(components[:index+1], "/")
			key := strings.ToLower(candidate)
			if prior, exists := casePaths[key]; exists && prior != candidate {
				return nil, fmt.Errorf(
					"distribution release source paths %q and %q collide on case-insensitive filesystems",
					prior,
					candidate,
				)
			}
			casePaths[key] = candidate
			kind := "directory"
			if index == len(components)-1 {
				kind = "file"
			}
			if priorKind, exists := pathKinds[key]; exists && priorKind != kind {
				return nil, fmt.Errorf("distribution release source path %q is both a file and directory", candidate)
			}
			pathKinds[key] = kind
		}
		if _, duplicate := result[name]; duplicate {
			return nil, fmt.Errorf("distribution release source path %q is duplicated", name)
		}
		result[name] = treeEntry{mode: fields[0], object: fields[2]}
	}
	if len(result) == 0 {
		return nil, errors.New("distribution release commit has no tracked files")
	}
	return result, nil
}

func materializeTaggedTree(
	ctx context.Context,
	repositoryRoot string,
	commit string,
	tree map[string]treeEntry,
) (scratchRoot string, sourceRoot string, resultErr error) {
	scratchRoot, err := os.MkdirTemp("", "spice-distribution-source-*")
	if err != nil {
		return "", "", fmt.Errorf("create distribution source scratch directory: %w", err)
	}
	cleanupRoot := scratchRoot
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, os.RemoveAll(cleanupRoot))
		}
	}()
	sourceRoot = filepath.Join(scratchRoot, "source")
	if err := os.Mkdir(sourceRoot, 0o700); err != nil {
		return "", "", fmt.Errorf("create materialized distribution source: %w", err)
	}
	archive, err := gitBinary(
		ctx,
		repositoryRoot,
		maximumSourceArchiveSize,
		"archive",
		"--format=tar",
		commit,
	)
	if err != nil {
		return "", "", fmt.Errorf("archive tagged distribution source: %w", err)
	}
	if err := extractMaterializedTree(sourceRoot, archive, tree); err != nil {
		return "", "", err
	}
	for _, name := range []string{"gocache", "gomodcache", "gopath", "gotmp"} {
		if err := os.Mkdir(filepath.Join(scratchRoot, name), 0o700); err != nil {
			return "", "", fmt.Errorf("create isolated distribution %s: %w", name, err)
		}
	}
	return scratchRoot, sourceRoot, nil
}

func extractMaterializedTree(
	sourceRoot string,
	archive []byte,
	tree map[string]treeEntry,
) error {
	wantedDirectories := make(map[string]struct{})
	for name := range tree {
		for directory := path.Dir(name); directory != "."; directory = path.Dir(directory) {
			wantedDirectories[directory] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(tree))
	seenGlobalHeader := false
	reader := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tagged distribution archive: %w", err)
		}
		if header.Typeflag == tar.TypeXGlobalHeader && header.Name == "pax_global_header" &&
			!seenGlobalHeader && len(seen) == 0 {
			seenGlobalHeader = true
			continue
		}
		name := strings.TrimSuffix(header.Name, "/")
		if header.Typeflag == tar.TypeDir {
			if _, wanted := wantedDirectories[name]; !wanted {
				return fmt.Errorf("tagged distribution archive contains unexpected directory %q", header.Name)
			}
			continue
		}
		entry, wanted := tree[name]
		if !wanted || header.Typeflag != tar.TypeReg || header.Size < 0 ||
			header.Size > maximumSourceArchiveSize {
			return fmt.Errorf("tagged distribution archive contains unsupported entry %q", header.Name)
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("tagged distribution archive repeats %q", name)
		}
		mode := os.FileMode(0o600)
		if entry.mode == "100755" {
			mode = 0o700
		}
		destination := filepath.Join(sourceRoot, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return fmt.Errorf("create materialized distribution directory for %q: %w", name, err)
		}
		file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return fmt.Errorf("create materialized distribution file %q: %w", name, err)
		}
		hasher, err := gitBlobHasher(entry.object, header.Size)
		if err != nil {
			_ = file.Close()
			return fmt.Errorf("prepare Git identity verification for %q: %w", name, err)
		}
		written, copyErr := io.Copy(io.MultiWriter(file, hasher), reader)
		closeErr := file.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return fmt.Errorf("write materialized distribution file %q: %w", name, err)
		}
		if written != header.Size {
			return fmt.Errorf("materialized distribution file %q size changed", name)
		}
		if actual := hex.EncodeToString(hasher.Sum(nil)); actual != entry.object {
			return fmt.Errorf(
				"materialized distribution file %q has Git object %s; tagged tree requires %s",
				name,
				actual,
				entry.object,
			)
		}
		seen[name] = struct{}{}
	}
	if len(seen) != len(tree) {
		return fmt.Errorf("tagged distribution archive contains %d files; tree requires %d", len(seen), len(tree))
	}
	return nil
}

func gitBlobHasher(object string, size int64) (hash.Hash, error) {
	var hasher hash.Hash
	switch len(object) {
	case 40:
		hasher = sha1.New() // #nosec G401 -- Git SHA-1 object identity compatibility, not security hashing.
	case 64:
		hasher = sha256.New()
	default:
		return nil, fmt.Errorf("unsupported Git object identity length %d", len(object))
	}
	_, _ = fmt.Fprintf(hasher, "blob %d%c", size, byte(0))
	return hasher, nil
}
