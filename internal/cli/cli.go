// Package cli exposes the cross-platform spice-dev command boundary.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/spice-framework/development/internal/bootstrap"
	"github.com/spice-framework/development/internal/catalog"
	"github.com/spice-framework/development/internal/distributionrelease"
	"github.com/spice-framework/development/internal/gorelease"
	"github.com/spice-framework/development/internal/libraryrelease"
	"github.com/spice-framework/development/internal/process"
	"github.com/spice-framework/development/internal/snapshot"
	"github.com/spice-framework/development/internal/verify"
	"github.com/spice-framework/development/internal/workspace"
)

var Version = "0.1.0-dev"

type Runtime struct {
	Catalog catalog.Catalog
	Runner  process.Runner
}

func Main(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	value, err := catalog.Default()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "spice-dev failed: %v\n", err)
		return 1
	}
	return Runtime{Catalog: value, Runner: process.ExecRunner{}}.Run(
		ctx,
		arguments,
		stdout,
		stderr,
	)
}

func (runtime Runtime) Run(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	if ctx == nil || stdout == nil || stderr == nil {
		return 1
	}
	if len(arguments) == 0 || arguments[0] == "help" || arguments[0] == "-h" ||
		arguments[0] == "--help" {
		if err := printHelp(stdout); err != nil {
			return 1
		}
		return 0
	}
	var code int
	switch arguments[0] {
	case "version":
		code = writeVersion(stdout)
	case "catalog":
		code = runtime.catalogCommand(arguments[1:], stdout, stderr)
	case "bootstrap":
		code = runtime.bootstrapCommand(ctx, arguments[1:], stdout, stderr)
	case "workspace":
		code = runtime.workspaceCommand(arguments[1:], stdout, stderr)
	case "verify":
		code = runtime.verifyCommand(ctx, arguments[1:], stdout, stderr)
	case "library-release":
		code = runtime.libraryReleaseCommand(ctx, arguments[1:], stdout, stderr)
	case "go-release":
		code = runtime.goReleaseCommand(ctx, arguments[1:], stdout, stderr)
	case "distribution-release":
		code = runtime.distributionReleaseCommand(ctx, arguments[1:], stdout, stderr)
	case "snapshot":
		code = runtime.snapshotCommand(ctx, arguments[1:], stdout, stderr)
	default:
		if _, err := fmt.Fprintf(stderr, "spice-dev: unknown command %q\n", arguments[0]); err != nil {
			return 1
		}
		code = 2
	}
	return code
}

func (runtime Runtime) distributionReleaseCommand(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	if len(arguments) == 0 || (arguments[0] != "render" && arguments[0] != "verify") {
		return usageError(stderr, "distribution-release requires the render or verify subcommand")
	}
	subcommand := arguments[0]
	flags := flag.NewFlagSet("distribution-release "+subcommand, flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "clean tagged distribution repository root")
	repository := flags.String("repo", "", "catalog distribution repository name")
	version := flags.String("version", "", "catalog-authorized canonical release version")
	output := flags.String("output", "", "new deterministic distribution directory")
	artifacts := flags.String("artifacts", "", "existing distribution artifact directory")
	if err := flags.Parse(arguments[1:]); err != nil {
		return flagCode(err)
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "distribution-release "+subcommand+" accepts no positional arguments")
	}
	if subcommand == "render" && *artifacts != "" {
		return usageError(stderr, "distribution-release render does not accept --artifacts")
	}
	if subcommand == "verify" && *output != "" {
		return usageError(stderr, "distribution-release verify does not accept --output")
	}
	options := distributionrelease.Options{Root: *root, Repository: *repository, Version: *version}
	var result distributionrelease.Result
	var err error
	if subcommand == "render" {
		result, err = distributionrelease.Render(ctx, options, runtime.Catalog, runtime.Runner, *output)
	} else {
		result, err = distributionrelease.Verify(ctx, options, runtime.Catalog, runtime.Runner, *artifacts)
	}
	if err != nil {
		return commandError(stderr, "distribution-release "+subcommand, err)
	}
	if _, err := fmt.Fprintf(stdout, "%s\t%s\t%d artifact(s)\n", result.OutputDir, result.Commit, len(result.Files)); err != nil {
		return 1
	}
	return 0
}

