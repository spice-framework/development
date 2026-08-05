package catalog

import (
	"slices"
	"strings"
	"testing"
)

func TestDefaultCatalogIsValidAndMigrationIsExplicit(t *testing.T) {
	t.Parallel()
	value, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if value.Toolchains.Go != "1.26.5" || len(value.Active()) != 3 {
		t.Fatalf("Default() = %#v", value)
	}
	spice := value.Repositories[2]
	if spice.Status != "migrating" || spice.CloneURL == spice.CanonicalURL ||
		spice.Module == spice.CanonicalModule {
		t.Fatalf("Spice migration provenance = %#v", spice)
	}
}

func TestParseRejectsMalformedCatalogs(t *testing.T) {
	t.Parallel()
	base := `{
  "schema": 1,
  "toolchains": {"go":"1.26.5","java":"25","goland":"2026.2"},
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
  "schema":1,
  "toolchains":{"go":"1.26.5","java":"25","goland":"2026.2"},
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
