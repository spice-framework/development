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
		value.Toolchains.GoLand != "2026.2.0.1" || len(value.Active()) != 4 {
		t.Fatalf("Default() = %#v", value)
	}
	spice := requireRepository(t, value.Repositories, "spice")
	if spice.Status != "active" ||
		spice.CanonicalURL != "https://github.com/spice-framework/spice" ||
		spice.CloneURL != "https://github.com/spice-framework/spice.git" ||
		spice.Module != "github.com/spice-framework/spice" ||
		spice.CanonicalModule != "" {
		t.Fatalf("Spice repository identity = %#v", spice)
	}
	zed := requireRepository(t, value.Repositories, "zed")
	if zed.Status != "active" ||
		zed.CanonicalURL != "https://github.com/spice-framework/zed" ||
		zed.CloneURL != "https://github.com/spice-framework/zed.git" ||
		zed.Artifact != "zed-extension" || len(zed.Full) != 5 ||
		zed.Full[4].Directory != "fixture" {
		t.Fatalf("Zed repository identity = %#v", zed)
	}
}

func TestParseRejectsMalformedCatalogs(t *testing.T) {
	t.Parallel()
	base := `{
  "schema": 2,
  "toolchains": {"go":"1.26.5","java":"25","goland":"2026.2.0.1"},
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
  "schema":2,
  "toolchains":{"go":"1.26.5","java":"25","goland":"2026.2.0.1"},
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