func (runtime Runtime) goReleaseCommand(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	if len(arguments) == 0 ||
		(arguments[0] != "policy-check" && arguments[0] != "render" && arguments[0] != "verify") {
		return usageError(stderr, "go-release requires the policy-check, render, or verify subcommand")
	}
	subcommand := arguments[0]
	if subcommand == "policy-check" {
		return runtime.goReleasePolicyCheckCommand(arguments[1:], stdout, stderr)
	}
	flags := flag.NewFlagSet("go-release "+subcommand, flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "clean tagged Go repository root")
	repository := flags.String("repo", "", "catalog repository name")
	version := flags.String("version", "", "catalog-authorized canonical release version")
	output := flags.String("output", "", "new deterministic release directory")
	artifacts := flags.String("artifacts", "", "existing release artifact directory")
	if err := flags.Parse(arguments[1:]); err != nil {
		return flagCode(err)
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "go-release "+subcommand+" accepts no positional arguments")
	}
	if subcommand == "render" && *artifacts != "" {
		return usageError(stderr, "go-release render does not accept --artifacts")
	}
	if subcommand == "verify" && *output != "" {
		return usageError(stderr, "go-release verify does not accept --output")
	}
	options := gorelease.Options{Root: *root, Repository: *repository, Version: *version}
	var result gorelease.Result
	var err error
	if subcommand == "render" {
		result, err = gorelease.Render(ctx, options, runtime.Catalog, runtime.Runner, *output)
	} else {
		result, err = gorelease.Verify(ctx, options, runtime.Catalog, runtime.Runner, *artifacts)
	}
	if err != nil {
		return commandError(stderr, "go-release "+subcommand, err)
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s\t%s\t%d artifact(s)\n",
		result.OutputDir,
		result.Commit,
		len(result.Files),
	); err != nil {
		return 1
	}
	return 0
}

func (runtime Runtime) goReleasePolicyCheckCommand(
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("go-release policy-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repo", "", "catalog repository name")
	module := flags.String("module", "", "canonical Go module path")
	version := flags.String("version", "", "catalog-authorized canonical release version")
	profile := flags.String("profile", "", "catalog release profile")
	if err := flags.Parse(arguments); err != nil {
		return flagCode(err)
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "go-release policy-check accepts no positional arguments")
	}
	policy, err := gorelease.CheckPolicy(gorelease.PolicyOptions{
		Repository: *repository,
		Module:     *module,
		Version:    *version,
		Profile:    *profile,
	}, runtime.Catalog)
	if err != nil {
		return commandError(stderr, "go-release policy-check", err)
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s\t%s\t%s\t%s\n",
		policy.Profile,
		policy.Repository,
		policy.Module,
		policy.Version,
	); err != nil {
		return 1
	}
	return 0
}

func (runtime Runtime) snapshotCommand(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	if len(arguments) == 0 || (arguments[0] != "materialize" && arguments[0] != "verify") {
		return usageError(stderr, "snapshot requires the materialize or verify subcommand")
	}
	subcommand := arguments[0]
	flags := flag.NewFlagSet("snapshot "+subcommand, flag.ContinueOnError)
	flags.SetOutput(stderr)
	lockFile := flags.String("lock", "", "exact-repository snapshot lock JSON file")
	root := flags.String("root", "", "new or existing materialized snapshot root")
	offline := flags.Bool("offline", false, "require the verification-only offline mode")
	if err := flags.Parse(arguments[1:]); err != nil {
		return flagCode(err)
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "snapshot "+subcommand+" accepts no positional arguments")
	}
	if subcommand == "materialize" && *offline {
		return usageError(stderr, "snapshot materialize is the explicit online operation and does not accept --offline")
	}
	if subcommand == "verify" && !*offline {
		return usageError(stderr, "snapshot verify requires --offline")
	}
	lock, err := snapshot.Load(*lockFile, runtime.Catalog)
	if err != nil {
		return commandError(stderr, "snapshot "+subcommand, err)
	}
	var manifest snapshot.Manifest
	if subcommand == "materialize" {
		manifest, err = snapshot.Materialize(ctx, lock, *root, runtime.Catalog, runtime.Runner)
	} else {
		manifest, err = snapshot.Verify(ctx, lock, *root, runtime.Catalog, runtime.Runner)
	}
	if err != nil {
		return commandError(stderr, "snapshot "+subcommand, err)
	}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return commandError(stderr, "snapshot "+subcommand, err)
	}
	if _, err := stdout.Write(append(content, '\n')); err != nil {
		return 1
	}
	return 0
}

