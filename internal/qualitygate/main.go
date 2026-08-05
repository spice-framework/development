package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spice-framework/development/internal/catalog"
	"github.com/spice-framework/development/internal/process"
)

const minimumCoverage = 85.0

func main() {
	if err := run(context.Background()); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "development verification failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	return runAt(ctx, root, process.ExecRunner{})
}

func runAt(
	ctx context.Context,
	root string,
	runner process.Runner,
) error {
	steps := []struct {
		name string
		run  func() error
	}{
		{name: "Go toolchain", run: func() error { return verifyGo(ctx, root, runner) }},
		{name: "compatibility catalog", run: func() error {
			_, catalogErr := catalog.Default()
			return catalogErr
		}},
		{name: "formatting", run: func() error { return formatting(ctx, root, runner) }},
		{name: "module tidiness", run: func() error {
			return command(ctx, root, runner, "go", "mod", "tidy", "-diff")
		}},
		{name: "diff hygiene", run: func() error {
			return command(ctx, root, runner, "git", "diff", "--check")
		}},
		{name: "vet", run: func() error {
			return command(ctx, root, runner, "go", "vet", "./...")
		}},
		{name: "shuffled tests", run: func() error {
			return command(ctx, root, runner, "go", "test", "-shuffle=on", "-count=1", "./...")
		}},
		{name: "race tests", run: func() error {
			return command(ctx, root, runner, "go", "test", "-race", "-shuffle=on", "-count=1", "./...")
		}},
		{name: "coverage", run: func() error { return coverage(ctx, root, runner) }},
		{name: "trimpath build", run: func() error {
			return command(ctx, root, runner, "go", "build", "-trimpath", "./...")
		}},
	}
	for _, step := range steps {
		started := time.Now()
		if err := step.run(); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
		fmt.Printf("%-24s passed in %s\n", step.name, time.Since(started).Round(time.Millisecond))
	}
	return nil
}

func verifyGo(
	ctx context.Context,
	root string,
	runner process.Runner,
) error {
	output, err := runner.Run(ctx, root, "go", "version")
	if err != nil {
		return err
	}
	if !strings.Contains(output, "go version go1.26.5 ") {
		return fmt.Errorf("require Go 1.26.5, got %q", output)
	}
	return nil
}

func formatting(
	ctx context.Context,
	root string,
	runner process.Runner,
) error {
	arguments := []string{"gofmt", "-l"}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			arguments = append(arguments, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("discover Go source: %w", err)
	}
	output, err := runner.Run(ctx, root, arguments...)
	if err != nil {
		return err
	}
	if output != "" {
		return fmt.Errorf("files require gofmt:\n%s", output)
	}
	return nil
}

func command(
	ctx context.Context,
	root string,
	runner process.Runner,
	arguments ...string,
) error {
	output, err := runner.Run(ctx, root, arguments...)
	if output != "" {
		fmt.Println(output)
	}
	return err
}

func coverage(
	ctx context.Context,
	root string,
	runner process.Runner,
) error {
	directory, err := os.MkdirTemp("", "spice-development-coverage-*")
	if err != nil {
		return fmt.Errorf("create coverage directory: %w", err)
	}
	coverageErr := coverageInDirectory(ctx, root, directory, runner)
	cleanupErr := os.RemoveAll(directory)
	return errors.Join(coverageErr, cleanupErr)
}

func coverageInDirectory(
	ctx context.Context,
	root string,
	directory string,
	runner process.Runner,
) error {
	profile := filepath.Join(directory, "coverage.out")
	if err := command(
		ctx,
		root,
		runner,
		"go",
		"test",
		"-covermode=atomic",
		"-coverprofile="+profile,
		"./...",
	); err != nil {
		return err
	}
	output, err := runner.Run(ctx, root, "go", "tool", "cover", "-func="+profile)
	if err != nil {
		return err
	}
	total, err := totalCoverage(output)
	if err != nil {
		return err
	}
	fmt.Printf("repository coverage %.1f%%\n", total)
	if total < minimumCoverage {
		return fmt.Errorf("coverage %.1f%% is below %.1f%%", total, minimumCoverage)
	}
	return nil
}

func totalCoverage(output string) (float64, error) {
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "total:" {
			return strconv.ParseFloat(
				strings.TrimSuffix(fields[len(fields)-1], "%"),
				64,
			)
		}
	}
	return 0, errors.New("coverage report has no total")
}
