// Package catalog owns the versioned Spice ecosystem compatibility graph.
package catalog

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const CurrentSchema = 4

const (
	ReleaseProfileGoModule     = "go-module-v1"
	ReleaseProfileDistribution = "go-distribution-v1"
)

//go:embed compatibility.json
var defaultContent []byte

type Catalog struct {
	Schema               int                        `json:"schema"`
	Toolchains           Toolchains                 `json:"toolchains"`
	StarterCompatibility StarterCompatibilityPolicy `json:"starter_compatibility"`
	Repositories         []Repository               `json:"repositories"`
}

type Toolchains struct {
	Go     string `json:"go"`
	Java   string `json:"java"`
	GoLand string `json:"goland"`
}

type Repository struct {
	Name            string         `json:"name"`
	Directory       string         `json:"directory"`
	Status          string         `json:"status"`
	CanonicalURL    string         `json:"canonical_url"`
	CloneURL        string         `json:"clone_url"`
	Artifact        string         `json:"artifact"`
	Module          string         `json:"module,omitempty"`
	CanonicalModule string         `json:"canonical_module,omitempty"`
	Dependencies    []string       `json:"dependencies"`
	Release         *ReleasePolicy `json:"release,omitempty"`
	Fast            []Invocation   `json:"fast"`
	Full            []Invocation   `json:"full"`
}

// ReleasePolicy is the catalog-owned authorization for one generic Go-module
// or binary-distribution release. Starter releases deliberately remain under
// StarterCompatibilityPolicy and may not use this path.
type ReleasePolicy struct {
	Profile         string          `json:"profile,omitempty"`
	Version         string          `json:"version,omitempty"`
	MetadataFile    string          `json:"metadata_file,omitempty"`
	RequiredModules []string        `json:"required_modules,omitempty"`
	Binaries        []ReleaseBinary `json:"binaries,omitempty"`
	Targets         []ReleaseTarget `json:"targets,omitempty"`
	PayloadFiles    []string        `json:"payload_files,omitempty"`
}

type ReleaseBinary struct {
	Name    string `json:"name"`
	Package string `json:"package"`
}

type ReleaseTarget struct {
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
}

type Invocation struct {
	Name      string   `json:"name"`
	Directory string   `json:"directory,omitempty"`
	Arguments []string `json:"arguments"`
}

// StarterCompatibilityPolicy is the organization-wide compatibility contract
// applied to every active starter repository in the catalog.
type StarterCompatibilityPolicy struct {
	RepositoryPrefix string `json:"repository_prefix"`
	MetadataFile     string `json:"metadata_file"`
	MetadataSchema   int    `json:"metadata_schema"`
	CoreModule       string `json:"core_module"`
	CurrentCore      string `json:"current_core"`
}

// StarterCompatibility is the strict metadata stored by each starter.
type StarterCompatibility struct {
	Schema  int    `json:"schema"`
	Minimum string `json:"minimum"`
	Current string `json:"current"`
}

var moduleVersionPattern = regexp.MustCompile(
	`^v([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`,
)

func Default() (Catalog, error) {
	return Parse(defaultContent)
}

func Parse(content []byte) (Catalog, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var result Catalog
	if err := decoder.Decode(&result); err != nil {
		return Catalog{}, fmt.Errorf("decode compatibility catalog: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Catalog{}, errors.New("compatibility catalog has trailing JSON values")
		}
		return Catalog{}, fmt.Errorf("decode trailing compatibility content: %w", err)
	}
	if err := result.Validate(); err != nil {
		return Catalog{}, err
	}
	return result, nil
}

