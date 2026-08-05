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
	"slices"
	"strings"
)

const CurrentSchema = 2

//go:embed compatibility.json
var defaultContent []byte

type Catalog struct {
	Schema       int          `json:"schema"`
	Toolchains   Toolchains   `json:"toolchains"`
	Repositories []Repository `json:"repositories"`
}

type Toolchains struct {
	Go     string `json:"go"`
	Java   string `json:"java"`
	GoLand string `json:"goland"`
}

type Repository struct {
	Name            string       `json:"name"`
	Directory       string       `json:"directory"`
	Status          string       `json:"status"`
	CanonicalURL    string       `json:"canonical_url"`
	CloneURL        string       `json:"clone_url"`
	Artifact        string       `json:"artifact"`
	Module          string       `json:"module,omitempty"`
	CanonicalModule string       `json:"canonical_module,omitempty"`
	Dependencies    []string     `json:"dependencies"`
	Fast            []Invocation `json:"fast"`
	Full            []Invocation `json:"full"`
}

type Invocation struct {
	Name      string   `json:"name"`
	Directory string   `json:"directory,omitempty"`
	Arguments []string `json:"arguments"`
}

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
