package catalog

import (
	"slices"
	"strings"
	"testing"
)

func TestDefaultCatalogUsesCanonicalSpiceRepository(t *testing.T) {
	t.Parallel()
	value, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if value.Toolchains.Go != "1.26.5" ||
		value.Toolchains.GoLand != "2026.2.0.1" || len(value.Active()) != 25 {
		t.Fatalf("Default() = %#v", value)
	}
	if value.StarterCompatibility != testStarterCompatibilityPolicy() {
		t.Fatalf("starter compatibility policy = %#v", value.StarterCompatibility)
	}
	spice := requireRepository(t, value.Repositories, "spice")
	if spice.Status != "active" ||
		spice.CanonicalURL != "https://github.com/spice-framework/spice" ||
		spice.CloneURL != "https://github.com/spice-framework/spice.git" ||
		spice.Module != "github.com/spice-framework/spice" ||
		spice.CanonicalModule != "" {
		t.Fatalf("Spice repository identity = %#v", spice)
	}
	toolchain := requireRepository(t, value.Repositories, "toolchain")
	if toolchain.Status != "active" ||
		toolchain.CanonicalURL != "https://github.com/spice-framework/toolchain" ||
		toolchain.CloneURL != "https://github.com/spice-framework/toolchain.git" ||
		toolchain.Module != "github.com/spice-framework/toolchain" ||
		!slices.Equal(toolchain.Dependencies, []string{".github", "development", "spice"}) ||
		len(toolchain.Fast) != 1 || len(toolchain.Full) != 1 ||
		!slices.Contains(toolchain.Fast[0].Arguments, "./internal/boundarygate/cmd") ||
		!slices.Contains(toolchain.Full[0].Arguments, "./internal/boundarygate/cmd") {
		t.Fatalf("Toolchain repository identity = %#v", toolchain)
	}
	agentDependencies := []string{".github", "development", "spice", "toolchain"}
	for _, name := range []string{"spice-agent"} {
		repository := requireRepository(t, value.Repositories, name)
		if repository.Status != "active" ||
			repository.CanonicalURL != "https://github.com/spice-framework/"+name ||
			repository.CloneURL != "https://github.com/spice-framework/"+name+".git" ||
			repository.Module != "github.com/spice-framework/"+name ||
			!slices.Equal(repository.Dependencies, agentDependencies) ||
			len(repository.Fast) != 1 || len(repository.Full) != 1 ||
			!slices.Contains(repository.Fast[0].Arguments, "-mode=fast") ||
			repository.Release == nil || repository.Release.Profile != ReleaseProfileGoModule {
			t.Fatalf("%s repository identity = %#v", name, repository)
		}
	}
	for _, name := range []string{
		"spice-agent-provider-openai",
		"spice-agent-tools-coding",
		"spice-agent-tui",
	} {
		repository := requireRepository(t, value.Repositories, name)
		if repository.Status != "active" ||
			repository.CanonicalURL != "https://github.com/spice-framework/"+name ||
			repository.CloneURL != "https://github.com/spice-framework/"+name+".git" ||
			repository.Module != "github.com/spice-framework/"+name ||
			!slices.Equal(
				repository.Dependencies,
				append(slices.Clone(agentDependencies), "spice-agent"),
			) || len(repository.Fast) != 1 || len(repository.Full) != 1 ||
			!slices.Contains(repository.Fast[0].Arguments, "-mode=fast") {
			t.Fatalf("%s repository identity = %#v", name, repository)
		}
	}
	agentCoding := requireRepository(t, value.Repositories, "spice-agent-coding")
	if agentCoding.Status != "active" ||
		agentCoding.Module != "github.com/spice-framework/spice-agent-coding" ||
		!slices.Equal(agentCoding.Dependencies, []string{
			".github", "development", "spice", "toolchain", "spice-agent",
			"spice-agent-provider-openai", "spice-agent-tools-coding", "spice-agent-tui",
		}) || len(agentCoding.Fast) != 1 || len(agentCoding.Full) != 1 ||
		!slices.Contains(agentCoding.Fast[0].Arguments, "-mode=fast") ||
		agentCoding.Release == nil || agentCoding.Release.Profile != ReleaseProfileDistribution ||
		len(agentCoding.Release.Binaries) != 2 || len(agentCoding.Release.Targets) != 6 {
		t.Fatalf("Spice Agent coding repository identity = %#v", agentCoding)
	}
	starterSMTP := requireRepository(t, value.Repositories, "starter-smtp")
	if starterSMTP.Status != "active" ||
		starterSMTP.CanonicalURL != "https://github.com/spice-framework/starter-smtp" ||
		starterSMTP.CloneURL != "https://github.com/spice-framework/starter-smtp.git" ||
		starterSMTP.Module != "github.com/spice-framework/starter-smtp" ||
		!slices.Equal(starterSMTP.Dependencies, []string{".github", "development", "spice"}) ||
		len(starterSMTP.Fast) != 1 || len(starterSMTP.Full) != 1 {
		t.Fatalf("SMTP starter repository identity = %#v", starterSMTP)
	}
	starterPostgres := requireRepository(t, value.Repositories, "starter-postgres")
	if starterPostgres.Status != "active" ||
		starterPostgres.CanonicalURL != "https://github.com/spice-framework/starter-postgres" ||
		starterPostgres.CloneURL != "https://github.com/spice-framework/starter-postgres.git" ||
		starterPostgres.Module != "github.com/spice-framework/starter-postgres" ||
		!slices.Equal(starterPostgres.Dependencies, []string{".github", "development", "spice"}) ||
		len(starterPostgres.Fast) != 1 || len(starterPostgres.Full) != 1 {
		t.Fatalf("PostgreSQL starter repository identity = %#v", starterPostgres)
	}
	starterMySQL := requireRepository(t, value.Repositories, "starter-mysql")
	if starterMySQL.Status != "active" ||
		starterMySQL.CanonicalURL != "https://github.com/spice-framework/starter-mysql" ||
		starterMySQL.CloneURL != "https://github.com/spice-framework/starter-mysql.git" ||
		starterMySQL.Module != "github.com/spice-framework/starter-mysql" ||
		!slices.Equal(starterMySQL.Dependencies, []string{".github", "development", "spice"}) ||
		len(starterMySQL.Fast) != 1 || len(starterMySQL.Full) != 1 {
		t.Fatalf("MySQL starter repository identity = %#v", starterMySQL)
	}
	starterRedis := requireRepository(t, value.Repositories, "starter-redis")
	if starterRedis.Status != "active" ||
		starterRedis.CanonicalURL != "https://github.com/spice-framework/starter-redis" ||
		starterRedis.CloneURL != "https://github.com/spice-framework/starter-redis.git" ||
		starterRedis.Module != "github.com/spice-framework/starter-redis" ||
		!slices.Equal(starterRedis.Dependencies, []string{".github", "development", "spice"}) ||
		len(starterRedis.Fast) != 1 || len(starterRedis.Full) != 1 {
		t.Fatalf("Redis starter repository identity = %#v", starterRedis)
	}
	for _, name := range []string{
		"starter-otel",
		"starter-oauth2client",
		"starter-oidc",
		"starter-websocket",
		"starter-grpc",
		"starter-kafka",
	} {
		starter := requireRepository(t, value.Repositories, name)
		if starter.Status != "active" ||
			starter.CanonicalURL != "https://github.com/spice-framework/"+name ||
			starter.CloneURL != "https://github.com/spice-framework/"+name+".git" ||
			starter.Module != "github.com/spice-framework/"+name ||
			!slices.Equal(starter.Dependencies, []string{".github", "development", "spice"}) ||
			len(starter.Fast) != 1 || len(starter.Full) != 1 {
			t.Fatalf("%s repository identity = %#v", name, starter)
		}
	}
	zed := requireRepository(t, value.Repositories, "zed")
	if zed.Status != "active" ||
		zed.CanonicalURL != "https://github.com/spice-framework/zed" ||
		zed.CloneURL != "https://github.com/spice-framework/zed.git" ||
		zed.Artifact != "zed-extension" || len(zed.Full) != 5 ||
		zed.Full[4].Directory != "fixture" ||
		!slices.Equal(
			zed.Dependencies,
			[]string{".github", "development", "spice", "toolchain"},
		) || !slices.Contains(
		zed.Full[len(zed.Full)-1].Arguments,
		"github.com/spice-framework/toolchain/cmd/spice",
	) {
		t.Fatalf("Zed repository identity = %#v", zed)
	}
	chrome := requireRepository(t, value.Repositories, "chrome")
	if chrome.Status != "active" ||
		chrome.CanonicalURL != "https://github.com/spice-framework/chrome" ||
		chrome.CloneURL != "https://github.com/spice-framework/chrome.git" ||
		chrome.Artifact != "chrome-extension" ||
		!slices.Equal(chrome.Dependencies, []string{
			".github", "development", "spice", "toolchain",
		}) || len(chrome.Fast) != 1 || len(chrome.Full) != 1 ||
		!slices.Equal(chrome.Full[0].Arguments, []string{"npm", "run", "verify"}) {
		t.Fatalf("Chrome repository identity = %#v", chrome)
	}
	docs := requireRepository(t, value.Repositories, "docs")
	if docs.Status != "active" ||
		docs.CanonicalURL != "https://github.com/spice-framework/docs" ||
		docs.CloneURL != "https://github.com/spice-framework/docs.git" ||
		docs.Artifact != "documentation-site" ||
		!slices.Contains(docs.Dependencies, "chrome") ||
		!slices.Contains(docs.Dependencies, "spice-agent-coding") ||
		len(docs.Fast) != 1 || len(docs.Full) != 1 ||
		!slices.Equal(docs.Full[0].Arguments, []string{"pnpm", "run", "verify"}) {
		t.Fatalf("Docs repository identity = %#v", docs)
	}
	petclinic := requireRepository(t, value.Repositories, "petclinic")
	if petclinic.Status != "active" ||
		petclinic.CanonicalURL != "https://github.com/spice-framework/petclinic" ||
		petclinic.CloneURL != "https://github.com/spice-framework/petclinic.git" ||
		petclinic.Module != "github.com/spice-framework/petclinic" ||
		!slices.Equal(
			petclinic.Dependencies,
			[]string{".github", "development", "spice", "toolchain", "starter-mysql", "starter-postgres"},
		) ||
		len(petclinic.Fast) != 1 || len(petclinic.Full) != 1 {
		t.Fatalf("Petclinic repository identity = %#v", petclinic)
	}
	commerce := requireRepository(t, value.Repositories, "commerce")
	if commerce.Status != "active" ||
		commerce.CanonicalURL != "https://github.com/spice-framework/commerce" ||
		commerce.CloneURL != "https://github.com/spice-framework/commerce.git" ||
		commerce.Module != "github.com/spice-framework/commerce" ||
		!slices.Equal(
			commerce.Dependencies,
			[]string{".github", "development", "spice", "toolchain", "starter-postgres", "starter-smtp"},
		) ||
		len(commerce.Fast) != 1 || len(commerce.Full) != 1 {
		t.Fatalf("Commerce repository identity = %#v", commerce)
	}
	goland := requireRepository(t, value.Repositories, "goland")
	if goland.Status != "active" ||
		goland.CanonicalURL != "https://github.com/spice-framework/goland" ||
		goland.CloneURL != "https://github.com/spice-framework/goland.git" ||
		goland.Artifact != "goland-plugin" ||
		!slices.Equal(
			goland.Dependencies,
			[]string{".github", "development", "spice", "toolchain", "petclinic"},
		) || len(goland.Fast) != 1 || len(goland.Full) != 1 ||
		goland.Fast[0].Arguments[0] != "java" ||
		goland.Full[0].Arguments[len(goland.Full[0].Arguments)-1] != "verifyRepository" {
		t.Fatalf("GoLand repository identity = %#v", goland)
	}
}

