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
	semaphore := make(chan struct{}, options.Jobs)
	var group sync.WaitGroup
	for index, repository := range repositories {
		group.Add(1)
		go func() {
			defer group.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-workCtx.Done():
				results[index] = Result{Repository: repository.Name, Err: workCtx.Err()}
				return
			}
			results[index] = runRepository(
				workCtx,
				root,
				repository,
				options.Mode,
				runner,
			)
			if results[index].Err != nil {
				cancel()
			}
		}()
	}
	group.Wait()
	for _, result := range results {
		if result.Err != nil {
			return results, fmt.Errorf("verify repository %s: %w", result.Repository, result.Err)
		}
	}
	return results, nil
}

func runRepository(
	ctx context.Context,
	root string,
	repository catalog.Repository,
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
	commands := repository.Fast
	if mode == Full {
		commands = repository.Full
	}
	var output []string
	for _, invocation := range commands {
		text, runErr := runner.Run(ctx, directory, invocation.Arguments...)
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
	result.Output = strings.Join(output, "\n")
	return result
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
