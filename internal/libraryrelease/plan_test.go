package libraryrelease

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spice-framework/development/internal/catalog"
)

const (
	testCommit  = "0123456789abcdef0123456789abcdef01234567"
	testCurrent = "v0.0.0-20260806053623-2ec6f862073f"
)

func TestCreatePlanProducesDeterministicProductionAndRehearsalContracts(t *testing.T) {
	t.Parallel()
	root, value := releaseCatalog(t)
	for _, rehearsal := range []bool{false, true} {
		runner := newReleaseRunner()
		plan, err := CreatePlan(t.Context(), value, Options{
			Root: root, Repository: "starter-mail", Version: "v1.2.3",
			Rehearsal: rehearsal, SourceDateEpoch: 1_700_000_000,
		}, runner)
		if err != nil {
			t.Fatal(err)
		}
		mode := "production"
		artifacts := []string{
			"checksums.txt", "checksums.txt.pem", "checksums.txt.sig",
			"starter-mail_1.2.3_sbom.spdx.json", "starter-mail_1.2.3_source.tar.gz",
		}
		if rehearsal {
			mode = "rehearsal"
			artifacts = []string{
				"checksums.txt", "starter-mail_1.2.3_sbom.spdx.json",
				"starter-mail_1.2.3_source.tar.gz",
			}
		}
		if plan.Schema != PlanSchema || plan.Repository != "starter-mail" ||
			plan.Module != "github.com/spice-framework/starter-mail" ||
			plan.Source != "https://github.com/spice-framework/starter-mail" ||
			plan.Mode != mode || plan.Commit != testCommit ||
			plan.SourceDateEpoch != 1_700_000_000 ||
			plan.CompatibilityMinimum != "v0.1.0" ||
			plan.CompatibilityCurrent != testCurrent ||
			!slices.Equal(plan.Artifacts, artifacts) || !slices.IsSorted(plan.RequiredFiles) {
			t.Fatalf("CreatePlan() = %#v", plan)
		}
		productionCommand := runner.called("git", "status", "--porcelain", "--untracked-files=all")
		if productionCommand == rehearsal {
			t.Fatalf("production validation called = %t, rehearsal = %t", productionCommand, rehearsal)
		}
	}
}

func TestCreatePlanRejectsUntrustedReleaseState(t *testing.T) {
	t.Parallel()
	for name, configure := range map[string]func(*Options, *releaseRunner, *catalog.Catalog){
		"invalid version": func(options *Options, _ *releaseRunner, _ *catalog.Catalog) {
			options.Version = "1.2.3"
		},
		"epoch mismatch": func(options *Options, _ *releaseRunner, _ *catalog.Catalog) {
			options.SourceDateEpoch++
		},
		"module mismatch": func(_ *Options, runner *releaseRunner, _ *catalog.Catalog) {
			runner.module = "example.com/wrong"
		},
		"origin mismatch": func(_ *Options, runner *releaseRunner, _ *catalog.Catalog) {
			runner.origin = "https://github.com/example/fork.git"
		},
		"dirty production checkout": func(_ *Options, runner *releaseRunner, _ *catalog.Catalog) {
			runner.status = "?? untracked"
		},
		"tag mismatch": func(_ *Options, runner *releaseRunner, _ *catalog.Catalog) {
			runner.tagCommit = strings.Repeat("f", 40)
		},
		"missing committed file": func(_ *Options, runner *releaseRunner, _ *catalog.Catalog) {
			runner.missingFile = "LICENSE"
		},
		"modified policy file": func(_ *Options, runner *releaseRunner, _ *catalog.Catalog) {
			runner.policyDiff = "diff --git a/go.mod b/go.mod"
		},
		"non-library repository": func(options *Options, _ *releaseRunner, _ *catalog.Catalog) {
			options.Repository = "spice"
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, value := releaseCatalog(t)
			options := Options{
				Root: root, Repository: "starter-mail", Version: "v1.2.3",
				SourceDateEpoch: 1_700_000_000,
			}
			runner := newReleaseRunner()
			configure(&options, runner, &value)
			if _, err := CreatePlan(t.Context(), value, options, runner); err == nil {
				t.Fatal("CreatePlan() error = nil")
			}
		})
	}
}

func TestCreatePlanRejectsNilBoundaries(t *testing.T) {
	t.Parallel()
	root, value := releaseCatalog(t)
	options := Options{Root: root, Repository: "starter-mail", Version: "v1.2.3"}
	if _, err := CreatePlan(nil, value, options, newReleaseRunner()); err == nil { //nolint:staticcheck // Boundary test.
		t.Fatal("CreatePlan(nil context) error = nil")
	}
	if _, err := CreatePlan(t.Context(), value, options, nil); err == nil {
		t.Fatal("CreatePlan(nil runner) error = nil")
	}
}