func (runtime Runtime) libraryReleaseCommand(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	if len(arguments) == 0 {
		return usageError(stderr, "library-release requires the plan, public-key, render, or sign subcommand")
	}
	switch arguments[0] {
	case "public-key":
		return runtime.libraryReleasePublicKeyCommand(ctx, arguments[1:], stdout, stderr)
	case "render":
		return runtime.libraryReleaseRenderCommand(ctx, arguments[1:], stdout, stderr)
	case "sign":
		return runtime.libraryReleaseSignCommand(ctx, arguments[1:], stdout, stderr)
	case "plan":
	default:
		return usageError(stderr, "library-release requires the plan, public-key, render, or sign subcommand")
	}
	flags := flag.NewFlagSet("library-release plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "library repository root")
	repository := flags.String("repo", "", "catalog library repository")
	version := flags.String("version", "", "canonical v-prefixed release version")
	rehearsal := flags.Bool("rehearsal", false, "plan an unsigned untagged rehearsal")
	epoch := flags.Int64("source-date-epoch", 0, "require this exact source commit epoch")
	if err := flags.Parse(arguments[1:]); err != nil {
		return flagCode(err)
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "library-release plan accepts no positional arguments")
	}
	plan, err := libraryrelease.CreatePlan(ctx, runtime.Catalog, libraryrelease.Options{
		Root: *root, Repository: *repository, Version: *version,
		Rehearsal: *rehearsal, SourceDateEpoch: *epoch,
	}, runtime.Runner)
	if err != nil {
		return commandError(stderr, "library-release plan", err)
	}
	content, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return commandError(stderr, "library-release plan", err)
	}
	if _, err := stdout.Write(append(content, '\n')); err != nil {
		return 1
	}
	return 0
}

func (runtime Runtime) libraryReleasePublicKeyCommand(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("library-release public-key", flag.ContinueOnError)
	flags.SetOutput(stderr)
	privateKey := flags.String("signing-key", "", "existing Ed25519 private-key file")
	output := flags.String("output", "", "new canonical Ed25519 public-key PEM file")
	if err := flags.Parse(arguments); err != nil {
		return flagCode(err)
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "library-release public-key accepts no positional arguments")
	}
	written, err := libraryrelease.WritePublicKey(ctx, *privateKey, *output)
	if err != nil {
		return commandError(stderr, "library-release public-key", err)
	}
	if _, err := fmt.Fprintf(stdout, "%s\tEd25519 PKIX public key\n", written); err != nil {
		return 1
	}
	return 0
}

func (runtime Runtime) libraryReleaseSignCommand(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("library-release sign", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "library repository root")
	planFile := flags.String("plan", "", "validated production release plan JSON file")
	output := flags.String("output", "", "new release output directory outside the repository")
	privateKey := flags.String("signing-key", "", "Ed25519 private-key file outside the repository")
	publicKey := flags.String("trusted-public-key", "", "independently trusted Ed25519 public-key PEM file")
	if err := flags.Parse(arguments); err != nil {
		return flagCode(err)
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "library-release sign accepts no positional arguments")
	}
	plan, err := libraryrelease.LoadPlan(*planFile)
	if err != nil {
		return commandError(stderr, "library-release sign", err)
	}
	result, err := libraryrelease.Sign(
		ctx,
		*root,
		*output,
		plan,
		runtime.Catalog,
		libraryrelease.SigningFiles{
			PrivateKey:       *privateKey,
			TrustedPublicKey: *publicKey,
		},
	)
	if err != nil {
		return commandError(stderr, "library-release sign", err)
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s\t%s\t%d signed artifact(s)\n",
		result.OutputDir,
		plan.Commit,
		len(result.Files),
	); err != nil {
		return 1
	}
	return 0
}

func (runtime Runtime) libraryReleaseRenderCommand(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("library-release render", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "library repository root")
	planFile := flags.String("plan", "", "validated release plan JSON file")
	output := flags.String("output", "", "new release output directory")
	if err := flags.Parse(arguments); err != nil {
		return flagCode(err)
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "library-release render accepts no positional arguments")
	}
	plan, err := libraryrelease.LoadPlan(*planFile)
	if err != nil {
		return commandError(stderr, "library-release render", err)
	}
	result, err := libraryrelease.Render(ctx, *root, *output, plan, runtime.Catalog)
	if err != nil {
		return commandError(stderr, "library-release render", err)
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s\t%s\t%d artifact(s)\n",
		result.OutputDir,
		plan.Commit,
		len(result.Files),
	); err != nil {
		return 1
	}
	return 0
}

func (runtime Runtime) catalogCommand(
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "print the exact compatibility catalog as JSON")
	if err := flags.Parse(arguments); err != nil {
		return flagCode(err)
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "catalog accepts no positional arguments")
	}
	if *asJSON {
		content, err := json.MarshalIndent(runtime.Catalog, "", "  ")
		if err != nil {
			return commandError(stderr, "catalog", err)
		}
		content = append(content, '\n')
		if _, err := stdout.Write(content); err != nil {
			return 1
		}
		return 0
	}
	for _, repository := range runtime.Catalog.Repositories {
		if _, err := fmt.Fprintf(
			stdout,
			"%s\t%s\t%s\t%s\n",
			repository.Name,
			repository.Status,
			repository.Artifact,
			repository.CanonicalURL,
		); err != nil {
			return 1
		}
	}
	return 0
}

