// Package gorelease renders and locally verifies catalog-authorized generic Go
// module releases. Starter libraries use the separate libraryrelease package.
package gorelease

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/spice-framework/development/internal/catalog"
	"github.com/spice-framework/development/internal/process"
)

const (
	metadataSchema       = 1
	artifactSchema       = 1
	maximumArtifactBytes = 256 << 20
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// Options identifies one immutable catalog-authorized release.
type Options struct {
	Root       string
	Repository string
	Version    string
}

// Result describes the committed output without exposing host-specific paths
// in release metadata.
type Result struct {
	OutputDir string
	Commit    string
	Files     []string
}

type source struct {
	root       string
	repository catalog.Repository
	commit     string
	epoch      int64
	modules    []module
}

type module struct {
	Path    string
	Version string
}

type intent struct {
	Schema     int    `json:"schema"`
	Profile    string `json:"profile"`
	Repository string `json:"repository"`
	Module     string `json:"module"`
	Version    string `json:"version"`
}

// Render creates a new deterministic artifact directory from a clean, tagged,
// committed checkout. It never downloads dependencies or replaces an output.
func Render(
	ctx context.Context,
	options Options,
	value catalog.Catalog,
	runner process.Runner,
	outputDirectory string,
) (result Result, resultErr error) {
	prepared, err := prepare(ctx, options, value, runner)
	if err != nil {
		return Result{}, err
	}
	artifacts, err := renderArtifacts(ctx, prepared, options.Version)
	if err != nil {
		return Result{}, err
	}
	files := sortedArtifactNames(artifacts)
	output, staging, err := prepareOutput(prepared.root, outputDirectory)
	if err != nil {
		return Result{}, err
	}
	committed := false
	defer func() {
		if !committed {
			resultErr = errors.Join(resultErr, os.RemoveAll(staging))
		}
	}()
	for _, name := range files {
		if err := writeArtifact(staging, name, artifacts[name]); err != nil {
			return Result{}, err
		}
	}
	if err := requireClean(ctx, prepared.root, runner); err != nil {
		return Result{}, err
	}
	if err := commitOutput(staging, output); err != nil {
		return Result{}, fmt.Errorf("commit Go release output without replacement: %w", err)
	}
	committed = true
	return Result{OutputDir: output, Commit: prepared.commit, Files: files}, nil
}

// Verify performs the renderer-owned local reproducibility gate. It strictly
// parses the artifact allowlist and compares every byte with a fresh rendering
// from the exact clean checkout. Independent trust verification is owned by the
// separately versioned toolchain verifier.
func Verify(
	ctx context.Context,
	options Options,
	value catalog.Catalog,
	runner process.Runner,
	artifactDirectory string,
) (Result, error) {
	prepared, err := prepare(ctx, options, value, runner)
	if err != nil {
		return Result{}, err
	}
	expected, err := renderArtifacts(ctx, prepared, options.Version)
	if err != nil {
		return Result{}, err
	}
	directory, err := realDirectory(artifactDirectory, "artifact directory")
	if err != nil {
		return Result{}, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return Result{}, fmt.Errorf("read Go release artifacts: %w", err)
	}
	wantFiles := sortedArtifactNames(expected)
	gotFiles := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return Result{}, fmt.Errorf("Go release artifact %q is not a regular file", entry.Name())
		}
		gotFiles = append(gotFiles, entry.Name())
	}
	slices.Sort(gotFiles)
	if !slices.Equal(gotFiles, wantFiles) {
		return Result{}, fmt.Errorf("Go release artifacts %v do not match contract %v", gotFiles, wantFiles)
	}
	for _, name := range wantFiles {
		actual, readErr := readBounded(filepath.Join(directory, name), maximumArtifactBytes)
		if readErr != nil {
			return Result{}, fmt.Errorf("read Go release artifact %q: %w", name, readErr)
		}
		if !bytes.Equal(actual, expected[name]) {
			return Result{}, fmt.Errorf("Go release artifact %q is not reproducible from commit %s", name, prepared.commit)
		}
	}
	return Result{OutputDir: directory, Commit: prepared.commit, Files: wantFiles}, nil
}

