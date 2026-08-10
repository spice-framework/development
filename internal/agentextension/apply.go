package agentextension

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

type beforeWrite func(string) error

func apply(ctx context.Context, target string, files []plannedFile, hook beforeWrite) (Result, error) {
	parent := filepath.Dir(target)
	if err := requireRealDirectory(parent); err != nil {
		return Result{}, fmt.Errorf("inspect Agent extension parent: %w", err)
	}
	info, err := os.Lstat(target)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return Result{}, fmt.Errorf("Agent extension target %q is not a real directory", target)
		}
		if err := requireRealDirectory(target); err != nil {
			return Result{}, err
		}
		entries, readErr := os.ReadDir(target)
		if readErr != nil {
			return Result{}, fmt.Errorf("inspect Agent extension target: %w", readErr)
		}
		if len(entries) != 0 {
			return Result{}, fmt.Errorf("Agent extension target %q is not empty", target)
		}
		created, directories, writeErr := writeFiles(ctx, target, files, hook)
		if writeErr != nil {
			return Result{}, errors.Join(writeErr, rollback(target, created, directories))
		}
		return resultFor(target, files, false), nil
	case !errors.Is(err, os.ErrNotExist):
		return Result{}, fmt.Errorf("inspect Agent extension target: %w", err)
	}

	staging, err := os.MkdirTemp(parent, ".spice-agent-extension-*")
	if err != nil {
		return Result{}, fmt.Errorf("create Agent extension staging directory: %w", err)
	}
	if _, _, err = writeFiles(ctx, staging, files, hook); err != nil {
		return Result{}, errors.Join(err, cleanupStaging(staging))
	}
	if err = ctx.Err(); err != nil {
		return Result{}, errors.Join(err, cleanupStaging(staging))
	}
	if err = os.Rename(staging, target); err != nil {
		return Result{}, errors.Join(
			fmt.Errorf("commit Agent extension without replacement: %w", err),
			cleanupStaging(staging),
		)
	}
	return resultFor(target, files, false), nil
}

func cleanupStaging(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove Agent extension staging directory: %w", err)
	}
	return nil
}

func writeFiles(
	ctx context.Context,
	root string,
	files []plannedFile,
	hook beforeWrite,
) ([]string, []string, error) {
	created := make([]string, 0, len(files))
	directories := make([]string, 0)
	seenDirectories := make(map[string]struct{})
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return created, directories, err
		}
		name := filepath.FromSlash(file.name)
		if filepath.IsAbs(name) || filepath.Clean(name) != name || name == "." ||
			strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return created, directories, fmt.Errorf("scaffold file path %q is unsafe", file.name)
		}
		directory := filepath.Dir(name)
		if directory != "." {
			parts := strings.Split(filepath.Clean(directory), string(filepath.Separator))
			current := root
			for _, part := range parts {
				current = filepath.Join(current, part)
				if _, exists := seenDirectories[current]; exists {
					continue
				}
				if err := os.Mkdir(current, 0o750); err != nil && !errors.Is(err, os.ErrExist) {
					return created, directories, fmt.Errorf("create scaffold directory %s: %w", directory, err)
				}
				seenDirectories[current] = struct{}{}
				directories = append(directories, current)
			}
		}
		if hook != nil {
			if err := hook(file.name); err != nil {
				return created, directories, err
			}
		}
		path := filepath.Join(root, name)
		handle, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(file.mode))
		if err != nil {
			return created, directories, fmt.Errorf("create scaffold file %s: %w", file.name, err)
		}
		_, writeErr := handle.Write(file.content)
		closeErr := handle.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return created, directories, fmt.Errorf("write scaffold file %s: %w", file.name, err)
		}
		created = append(created, path)
	}
	return created, directories, nil
}

func rollback(root string, files, directories []string) error {
	var result error
	for index := len(files) - 1; index >= 0; index-- {
		if err := os.Remove(files[index]); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	slices.Reverse(directories)
	for _, directory := range directories {
		if err := os.Remove(directory); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	if root != "" {
		// Existing empty targets are intentionally preserved.
		_ = root
	}
	return result
}

func requireRealDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%q is not a real directory", path)
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", path, err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if !samePath(filepath.Clean(absolute), filepath.Clean(real)) {
		return fmt.Errorf("%q traverses a symbolic link or reparse point", path)
	}
	return nil
}

func samePath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func resultFor(root string, files []plannedFile, materialized bool) Result {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.name)
	}
	slices.Sort(names)
	return Result{Root: root, Files: names, Materialized: materialized}
}