func TestParseRejectsMalformedCatalogs(t *testing.T) {
	t.Parallel()
	base := `{
  "schema": 4,
  "toolchains": {"go":"1.26.5","java":"25","goland":"2026.2.0.1"},
  "starter_compatibility": {"repository_prefix":"starter-","metadata_file":"spice-compatibility.json","metadata_schema":1,"core_module":"github.com/spice-framework/spice","current_core":"v0.0.0-20260806053623-2ec6f862073f"},
  "repositories": [%s]
}`
	repository := `{
  "name":"core","directory":"core","status":"active",
  "canonical_url":"https://github.com/spice-framework/core",
  "clone_url":"https://github.com/spice-framework/core.git",
  "artifact":"go-module","module":"github.com/spice-framework/core",
  "dependencies":[],"fast":[],"full":[]
}`
	tests := map[string]string{
		"unknown field":  strings.Replace(repository, `"full":[]`, `"full":[],"mystery":true`, 1),
		"unsafe path":    strings.Replace(repository, `"directory":"core"`, `"directory":"../core"`, 1),
		"insecure URL":   strings.Replace(repository, "https://github.com", "http://github.com", 1),
		"unknown status": strings.Replace(repository, `"status":"active"`, `"status":"unknown"`, 1),
		"missing module": strings.Replace(repository, `"module":"github.com/spice-framework/core",`, "", 1),
		"unsafe invocation directory": strings.Replace(
			repository,
			`"fast":[]`,
			`"fast":[{"name":"test","directory":"../escape","arguments":["go","test"]}]`,
			1,
		),
	}
	for name, entry := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(fmtCatalog(base, entry))); err == nil {
				t.Fatal("Parse() error = nil")
			}
		})
	}
}

