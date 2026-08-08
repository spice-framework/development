// Package distributionrelease renders and locally verifies deterministic
// catalog-authorized Go binary distributions.
package distributionrelease

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
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/spice-framework/development/internal/catalog"
	"github.com/spice-framework/development/internal/process"
)

const (
	intentSchema         = 1
	artifactSchema       = 1
	maximumArtifactBytes = 512 << 20
	maximumGitBytes      = 256 << 20
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

type Options struct {
	Root       string
	Repository string
	Version    string
}

type Result struct {
	OutputDir string
	Commit    string
	Files     []string
}

type preparedRelease struct {
	root         string
	sourceRoot   string
	scratchRoot  string
	goExecutable string
	repository   catalog.Repository
	commit       string
	epoch        int64
	modules      []module
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
	defer func() {
		resultErr = errors.Join(resultErr, prepared.cleanup())
	}()
	artifacts, err := renderArtifacts(ctx, prepared)
	if err != nil {
		return Result{}, err
	}
	files := artifactNames(artifacts)
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
	if err := requireCurrentSource(ctx, prepared, runner); err != nil {
		return Result{}, err
	}
	if err := commitOutput(staging, output); err != nil {
		return Result{}, fmt.Errorf("commit distribution release without replacement: %w", err)
	}
	committed = true
	return Result{OutputDir: output, Commit: prepared.commit, Files: files}, nil
}

func Verify(
	ctx context.Context,
	options Options,
	value catalog.Catalog,
	runner process.Runner,
	artifactDirectory string,
) (result Result, resultErr error) {
	prepared, err := prepare(ctx, options, value, runner)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, prepared.cleanup())
	}()
	expected, err := renderArtifacts(ctx, prepared)
	if err != nil {
		return Result{}, err
	}
	directory, err := realDirectory(artifactDirectory, "artifact directory")
	if err != nil {
		return Result{}, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return Result{}, fmt.Errorf("read distribution artifacts: %w", err)
	}
	want := artifactNames(expected)
	actualNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return Result{}, fmt.Errorf("distribution artifact %q is not a regular file", entry.Name())
		}
		actualNames = append(actualNames, entry.Name())
	}
	slices.Sort(actualNames)
	if !slices.Equal(actualNames, want) {
		return Result{}, fmt.Errorf("distribution artifacts %v do not match contract %v", actualNames, want)
	}
	for _, name := range want {
		actual, readErr := readBounded(filepath.Join(directory, name), maximumArtifactBytes)
		if readErr != nil {
			return Result{}, fmt.Errorf("read distribution artifact %q: %w", name, readErr)
		}
		if !bytes.Equal(actual, expected[name]) {
			return Result{}, fmt.Errorf("distribution artifact %q is not reproducible from commit %s", name, prepared.commit)
		}
	}
	if err := requireCurrentSource(ctx, prepared, runner); err != nil {
		return Result{}, err
	}
	return Result{OutputDir: directory, Commit: prepared.commit, Files: want}, nil
}

func prepare(
	ctx context.Context,
	options Options,
	value catalog.Catalog,
	runner process.Runner,
) (preparedRelease, error) {
	if ctx == nil {
		return preparedRelease{}, errors.New("distribution release context must not be nil")
	}
	if runner == nil {
		return preparedRelease{}, errors.New("distribution release runner must not be nil")
	}
	if err := value.Validate(); err != nil {
		return preparedRelease{}, err
	}
	repository, err := selectRepository(value, options.Repository, options.Version)
	if err != nil {
		return preparedRelease{}, err
	}
	root, err := realDirectory(options.Root, "repository root")
	if err != nil {
		return preparedRelease{}, err
	}
	if err := requireOrigin(ctx, root, repository, runner); err != nil {
		return preparedRelease{}, err
	}
	if err := requireClean(ctx, root, runner); err != nil {
		return preparedRelease{}, err
	}
	commit, epoch, err := requireTaggedHEAD(ctx, root, options.Version, runner)
	if err != nil {
		return preparedRelease{}, err
	}
	tree, err := requirePortableTree(ctx, root, commit)
	if err != nil {
		return preparedRelease{}, err
	}
	if err := requireIntentAndPayloads(ctx, root, commit, repository, tree); err != nil {
		return preparedRelease{}, err
	}
	scratchRoot, sourceRoot, err := materializeTaggedTree(ctx, root, commit, tree)
	if err != nil {
		return preparedRelease{}, err
	}
	keepScratch := false
	defer func() {
		if !keepScratch {
			_ = os.RemoveAll(scratchRoot)
		}
	}()
	goExecutable, err := resolveGoExecutable()
	if err != nil {
		return preparedRelease{}, err
	}
	if err := requireGoRuntime(ctx, sourceRoot, goExecutable, scratchRoot); err != nil {
		return preparedRelease{}, err
	}
	modules, err := requireModuleGraph(ctx, sourceRoot, repository, goExecutable, scratchRoot)
	if err != nil {
		return preparedRelease{}, err
	}
	keepScratch = true
	return preparedRelease{
		root: root, sourceRoot: sourceRoot, scratchRoot: scratchRoot, goExecutable: goExecutable,
		repository: repository, commit: commit, epoch: epoch, modules: modules,
	}, nil
}