func prepare(
	ctx context.Context,
	options Options,
	value catalog.Catalog,
	runner process.Runner,
) (source, error) {
	if ctx == nil {
		return source{}, errors.New("Go release context must not be nil")
	}
	if runner == nil {
		return source{}, errors.New("Go release runner must not be nil")
	}
	if err := value.Validate(); err != nil {
		return source{}, err
	}
	repository, err := selectRepository(value, options.Repository, options.Version)
	if err != nil {
		return source{}, err
	}
	root, err := realDirectory(options.Root, "repository root")
	if err != nil {
		return source{}, err
	}
	if err := requireOrigin(ctx, root, repository, runner); err != nil {
		return source{}, err
	}
	if err := requireClean(ctx, root, runner); err != nil {
		return source{}, err
	}
	commit, epoch, err := requireTaggedHEAD(ctx, root, options.Version, runner)
	if err != nil {
		return source{}, err
	}
	if err := requireCommittedFiles(ctx, root, commit, repository.Release.MetadataFile, runner); err != nil {
		return source{}, err
	}
	if err := requireIntent(ctx, root, commit, repository, runner); err != nil {
		return source{}, err
	}
	modules, err := requireModuleGraph(ctx, root, repository, runner)
	if err != nil {
		return source{}, err
	}
	if err := requirePortableTree(ctx, root, commit); err != nil {
		return source{}, err
	}
	return source{root: root, repository: repository, commit: commit, epoch: epoch, modules: modules}, nil
}

func selectRepository(value catalog.Catalog, name string, version string) (catalog.Repository, error) {
	if strings.TrimSpace(name) == "" {
		return catalog.Repository{}, errors.New("Go release repository is required")
	}
	for _, repository := range value.Repositories {
		if repository.Name != name {
			continue
		}
		if strings.HasPrefix(repository.Name, "starter-") {
			return catalog.Repository{}, errors.New("starter repositories must use library-release")
		}
		if repository.Status != "active" || repository.Artifact != "go-module" ||
			repository.Release == nil || repository.Release.Profile != catalog.ReleaseProfileGoModule {
			return catalog.Repository{}, fmt.Errorf("repository %q is not authorized for go-module-v1", name)
		}
		if version != repository.Release.Version {
			return catalog.Repository{}, fmt.Errorf(
				"Go release version %q does not match catalog authorization %q",
				version,
				repository.Release.Version,
			)
		}
		return repository, nil
	}
	return catalog.Repository{}, fmt.Errorf("Go release repository %q is not in the catalog", name)
}

func requireOrigin(
	ctx context.Context,
	root string,
	repository catalog.Repository,
	runner process.Runner,
) error {
	actual, err := runner.Run(ctx, root, "git", "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("resolve Go release origin: %w", err)
	}
	actualIdentity, err := remoteIdentity(actual)
	if err != nil {
		return fmt.Errorf("parse Go release origin: %w", err)
	}
	expectedIdentity, err := remoteIdentity(repository.CloneURL)
	if err != nil {
		return fmt.Errorf("parse catalog clone URL: %w", err)
	}
	if actualIdentity != expectedIdentity {
		return fmt.Errorf("Go release origin %q does not match catalog clone URL %q", strings.TrimSpace(actual), repository.CloneURL)
	}
	return nil
}

func remoteIdentity(value string) (string, error) {
	value = strings.TrimSpace(value)
	if before, after, found := strings.Cut(value, ":"); found && !strings.Contains(before, "/") && strings.Contains(before, "@") {
		value = "ssh://" + before + "/" + after
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if !slices.Contains([]string{"https", "ssh"}, parsed.Scheme) || parsed.Hostname() == "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("require an HTTPS or SSH repository URL without query or fragment")
	}
	if parsed.Scheme == "https" && parsed.User != nil {
		return "", errors.New("HTTPS repository URL must not contain credentials")
	}
	if parsed.Scheme == "ssh" && (parsed.User == nil || parsed.User.Username() != "git") {
		return "", errors.New("SSH repository URL must use the git user")
	}
	repositoryPath := strings.TrimSuffix(strings.Trim(parsed.Path, "/"), ".git")
	if repositoryPath == "" || path.Clean(repositoryPath) != repositoryPath || repositoryPath == ".." || strings.HasPrefix(repositoryPath, "../") {
		return "", errors.New("repository path is empty or unsafe")
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Port() != "" {
		host += ":" + parsed.Port()
	}
	return host + "/" + repositoryPath, nil
}

func requireClean(ctx context.Context, root string, runner process.Runner) error {
	status, err := runner.Run(ctx, root, "git", "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect Go release checkout: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return errors.New("Go release checkout must be clean, including untracked files")
	}
	return nil
}

func requireTaggedHEAD(
	ctx context.Context,
	root string,
	version string,
	runner process.Runner,
) (string, int64, error) {
	commit, err := runner.Run(ctx, root, "git", "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", 0, fmt.Errorf("resolve Go release HEAD: %w", err)
	}
	commit = strings.TrimSpace(commit)
	if !commitPattern.MatchString(commit) {
		return "", 0, fmt.Errorf("Go release HEAD %q is not a full Git object ID", commit)
	}
	tag, err := runner.Run(ctx, root, "git", "rev-parse", "--verify", "refs/tags/"+version+"^{commit}")
	if err != nil {
		return "", 0, fmt.Errorf("resolve Go release tag %q: %w", version, err)
	}
	if strings.TrimSpace(tag) != commit {
		return "", 0, fmt.Errorf("Go release tag %q does not resolve to HEAD", version)
	}
	epochText, err := runner.Run(ctx, root, "git", "show", "-s", "--format=%ct", commit)
	if err != nil {
		return "", 0, fmt.Errorf("read Go release commit epoch: %w", err)
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(epochText), 10, 64)
	if err != nil || epoch <= 0 {
		return "", 0, fmt.Errorf("Go release commit epoch %q is invalid", strings.TrimSpace(epochText))
	}
	return commit, epoch, nil
}

func requireCommittedFiles(
	ctx context.Context,
	root string,
	commit string,
	metadataFile string,
	runner process.Runner,
) error {
	files := []string{"LICENSE", "README.md", "go.mod", "go.sum", metadataFile, "vendor/modules.txt"}
	for _, name := range files {
		kind, err := runner.Run(ctx, root, "git", "cat-file", "-t", commit+":"+name)
		if err != nil {
			return fmt.Errorf("required committed Go release file %q: %w", name, err)
		}
		if strings.TrimSpace(kind) != "blob" {
			return fmt.Errorf("required committed Go release file %q is not a regular Git blob", name)
		}
	}
	return nil
}

func requireIntent(
	ctx context.Context,
	root string,
	commit string,
	repository catalog.Repository,
	runner process.Runner,
) error {
	content, err := runner.Run(ctx, root, "git", "show", commit+":"+repository.Release.MetadataFile)
	if err != nil {
		return fmt.Errorf("read committed Go release metadata: %w", err)
	}
	var actual intent
	if err := decodeStrict([]byte(content), &actual); err != nil {
		return fmt.Errorf("parse committed Go release metadata: %w", err)
	}
	want := intent{
		Schema: metadataSchema, Profile: catalog.ReleaseProfileGoModule,
		Repository: repository.Name, Module: repository.Module, Version: repository.Release.Version,
	}
	if actual != want {
		return fmt.Errorf("committed Go release metadata %#v does not match catalog contract %#v", actual, want)
	}
	return nil
}

func decodeStrict(content []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("JSON document has trailing content")
	}
	return nil
}