func TestParseRejectsMissingDependenciesAndCycles(t *testing.T) {
	t.Parallel()
	content := `{
  "schema":4,
  "toolchains":{"go":"1.26.5","java":"25","goland":"2026.2.0.1"},
  "starter_compatibility":{"repository_prefix":"starter-","metadata_file":"spice-compatibility.json","metadata_schema":1,"core_module":"github.com/spice-framework/spice","current_core":"v0.0.0-20260806053623-2ec6f862073f"},
  "repositories":[
    {"name":"a","directory":"a","status":"active","canonical_url":"https://github.com/spice-framework/a","clone_url":"https://github.com/spice-framework/a.git","artifact":"docs","dependencies":["b"],"fast":[],"full":[]},
    {"name":"b","directory":"b","status":"active","canonical_url":"https://github.com/spice-framework/b","clone_url":"https://github.com/spice-framework/b.git","artifact":"docs","dependencies":["a"],"fast":[],"full":[]}
  ]
}`
	if _, err := Parse([]byte(content)); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("Parse(cycle) error = %v", err)
	}
	missing := strings.Replace(content, `"dependencies":["a"]`, `"dependencies":["missing"]`, 1)
	if _, err := Parse([]byte(missing)); err == nil || !strings.Contains(err.Error(), "unknown repository") {
		t.Fatalf("Parse(missing) error = %v", err)
	}
}