func (value Catalog) Validate() error {
	if value.Schema != CurrentSchema {
		return fmt.Errorf("compatibility schema %d is unsupported", value.Schema)
	}
	if value.Toolchains.Go == "" || value.Toolchains.Java == "" ||
		value.Toolchains.GoLand == "" {
		return errors.New("compatibility toolchain versions must be explicit")
	}
	if err := value.StarterCompatibility.validate(); err != nil {
		return err
	}
	if len(value.Repositories) == 0 {
		return errors.New("compatibility catalog has no repositories")
	}
	byName := make(map[string]Repository, len(value.Repositories))
	directories := make(map[string]string, len(value.Repositories))
	for _, repository := range value.Repositories {
		if err := validateRepository(repository); err != nil {
			return err
		}
		if _, exists := byName[repository.Name]; exists {
			return fmt.Errorf("repository name %q is duplicated", repository.Name)
		}
		if owner, exists := directories[repository.Directory]; exists {
			return fmt.Errorf(
				"repository directory %q is shared by %q and %q",
				repository.Directory,
				owner,
				repository.Name,
			)
		}
		byName[repository.Name] = repository
		directories[repository.Directory] = repository.Name
	}
	starters := make([]Repository, 0)
	for _, repository := range value.Repositories {
		if value.StarterCompatibility.Applies(repository) {
			starters = append(starters, repository)
		}
	}
	if len(starters) != 0 {
		coreOwner := ""
		for _, repository := range value.Repositories {
			if repository.Module == value.StarterCompatibility.CoreModule {
				coreOwner = repository.Name
				break
			}
		}
		if coreOwner == "" {
			return fmt.Errorf(
				"starter compatibility core module %q has no catalog repository",
				value.StarterCompatibility.CoreModule,
			)
		}
		for _, repository := range starters {
			if repository.Artifact != "go-module" || repository.Module == "" {
				return fmt.Errorf(
					"starter repository %q must be a Go module",
					repository.Name,
				)
			}
			if !slices.Contains(repository.Dependencies, coreOwner) {
				return fmt.Errorf(
					"starter repository %q must depend on core repository %q",
					repository.Name,
					coreOwner,
				)
			}
		}
	}
	for _, repository := range value.Repositories {
		for _, dependency := range repository.Dependencies {
			if _, exists := byName[dependency]; !exists {
				return fmt.Errorf(
					"repository %q depends on unknown repository %q",
					repository.Name,
					dependency,
				)
			}
		}
	}
	if cycle := dependencyCycle(value.Repositories); len(cycle) != 0 {
		return fmt.Errorf("repository dependency cycle: %s", strings.Join(cycle, " -> "))
	}
	return nil
}

// Applies reports whether the central policy governs a repository.
func (policy StarterCompatibilityPolicy) Applies(repository Repository) bool {
	return (repository.Status == "active" || repository.Status == "migrating") &&
		strings.HasPrefix(repository.Name, policy.RepositoryPrefix)
}

func (policy StarterCompatibilityPolicy) validate() error {
	if policy.RepositoryPrefix == "" || strings.TrimSpace(policy.RepositoryPrefix) != policy.RepositoryPrefix {
		return errors.New("starter compatibility repository prefix must be explicit")
	}
	if policy.MetadataFile == "" || filepath.Base(policy.MetadataFile) != policy.MetadataFile ||
		policy.MetadataFile == "." || policy.MetadataFile == ".." {
		return fmt.Errorf(
			"starter compatibility metadata file %q must be one safe file name",
			policy.MetadataFile,
		)
	}
	if policy.MetadataSchema < 1 {
		return errors.New("starter compatibility metadata schema must be positive")
	}
	if strings.TrimSpace(policy.CoreModule) == "" {
		return errors.New("starter compatibility core module must be explicit")
	}
	if !ValidModuleVersion(policy.CurrentCore) {
		return fmt.Errorf(
			"starter compatibility current core version %q is malformed",
			policy.CurrentCore,
		)
	}
	return nil
}