func TestSameGitRemoteAcceptsStandardTransportsAndRejectsUnsafeURLs(t *testing.T) {
	t.Parallel()
	expected := "https://github.com/spice-framework/starter-mail.git"
	for _, candidate := range []string{
		"git@github.com:spice-framework/starter-mail.git",
		"ssh://git@github.com/spice-framework/starter-mail.git",
	} {
		match, err := sameGitRemote(candidate, expected)
		if err != nil || !match {
			t.Fatalf("sameGitRemote(%q) = %t, %v", candidate, match, err)
		}
	}
	for _, candidate := range []string{
		"file:///tmp/starter-mail",
		"http://github.com/spice-framework/starter-mail.git",
		"git://github.com/spice-framework/starter-mail.git",
		"ssh://someone@github.com/spice-framework/starter-mail.git",
		"https://user:do-not-log@github.com/spice-framework/starter-mail.git",
		"https://github.com/spice-framework/starter-mail.git?ref=other",
		"https://github.com/example/starter-mail.git",
	} {
		match, err := sameGitRemote(candidate, expected)
		if err == nil && match {
			t.Fatalf("sameGitRemote(%q) = true", candidate)
		}
		if strings.Contains(candidate, "do-not-log") && strings.Contains(fmt.Sprint(err), "do-not-log") {
			t.Fatalf("sameGitRemote() leaked credentials: %v", err)
		}
	}
}

type releaseRunner struct {
	calls       [][]string
	module      string
	origin      string
	status      string
	tagCommit   string
	missingFile string
	policyDiff  string
}

func newReleaseRunner() *releaseRunner {
	return &releaseRunner{
		module: "github.com/spice-framework/starter-mail",
		origin: "git@github.com:spice-framework/starter-mail.git", tagCommit: testCommit,
	}
}

func (runner *releaseRunner) Run(
	ctx context.Context,
	_ string,
	arguments ...string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	runner.calls = append(runner.calls, slices.Clone(arguments))
	switch {
	case slices.Equal(arguments, []string{"git", "remote", "get-url", "origin"}):
		return runner.origin, nil
	case slices.Equal(arguments, []string{"go", "mod", "edit", "-json"}):
		return `{"Module":{"Path":"` + runner.module + `"},"Require":[{"Path":"github.com/spice-framework/spice","Version":"v0.1.0"}]}`, nil
	case slices.Equal(arguments, []string{"git", "rev-parse", "--verify", "HEAD^{commit}"}):
		return testCommit, nil
	case len(arguments) == 5 && slices.Equal(arguments[:4], []string{"git", "show", "-s", "--format=%ct"}):
		return "1700000000", nil
	case len(arguments) == 4 && slices.Equal(arguments[:3], []string{"git", "cat-file", "-e"}):
		_, file, _ := strings.Cut(arguments[3], ":")
		if file == runner.missingFile {
			return "", errors.New("missing")
		}
		return "", nil
	case len(arguments) == 8 && slices.Equal(arguments[:4], []string{"git", "diff", "--no-ext-diff", "--unified=0"}):
		return runner.policyDiff, nil
	case slices.Equal(arguments, []string{"git", "status", "--porcelain", "--untracked-files=all"}):
		return runner.status, nil
	case slices.Equal(arguments, []string{
		"git", "rev-parse", "--verify", "refs/tags/v1.2.3^{commit}",
	}):
		return runner.tagCommit, nil
	default:
		return "", errors.New("unexpected command: " + strings.Join(arguments, " "))
	}
}

func (runner *releaseRunner) called(arguments ...string) bool {
	return slices.ContainsFunc(runner.calls, func(call []string) bool {
		return slices.Equal(call, arguments)
	})
}

func releaseCatalog(t *testing.T) (string, catalog.Catalog) {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"spice", "starter-mail"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(
		filepath.Join(root, "starter-mail", "spice-compatibility.json"),
		[]byte(`{"schema":1,"minimum":"v0.1.0","current":"`+testCurrent+`"}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	policy := catalog.StarterCompatibilityPolicy{
		RepositoryPrefix: "starter-", MetadataFile: "spice-compatibility.json",
		MetadataSchema: 1, CoreModule: "github.com/spice-framework/spice",
		CurrentCore: testCurrent,
	}
	value := catalog.Catalog{
		Schema: catalog.CurrentSchema,
		Toolchains: catalog.Toolchains{
			Go: "1.26.5", Java: "25", GoLand: "2026.2.0.1",
		},
		StarterCompatibility: policy,
		Repositories: []catalog.Repository{
			{
				Name: "spice", Directory: "spice", Status: "active",
				CanonicalURL: "https://github.com/spice-framework/spice",
				CloneURL:     "https://github.com/spice-framework/spice.git",
				Artifact:     "go-module", Module: "github.com/spice-framework/spice",
				Dependencies: []string{}, Fast: []catalog.Invocation{}, Full: []catalog.Invocation{},
			},
			{
				Name: "starter-mail", Directory: "starter-mail", Status: "active",
				CanonicalURL: "https://github.com/spice-framework/starter-mail",
				CloneURL:     "https://github.com/spice-framework/starter-mail.git",
				Artifact:     "go-module", Module: "github.com/spice-framework/starter-mail",
				Dependencies: []string{"spice"}, Fast: []catalog.Invocation{}, Full: []catalog.Invocation{},
			},
		},
	}
	return filepath.Join(root, "starter-mail"), value
}