func TestValidateRejectsMalformedGenericReleasePolicies(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*Repository){
		"starter bypass": func(repository *Repository) {
			repository.Name = "starter-agent"
		},
		"unknown profile": func(repository *Repository) {
			repository.Release.Profile = "unknown"
		},
		"unsafe metadata": func(repository *Repository) {
			repository.Release.MetadataFile = "../spice-release.json"
		},
		"duplicate module": func(repository *Repository) {
			repository.Release.RequiredModules = []string{"example.com/module", "example.com/module"}
		},
		"module with payload": func(repository *Repository) {
			repository.Release.PayloadFiles = []string{"README.md"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value, err := Default()
			if err != nil {
				t.Fatal(err)
			}
			repository := requireRepository(t, value.Repositories, "spice-agent")
			mutate(&repository)
			if err = repository.Release.validate(repository); err == nil {
				t.Fatal("release validation error = nil")
			}
		})
	}
}

func TestParseStarterCompatibilityRequiresCentralCurrentVersion(t *testing.T) {
	t.Parallel()
	policy := testStarterCompatibilityPolicy()
	value, err := ParseStarterCompatibility([]byte(`{
  "schema": 1,
  "minimum": "v0.0.0-20260805175412-383c17744300",
  "current": "v0.0.0-20260806053623-2ec6f862073f"
}`), policy)
	if err != nil || value.Minimum != "v0.0.0-20260805175412-383c17744300" ||
		value.Current != policy.CurrentCore {
		t.Fatalf("ParseStarterCompatibility() = %#v, %v", value, err)
	}
	equal, err := ParseStarterCompatibility([]byte(`{
  "schema": 1,
  "minimum": "v0.0.0-20260806053623-2ec6f862073f",
  "current": "v0.0.0-20260806053623-2ec6f862073f"
}`), policy)
	if err != nil || equal.Minimum != equal.Current {
		t.Fatalf("ParseStarterCompatibility(equal boundaries) = %#v, %v", equal, err)
	}
	for name, content := range map[string]string{
		"wrong schema":      `{"schema":2,"minimum":"v0.1.0","current":"v0.2.0"}`,
		"unknown field":     `{"schema":1,"minimum":"v0.1.0","current":"v0.2.0","extra":true}`,
		"malformed minimum": `{"schema":1,"minimum":"v01.0.0","current":"v0.0.0-20260806053623-2ec6f862073f"}`,
		"stale current":     `{"schema":1,"minimum":"v0.1.0","current":"v0.2.0"}`,
		"trailing value":    `{"schema":1,"minimum":"v0.1.0","current":"v0.0.0-20260806053623-2ec6f862073f"} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, parseErr := ParseStarterCompatibility([]byte(content), policy); parseErr == nil {
				t.Fatal("ParseStarterCompatibility() error = nil")
			}
		})
	}
}

func TestValidateRejectsStarterCompatibilityPolicyDrift(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*Catalog){
		"missing policy": func(value *Catalog) {
			value.StarterCompatibility = StarterCompatibilityPolicy{}
		},
		"unsafe metadata path": func(value *Catalog) {
			value.StarterCompatibility.MetadataFile = "../spice-compatibility.json"
		},
		"starter without core dependency": func(value *Catalog) {
			for index := range value.Repositories {
				if value.Repositories[index].Name == "starter-smtp" {
					value.Repositories[index].Dependencies = []string{".github", "development"}
					return
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value, err := Default()
			if err != nil {
				t.Fatal(err)
			}
			mutate(&value)
			if err := value.Validate(); err == nil {
				t.Fatal("Validate() error = nil")
			}
		})
	}
}

func TestParseRejectsTrailingJSONValue(t *testing.T) {
	t.Parallel()
	content := append(slices.Clone(defaultContent), []byte("\n{}\n")...)
	if _, err := Parse(content); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("Parse(trailing) error = %v", err)
	}
}

func fmtCatalog(format, repository string) string {
	return strings.Replace(format, "%s", repository, 1)
}

func requireRepository(t *testing.T, repositories []Repository, name string) Repository {
	t.Helper()
	index := slices.IndexFunc(repositories, func(repository Repository) bool {
		return repository.Name == name
	})
	if index < 0 {
		t.Fatalf("repository %q is absent from %#v", name, repositories)
	}
	return repositories[index]
}

func testStarterCompatibilityPolicy() StarterCompatibilityPolicy {
	return StarterCompatibilityPolicy{
		RepositoryPrefix: "starter-",
		MetadataFile:     "spice-compatibility.json",
		MetadataSchema:   1,
		CoreModule:       "github.com/spice-framework/spice",
		CurrentCore:      "v0.0.0-20260806053623-2ec6f862073f",
	}
}
