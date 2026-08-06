// Package verify orchestrates repository-owned checks with bounded concurrency.
package verify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spice-framework/development/internal/catalog"
	"github.com/spice-framework/development/internal/process"
)

type Mode string

const (
	Fast Mode = "fast"
	Full Mode = "full"
)

type Options struct {
	Root         string
	Mode         Mode
	Repositories []string
	Jobs         int
}

type Result struct {
	Repository string
	Commands   int
	Duration   time.Duration
	Output     string
	Err        error
}

func Run(
	ctx context.Context,
	value catalog.Catalog,
	options Options,
	runner process.Runner,
) ([]Result, error) {
	if ctx == nil {
		return nil, errors.New("verification context must not be nil")
	}
	if runner == nil {
		return nil, errors.New("verification runner must not be nil")
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	if options.Mode != Fast && options.Mode != Full {
		return nil, fmt.Errorf("verification mode %q is invalid", options.Mode)
	}
	if options.Jobs < 1 || options.Jobs > 32 {
		return nil, fmt.Errorf("verification jobs %d must be between 1 and 32", options.Jobs)
	}
	root, err := requireRoot(options.Root)
	if err != nil {
		return nil, err
	}
	repositories, err := selectRepositories(value.Active(), options.Repositories)
	if err != nil {
		return nil, err
	}
	results := make([]Result, len(repositories))
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	waves := verificationWaves(repositories)
	indexes := make(map[string]int, len(repositories))
	for index, repository := range repositories {
		indexes[repository.Name] = index
	}
	for _, wave := range waves {
		if workCtx.Err() != nil {
			break
		}
		runWave(
			workCtx,
			cancel,
			root,
			wave,
			indexes,
			results,
			value.StarterCompatibility,
			options.Mode,
			options.Jobs,
			runner,
		)
	}
	for index := range results {
		if results[index].Repository == "" {
			results[index] = Result{Repository: repositories[index].Name, Err: workCtx.Err()}
		}
	}
	var canceled *Result
	for _, result := range results {
		if result.Err == nil {
			continue
		}
		if errors.Is(result.Err, context.Canceled) {
			if canceled == nil {
				copy := result
				canceled = &copy
			}
			continue
		}
		if result.Err != nil {
			return results, fmt.Errorf("verify repository %s: %w", result.Repository, result.Err)
		}
	}
	if canceled != nil {
		return results, fmt.Errorf("verify repository %s: %w", canceled.Repository, canceled.Err)
	}
	return results, nil
}

// verificationWaves preserves catalog order while holding a selected
// repository until every selected dependency has completed. Dependencies that
// were not selected are treated as already satisfied, which keeps focused
// checks useful without weakening ordering inside the requested subgraph.
func verificationWaves(repositories []catalog.Repository) [][]catalog.Repository {
	selected := make(map[string]struct{}, len(repositories))
	remaining := make(map[string]catalog.Repository, len(repositories))
	for _, repository := range repositories {
		selected[repository.Name] = struct{}{}
		remaining[repository.Name] = repository
	}
	completed := make(map[string]struct{}, len(repositories))
	waves := make([][]catalog.Repository, 0)
	for len(remaining) != 0 {
		wave := make([]catalog.Repository, 0)
		for _, repository := range repositories {
			if _, pending := remaining[repository.Name]; !pending {
				continue
			}
			ready := true
			for _, dependency := range repository.Dependencies {
				if _, included := selected[dependency]; !included {
					continue
				}
				if _, done := completed[dependency]; !done {
					ready = false
					break
				}
			}
			if ready {
				wave = append(wave, repository)
			}
		}
		// Catalog validation rejects cycles, so an empty wave is unreachable.
		if len(wave) == 0 {
			break
		}
		waves = append(waves, wave)
		for _, repository := range wave {
			delete(remaining, repository.Name)
			completed[repository.Name] = struct{}{}
		}
	}
	return waves
}

func runWave(
	ctx context.Context,
	cancel context.CancelFunc,
	root string,
	wave []catalog.Repository,
	indexes map[string]int,
	results []Result,
	compatibility catalog.StarterCompatibilityPolicy,
	mode Mode,
	jobs int,
	runner process.Runner,
) {
	semaphore := make(chan struct{}, jobs)
	var group sync.WaitGroup
	for _, repository := range wave {
		index := indexes[repository.Name]
		group.Add(1)
		go func() {
			defer group.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results[index] = Result{Repository: repository.Name, Err: ctx.Err()}
				return
			}
			results[index] = runRepository(
				ctx,
				root,
				repository,
				compatibility,
				mode,
				runner,
			)
			if results[index].Err != nil {
				cancel()
			}
		}()
	}
	group.Wait()
}