func (prepared preparedRelease) cleanup() error {
	if prepared.scratchRoot == "" {
		return nil
	}
	if err := os.RemoveAll(prepared.scratchRoot); err != nil {
		return fmt.Errorf("remove distribution release scratch directory: %w", err)
	}
	return nil
}

func requireCurrentSource(
	ctx context.Context,
	prepared preparedRelease,
	runner process.Runner,
) error {
	if err := requireOrigin(ctx, prepared.root, prepared.repository, runner); err != nil {
		return err
	}
	if err := requireClean(ctx, prepared.root, runner); err != nil {
		return err
	}
	commit, epoch, err := requireTaggedHEAD(
		ctx,
		prepared.root,
		prepared.repository.Release.Version,
		runner,
	)
	if err != nil {
		return err
	}
	if commit != prepared.commit || epoch != prepared.epoch {
		return errors.New("distribution source identity changed during rendering")
	}
	return nil
}

func selectRepository(value catalog.Catalog, name string, version string) (catalog.Repository, error) {
	if strings.TrimSpace(name) == "" {
		return catalog.Repository{}, errors.New("distribution repository is required")
	}
	for _, repository := range value.Repositories {
		if repository.Name != name {
			continue
		}
		if strings.HasPrefix(repository.Name, "starter-") {
			return catalog.Repository{}, errors.New("starter repositories must use library-release")
		}
		if repository.Status != "active" || repository.Artifact != "go-module" || repository.Release == nil ||
			repository.Release.Profile != catalog.ReleaseProfileDistribution {
			return catalog.Repository{}, fmt.Errorf("repository %q is not authorized for go-distribution-v1", name)
		}
		if version != repository.Release.Version {
			return catalog.Repository{}, fmt.Errorf("distribution version %q does not match catalog authorization %q", version, repository.Release.Version)
		}
		return repository, nil
	}
	return catalog.Repository{}, fmt.Errorf("distribution repository %q is not in the catalog", name)
}