// ParseStarterCompatibility strictly decodes one starter's compatibility
// metadata and binds its current boundary to the catalog-wide core revision.
func ParseStarterCompatibility(
	content []byte,
	policy StarterCompatibilityPolicy,
) (StarterCompatibility, error) {
	if err := policy.validate(); err != nil {
		return StarterCompatibility{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var result StarterCompatibility
	if err := decoder.Decode(&result); err != nil {
		return StarterCompatibility{}, fmt.Errorf("decode starter compatibility metadata: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return StarterCompatibility{}, errors.New(
				"starter compatibility metadata has trailing JSON values",
			)
		}
		return StarterCompatibility{}, fmt.Errorf(
			"decode trailing starter compatibility metadata: %w",
			err,
		)
	}
	if result.Schema != policy.MetadataSchema {
		return StarterCompatibility{}, fmt.Errorf(
			"starter compatibility schema %d does not match catalog schema %d",
			result.Schema,
			policy.MetadataSchema,
		)
	}
	if !ValidModuleVersion(result.Minimum) {
		return StarterCompatibility{}, fmt.Errorf(
			"starter compatibility minimum version %q is malformed",
			result.Minimum,
		)
	}
	if !ValidModuleVersion(result.Current) {
		return StarterCompatibility{}, fmt.Errorf(
			"starter compatibility current version %q is malformed",
			result.Current,
		)
	}
	if result.Current != policy.CurrentCore {
		return StarterCompatibility{}, fmt.Errorf(
			"starter compatibility current version %q is stale; catalog requires %q",
			result.Current,
			policy.CurrentCore,
		)
	}
	return result, nil
}

// ValidModuleVersion reports whether version is a canonical v-prefixed Go
// module version without leading-zero numeric identifiers.
func ValidModuleVersion(version string) bool {
	matches := moduleVersionPattern.FindStringSubmatch(version)
	if matches == nil {
		return false
	}
	for _, core := range matches[1:4] {
		if len(core) > 1 && core[0] == '0' {
			return false
		}
	}
	for identifier := range strings.SplitSeq(matches[4], ".") {
		if len(identifier) > 1 && identifier[0] == '0' &&
			strings.Trim(identifier, "0123456789") == "" {
			return false
		}
	}
	return true
}

func (value Catalog) Active() []Repository {
	result := make([]Repository, 0, len(value.Repositories))
	for _, repository := range value.Repositories {
		if repository.Status == "active" || repository.Status == "migrating" {
			result = append(result, repository)
		}
	}
	return result
}

func validateRepository(repository Repository) error {
	if strings.TrimSpace(repository.Name) == "" {
		return errors.New("repository name must not be empty")
	}
	if repository.Directory == "" || filepath.Base(repository.Directory) != repository.Directory ||
		repository.Directory == "." || repository.Directory == ".." {
		return fmt.Errorf("repository %q has unsafe directory %q", repository.Name, repository.Directory)
	}
	if repository.Status != "active" && repository.Status != "migrating" &&
		repository.Status != "planned" {
		return fmt.Errorf("repository %q has invalid status %q", repository.Name, repository.Status)
	}
	if err := validateGitHubURL(repository.CanonicalURL, false); err != nil {
		return fmt.Errorf("repository %q canonical URL: %w", repository.Name, err)
	}
	if err := validateGitHubURL(repository.CloneURL, true); err != nil {
		return fmt.Errorf("repository %q clone URL: %w", repository.Name, err)
	}
	if repository.Artifact == "go-module" && repository.Module == "" {
		return fmt.Errorf("repository %q has no Go module path", repository.Name)
	}
	if err := repository.Release.validate(repository); err != nil {
		return fmt.Errorf("repository %q release policy: %w", repository.Name, err)
	}
	for _, invocation := range append(slices.Clone(repository.Fast), repository.Full...) {
		if invocation.Name == "" || len(invocation.Arguments) == 0 ||
			invocation.Arguments[0] == "" {
			return fmt.Errorf("repository %q has an invalid verification invocation", repository.Name)
		}
		if err := validateInvocationDirectory(invocation.Directory); err != nil {
			return fmt.Errorf(
				"repository %q invocation %q directory: %w",
				repository.Name,
				invocation.Name,
				err,
			)
		}
	}
	return nil
}

func (policy *ReleasePolicy) validate(repository Repository) error {
	if policy == nil {
		return nil
	}
	if policy.Profile == "" {
		return errors.New("profile is required")
	}
	if strings.HasPrefix(repository.Name, "starter-") {
		return errors.New("starter repositories must use the starter release path")
	}
	if repository.Artifact != "go-module" || repository.Module == "" {
		return errors.New("generic release profiles require a Go module repository")
	}
	if !ValidModuleVersion(policy.Version) {
		return fmt.Errorf("version %q is malformed", policy.Version)
	}
	if !safeReleaseFile(policy.MetadataFile) {
		return fmt.Errorf("metadata file %q must be one safe file name", policy.MetadataFile)
	}
	if err := validateDistinctStrings("required module", policy.RequiredModules); err != nil {
		return err
	}
	switch policy.Profile {
	case ReleaseProfileGoModule:
		if len(policy.Binaries) != 0 || len(policy.Targets) != 0 || len(policy.PayloadFiles) != 0 {
			return errors.New("Go module release policy cannot define distribution payloads")
		}
	case ReleaseProfileDistribution:
		if len(policy.Binaries) == 0 || len(policy.Targets) == 0 || len(policy.PayloadFiles) == 0 {
			return errors.New("distribution release policy requires binaries, targets, and payload files")
		}
		if err := validateReleaseBinaries(policy.Binaries); err != nil {
			return err
		}
		if err := validateReleaseTargets(policy.Targets); err != nil {
			return err
		}
		for _, file := range policy.PayloadFiles {
			if err := validateInvocationDirectory(file); err != nil {
				return fmt.Errorf("payload file: %w", err)
			}
		}
	default:
		return fmt.Errorf("profile %q is unsupported", policy.Profile)
	}
	return nil
}

func validateReleaseBinaries(values []ReleaseBinary) error {
	names := make([]string, 0, len(values))
	for _, value := range values {
		if !safeReleaseFile(value.Name) {
			return fmt.Errorf("binary name %q must be one safe file name", value.Name)
		}
		relativePackage := strings.TrimPrefix(value.Package, "./")
		if !strings.HasPrefix(value.Package, "./cmd/") || path.Clean(relativePackage) != relativePackage {
			return fmt.Errorf("binary package %q must be a clean ./cmd path", value.Package)
		}
		names = append(names, value.Name)
	}
	return validateDistinctStrings("binary name", names)
}

func validateReleaseTargets(values []ReleaseTarget) error {
	identities := make([]string, 0, len(values))
	for _, value := range values {
		if !slices.Contains([]string{"linux", "darwin", "windows"}, value.GOOS) ||
			!slices.Contains([]string{"amd64", "arm64"}, value.GOARCH) {
			return fmt.Errorf("target %q/%q is unsupported", value.GOOS, value.GOARCH)
		}
		identities = append(identities, value.GOOS+"/"+value.GOARCH)
	}
	return validateDistinctStrings("release target", identities)
}

func validateDistinctStrings(kind string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
			return fmt.Errorf("%s %q is invalid", kind, value)
		}
		if _, duplicate := seen[value]; duplicate {
			return fmt.Errorf("%s %q is duplicated", kind, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func safeReleaseFile(value string) bool {
	return value != "" && filepath.Base(value) == value && value != "." && value != ".."
}

func validateInvocationDirectory(directory string) error {
	if directory == "" {
		return nil
	}
	if strings.ContainsAny(directory, `\:`) || strings.ContainsRune(directory, 0) ||
		path.IsAbs(directory) || path.Clean(directory) != directory ||
		directory == "." || directory == ".." || strings.HasPrefix(directory, "../") {
		return fmt.Errorf("%q must be a clean relative slash-separated path", directory)
	}
	return nil
}

func validateGitHubURL(raw string, clone bool) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" || parsed.Host != "github.com" ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%q must be an HTTPS github.com URL", raw)
	}
	if clone != strings.HasSuffix(parsed.Path, ".git") {
		return fmt.Errorf("%q has an invalid .git suffix", raw)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || strings.TrimSuffix(parts[1], ".git") == "" {
		return fmt.Errorf("%q must identify one repository", raw)
	}
	return nil
}

func dependencyCycle(repositories []Repository) []string {
	dependencies := make(map[string][]string, len(repositories))
	for _, repository := range repositories {
		dependencies[repository.Name] = repository.Dependencies
	}
	state := make(map[string]uint8, len(repositories))
	var stack []string
	var visit func(string) []string
	visit = func(name string) []string {
		if state[name] == 1 {
			index := slices.Index(stack, name)
			return append(slices.Clone(stack[index:]), name)
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		stack = append(stack, name)
		for _, dependency := range dependencies[name] {
			if cycle := visit(dependency); len(cycle) != 0 {
				return cycle
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = 2
		return nil
	}
	for _, repository := range repositories {
		if cycle := visit(repository.Name); len(cycle) != 0 {
			return cycle
		}
	}
	return nil
}