func runRepository(
	ctx context.Context,
	root string,
	repository catalog.Repository,
	compatibility catalog.StarterCompatibilityPolicy,
	mode Mode,
	runner process.Runner,
) Result {
	started := time.Now()
	result := Result{Repository: repository.Name}
	directory := filepath.Join(root, repository.Directory)
	info, err := os.Lstat(directory)
	if err != nil {
		result.Err = fmt.Errorf("inspect checkout: %w", err)
		return result
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		result.Err = errors.New("checkout is not a real directory")
		return result
	}
	if compatibility.Applies(repository) {
		text, executed, compatibilityErr := verifyStarterCompatibility(
			ctx,
			directory,
			compatibility,
			runner,
		)
		if executed {
			result.Commands++
		}
		if text != "" {
			result.Output = "starter compatibility:\n" + text
		}
		if compatibilityErr != nil {
			result.Err = fmt.Errorf("starter compatibility: %w", compatibilityErr)
			result.Duration = time.Since(started)
			return result
		}
	}
	commands := repository.Fast
	if mode == Full {
		commands = repository.Full
	}
	var output []string
	for _, invocation := range commands {
		workingDirectory, directoryErr := invocationDirectory(
			directory,
			invocation.Directory,
		)
		if directoryErr != nil {
			result.Err = fmt.Errorf("%s: %w", invocation.Name, directoryErr)
			break
		}
		text, runErr := runner.Run(ctx, workingDirectory, invocation.Arguments...)
		result.Commands++
		if text != "" {
			output = append(output, invocation.Name+":\n"+text)
		}
		if runErr != nil {
			result.Err = fmt.Errorf("%s: %w", invocation.Name, runErr)
			break
		}
	}
	result.Duration = time.Since(started)
	if result.Output != "" {
		output = append([]string{result.Output}, output...)
	}
	result.Output = strings.Join(output, "\n")
	return result
}

func invocationDirectory(root string, relative string) (string, error) {
	if relative == "" {
		return root, nil
	}
	current := root
	for _, segment := range strings.Split(relative, "/") {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err != nil {
			return "", fmt.Errorf("inspect working directory %q: %w", relative, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("working directory %q is not a real directory", relative)
		}
	}
	return current, nil
}

func requireRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("verification root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve verification root: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", fmt.Errorf("inspect verification root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("verification root %q is not a real directory", absolute)
	}
	return absolute, nil
}

func selectRepositories(
	active []catalog.Repository,
	selected []string,
) ([]catalog.Repository, error) {
	if len(selected) == 0 {
		return slices.Clone(active), nil
	}
	wanted := make(map[string]struct{}, len(selected))
	for _, name := range selected {
		if _, duplicate := wanted[name]; duplicate {
			return nil, fmt.Errorf("repository %q was selected more than once", name)
		}
		wanted[name] = struct{}{}
	}
	result := make([]catalog.Repository, 0, len(wanted))
	for _, repository := range active {
		if _, included := wanted[repository.Name]; included {
			result = append(result, repository)
			delete(wanted, repository.Name)
		}
	}
	if len(wanted) != 0 {
		names := make([]string, 0, len(wanted))
		for name := range wanted {
			names = append(names, name)
		}
		slices.Sort(names)
		return nil, fmt.Errorf("unknown or inactive repositories: %s", strings.Join(names, ", "))
	}
	return result, nil
}