func requireOrigin(ctx context.Context, root string, repository catalog.Repository, runner process.Runner) error {
	actual, err := runner.Run(ctx, root, "git", "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("resolve distribution origin: %w", err)
	}
	actualIdentity, err := remoteIdentity(actual)
	if err != nil {
		return fmt.Errorf("parse distribution origin: %w", err)
	}
	expectedIdentity, err := remoteIdentity(repository.CloneURL)
	if err != nil {
		return fmt.Errorf("parse catalog clone URL: %w", err)
	}
	if actualIdentity != expectedIdentity {
		return fmt.Errorf("distribution origin %q does not match catalog clone URL %q", strings.TrimSpace(actual), repository.CloneURL)
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
		parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("require an HTTPS or SSH repository URL without explicit port, query, or fragment")
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
	return strings.ToLower(parsed.Hostname()) + "/" + repositoryPath, nil
}

func requireClean(ctx context.Context, root string, runner process.Runner) error {
	status, err := runner.Run(ctx, root, "git", "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect distribution checkout: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return errors.New("distribution checkout must be clean, including untracked files")
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
		return "", 0, fmt.Errorf("resolve distribution HEAD: %w", err)
	}
	commit = strings.TrimSpace(commit)
	if !commitPattern.MatchString(commit) {
		return "", 0, fmt.Errorf("distribution HEAD %q is not a full Git object ID", commit)
	}
	tag, err := runner.Run(ctx, root, "git", "rev-parse", "--verify", "refs/tags/"+version+"^{commit}")
	if err != nil {
		return "", 0, fmt.Errorf("resolve distribution tag %q: %w", version, err)
	}
	if strings.TrimSpace(tag) != commit {
		return "", 0, fmt.Errorf("distribution tag %q does not resolve to HEAD", version)
	}
	epochText, err := runner.Run(ctx, root, "git", "show", "-s", "--format=%ct", commit)
	if err != nil {
		return "", 0, fmt.Errorf("read distribution commit epoch: %w", err)
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(epochText), 10, 64)
	if err != nil || epoch <= 0 {
		return "", 0, fmt.Errorf("distribution commit epoch %q is invalid", strings.TrimSpace(epochText))
	}
	return commit, epoch, nil
}

func requireIntentAndPayloads(
	ctx context.Context,
	root string,
	commit string,
	repository catalog.Repository,
	tree map[string]treeEntry,
) error {
	required := []string{"go.mod", "go.sum", "vendor/modules.txt", repository.Release.MetadataFile}
	required = append(required, repository.Release.PayloadFiles...)
	for _, name := range required {
		entry, found := tree[name]
		if !found || entry.mode != "100644" {
			return fmt.Errorf("required committed distribution file %q is not a regular Git blob", name)
		}
	}
	content, err := gitBinary(ctx, root, maximumGitBytes, "show", commit+":"+repository.Release.MetadataFile)
	if err != nil {
		return fmt.Errorf("read committed distribution metadata: %w", err)
	}
	var actual intent
	if err := decodeStrict(content, &actual); err != nil {
		return fmt.Errorf("parse committed distribution metadata: %w", err)
	}
	want := intent{
		Schema: intentSchema, Profile: catalog.ReleaseProfileDistribution,
		Repository: repository.Name, Module: repository.Module, Version: repository.Release.Version,
	}
	if actual != want {
		return fmt.Errorf("committed distribution metadata %#v does not match catalog contract %#v", actual, want)
	}
	return validateArchivePaths(repository.Release)
}

func validateArchivePaths(policy *catalog.ReleasePolicy) error {
	seen := make(map[string]string)
	for _, binary := range policy.Binaries {
		for _, target := range policy.Targets {
			name := binary.Name
			if target.GOOS == "windows" {
				name += ".exe"
			}
			if err := portablePath(name); err != nil {
				return fmt.Errorf("distribution binary path %q: %w", name, err)
			}
			key := strings.ToLower(name)
			if prior, duplicate := seen[key]; duplicate && prior != name {
				return fmt.Errorf("distribution archive paths %q and %q collide", prior, name)
			}
			seen[key] = name
		}
	}
	for _, name := range policy.PayloadFiles {
		if err := portablePath(name); err != nil {
			return fmt.Errorf("distribution payload path %q: %w", name, err)
		}
		key := strings.ToLower(name)
		if prior, duplicate := seen[key]; duplicate {
			return fmt.Errorf("distribution archive paths %q and %q collide", prior, name)
		}
		seen[key] = name
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
		return "", fmt.Errorf("distribution %s is required", label)
	}
	absolute, err := filepath.Abs(configured)
	if err != nil {
		return "", fmt.Errorf("resolve distribution %s: %w", label, err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", fmt.Errorf("inspect distribution %s: %w", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("distribution %s %q is not a real directory", label, absolute)
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve real distribution %s: %w", label, err)
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return "", fmt.Errorf("resolve absolute real distribution %s: %w", label, err)
	}
	resolvedInfo, err := os.Stat(real)
	if err != nil || !resolvedInfo.IsDir() {
		return "", fmt.Errorf("distribution %s %q does not resolve to a directory", label, absolute)
	}
	return filepath.Clean(real), nil
}

func prepareOutput(repositoryRoot string, configured string) (string, string, error) {
	if strings.TrimSpace(configured) == "" {
		return "", "", errors.New("distribution output directory is required")
	}
	configuredOutput, err := filepath.Abs(configured)
	if err != nil {
		return "", "", fmt.Errorf("resolve distribution output: %w", err)
	}
	parent, err := realDirectory(filepath.Dir(configuredOutput), "output parent")
	if err != nil {
		return "", "", err
	}
	output := filepath.Join(parent, filepath.Base(configuredOutput))
	if pathWithin(repositoryRoot, output) {
		return "", "", errors.New("distribution output must be outside the repository")
	}
	if _, err := os.Lstat(output); err == nil {
		return "", "", fmt.Errorf("distribution output %q already exists", output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("inspect distribution output: %w", err)
	}
	staging, err := os.MkdirTemp(parent, ".spice-distribution-release-*")
	if err != nil {
		return "", "", fmt.Errorf("create distribution staging directory: %w", err)
	}
	return output, staging, nil
}

func pathWithin(root string, candidate string) bool {
	root = comparablePath(root)
	candidate = comparablePath(candidate)
	relative, err := filepath.Rel(root, candidate)
	return err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative))
}

func comparablePath(value string) string {
	value = filepath.Clean(value)
	if runtime.GOOS == "windows" {
		return strings.ToLower(value)
	}
	return value
}

func writeArtifact(directory string, name string, content []byte) error {
	if filepath.Base(name) != name || name == "." || name == ".." {
		return fmt.Errorf("distribution artifact name %q is unsafe", name)
	}
	file, err := os.OpenFile(filepath.Join(directory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create distribution artifact %q: %w", name, err)
	}
	written, writeErr := file.Write(content)
	if writeErr == nil && written != len(content) {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return fmt.Errorf("write distribution artifact %q: %w", name, err)
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

func artifactNames(artifacts map[string][]byte) []string {
	names := make([]string, 0, len(artifacts))
	for name := range artifacts {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func artifactChecksums(artifacts map[string][]byte) []byte {
	var output strings.Builder
	for _, name := range artifactNames(artifacts) {
		digest := sha256.Sum256(artifacts[name])
		fmt.Fprintf(&output, "%s  %s\n", hex.EncodeToString(digest[:]), name)
	}
	return []byte(output.String())
}