func (runtime Runtime) bootstrapCommand(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "workspace root")
	offline := flags.Bool("offline", false, "validate without cloning or fetching")
	if err := flags.Parse(arguments); err != nil {
		return flagCode(err)
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "bootstrap accepts no positional arguments")
	}
	results, err := bootstrap.Ensure(ctx, *root, runtime.Catalog, *offline, runtime.Runner)
	if err != nil {
		return commandError(stderr, "bootstrap", err)
	}
	for _, result := range results {
		if _, err := fmt.Fprintf(
			stdout,
			"%s\t%s\t%s\n",
			result.Repository,
			result.Action,
			result.Directory,
		); err != nil {
			return 1
		}
	}
	return 0
}

func (runtime Runtime) workspaceCommand(
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("workspace", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "workspace root")
	check := flags.Bool("check", false, "require current generated workspace files")
	if err := flags.Parse(arguments); err != nil {
		return flagCode(err)
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "workspace accepts no positional arguments")
	}
	plan, err := workspace.Render(*root, runtime.Catalog)
	if err != nil {
		return commandError(stderr, "workspace", err)
	}
	if err := workspace.Apply(*root, plan, *check); err != nil {
		return commandError(stderr, "workspace", err)
	}
	action := "updated"
	if *check {
		action = "current"
	}
	if _, err := fmt.Fprintf(stdout, "workspace %s: go.work, spice.code-workspace\n", action); err != nil {
		return 1
	}
	return 0
}

func (runtime Runtime) verifyCommand(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "workspace root")
	full := flags.Bool("full", false, "run complete repository gates")
	jobs := flags.Int("jobs", min(4, runtime2GOMAXPROCS()), "maximum concurrent repositories")
	var repositories stringList
	flags.Var(&repositories, "repo", "repository to verify; repeat to select multiple")
	if err := flags.Parse(arguments); err != nil {
		return flagCode(err)
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "verify accepts no positional arguments")
	}
	mode := verify.Fast
	if *full {
		mode = verify.Full
	}
	results, err := verify.Run(ctx, runtime.Catalog, verify.Options{
		Root: *root, Mode: mode, Repositories: repositories, Jobs: *jobs,
	}, runtime.Runner)
	for _, result := range results {
		status := "passed"
		if result.Err != nil {
			status = "failed"
		}
		if _, writeErr := fmt.Fprintf(
			stdout,
			"%s\t%s\t%d command(s)\t%s\n",
			result.Repository,
			status,
			result.Commands,
			result.Duration.Round(10_000_000),
		); writeErr != nil {
			return 1
		}
		if result.Output != "" {
			if _, writeErr := fmt.Fprintln(stdout, result.Output); writeErr != nil {
				return 1
			}
		}
	}
	if err != nil {
		return commandError(stderr, "verify", err)
	}
	return 0
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }

func (values *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("repository name must not be empty")
	}
	*values = append(*values, value)
	return nil
}

func runtime2GOMAXPROCS() int { return runtime.GOMAXPROCS(0) }

func writeVersion(writer io.Writer) int {
	if _, err := fmt.Fprintln(writer, Version); err != nil {
		return 1
	}
	return 0
}

func printHelp(writer io.Writer) error {
	_, err := io.WriteString(writer, `spice-dev manages the Spice multi-repository workspace.

Usage:
  spice-dev version
  spice-dev catalog [--json]
  spice-dev bootstrap --root path [--offline]
  spice-dev workspace --root path [--check]
  spice-dev verify --root path [--full] [--jobs n] [--repo name ...]
  spice-dev snapshot materialize --lock lock.json --root new-path
  spice-dev snapshot verify --lock lock.json --root path --offline
  spice-dev library-release plan --root path --repo name --version vX.Y.Z [--rehearsal]
  spice-dev library-release public-key --signing-key external-private-key --output new-public.pem
  spice-dev library-release render --root path --plan plan.json --output new-path
  spice-dev library-release sign --root path --plan plan.json --output new-path --signing-key key --trusted-public-key public.pem
  spice-dev go-release policy-check --repo name --module path --version vX.Y.Z --profile go-module-v1
  spice-dev go-release render --root path --repo name --version vX.Y.Z --output new-path
  spice-dev go-release verify --root path --repo name --version vX.Y.Z --artifacts path
  spice-dev distribution-release render --root path --repo name --version vX.Y.Z --output new-path
  spice-dev distribution-release verify --root path --repo name --version vX.Y.Z --artifacts path
`)
	return err
}

func usageError(writer io.Writer, message string) int {
	if _, err := fmt.Fprintf(writer, "spice-dev: %s\n", message); err != nil {
		return 1
	}
	return 2
}

func commandError(writer io.Writer, command string, err error) int {
	if _, writeErr := fmt.Fprintf(writer, "spice-dev %s failed: %v\n", command, err); writeErr != nil {
		return 1
	}
	return 1
}

func flagCode(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}