func realDirectory(configured string, label string) (string, error) {
	if strings.TrimSpace(configured) == "" {
		return "", fmt.Errorf("Go release %s is required", label)
	}
	absolute, err := filepath.Abs(configured)
	if err != nil {
		return "", fmt.Errorf("resolve Go release %s: %w", label, err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", fmt.Errorf("inspect Go release %s: %w", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("Go release %s %q is not a real directory", label, absolute)
	}
	return absolute, nil
}

func prepareOutput(repositoryRoot string, configured string) (string, string, error) {
	if strings.TrimSpace(configured) == "" {
		return "", "", errors.New("Go release output directory is required")
	}
	output, err := filepath.Abs(configured)
	if err != nil {
		return "", "", fmt.Errorf("resolve Go release output: %w", err)
	}
	if within(repositoryRoot, output) {
		return "", "", errors.New("Go release output must be outside the repository")
	}
	parent := filepath.Dir(output)
	if _, err := realDirectory(parent, "output parent"); err != nil {
		return "", "", err
	}
	if _, err := os.Lstat(output); err == nil {
		return "", "", fmt.Errorf("Go release output %q already exists", output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("inspect Go release output: %w", err)
	}
	staging, err := os.MkdirTemp(parent, ".spice-go-release-*")
	if err != nil {
		return "", "", fmt.Errorf("create Go release staging directory: %w", err)
	}
	return output, staging, nil
}

func within(root string, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative))
}

func writeArtifact(directory string, name string, content []byte) error {
	if filepath.Base(name) != name || name == "." || name == ".." {
		return fmt.Errorf("Go release artifact name %q is unsafe", name)
	}
	file, err := os.OpenFile(filepath.Join(directory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create Go release artifact %q: %w", name, err)
	}
	written, writeErr := file.Write(content)
	if writeErr == nil && written != len(content) {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return fmt.Errorf("write Go release artifact %q: %w", name, err)
	}
	return nil
}

func readBounded(name string, maximum int64) ([]byte, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maximum {
		return nil, fmt.Errorf("content exceeds %d bytes", maximum)
	}
	return content, nil
}

func sortedArtifactNames(artifacts map[string][]byte) []string {
	files := make([]string, 0, len(artifacts))
	for name := range artifacts {
		files = append(files, name)
	}
	slices.Sort(files)
	return files
}

func checksums(artifacts map[string][]byte) []byte {
	names := sortedArtifactNames(artifacts)
	var output strings.Builder
	for _, name := range names {
		digest := sha256.Sum256(artifacts[name])
		fmt.Fprintf(&output, "%s  %s\n", hex.EncodeToString(digest[:]), name)
	}
	return []byte(output.String())
}
