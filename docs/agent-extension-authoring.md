# Spice Agent extension authoring architecture proof

## Boundary

`spice-dev agent-extension` is a pre-stable, source-run Development utility. It
does not add an Agent container, registry, reflection lookup, package scan, or
runtime activation mechanism. The generated extension is an ordinary Go module
whose default `tool.Tool` becomes a Spice fallback bean only when an application
explicitly blank-imports its `/autoconfigure` package.

Two immutable profiles exist. `compiled-tool-autoconfigure/v1alpha1-preview5`
retains the original Agent preview5/development-generator graph for existing
modules. `compiled-tool-autoconfigure/v1alpha1-preview6` is current and pins the
released Agent preview6 plus Toolchain preview2 ownership-schema-6 generator.
Neither profile implies provider, stage, UI, persistence, or runtime-plugin
scaffolding. Adding another kind or dependency line requires a new reviewed
catalog profile, tests, migration text, and compatibility evidence.

Development currently has no immutable public tool release. Run this command
from an exact reviewed Development checkout; do not add it as a generated
module dependency or claim a stable public conformance CLI. The generated
repository owns its verification entrypoint and pins released Spice, Agent, and
Toolchain modules.

## Initialize

```text
go run ./cmd/spice-dev agent-extension init \
  --directory ../example-agent-tool \
  --module example.com/acme/agent-tool \
  --tool-name acme.inspect \
  --profile compiled-tool-autoconfigure/v1alpha1-preview6
```

Initialization accepts only a new or empty real directory. It rejects path
escapes, symbolic links/reparse traversal, unsafe module and tool identities,
unknown profiles, and existing content. Files are rendered with stable order,
modes, and LF endings, with no timestamps or absolute paths. A failure or
cancellation rolls back files created in an existing empty directory or removes
the private staging directory before publication. Initialization never invokes
Go, Git, Spice, VCS setup, module resolution, or the network.

The source scaffold includes:

- exact Go 1.26.0 and `toolchain go1.26.5` declarations;
- exact Spice preview2, Agent preview6, Toolchain preview2, Go sums, and
  approved Go tool declarations;
- strict authoring and Spice compatibility manifests;
- typed `starter.Manifest`, explicit `/autoconfigure`, and a deterministic
  read-only/replay-safe example tool;
- source for an external-package generated composition acceptance test;
- architecture, security, dependency, compatibility, deletion, verification,
  and provisional benchmark documentation; and
- a repository-owned standard-library quality command with fast, check,
  verify, benchmark, fuzz, and explicit materialization modes.

It does not bundle the roughly 17 MB dependency vendor tree and does not copy
generated Go under a different module identity. Source-only output is not ready
to publish.

## Materialize and verify

From the generated repository, run:

```text
make materialize
git diff -- vendor .spice internal/spicegen go.mod go.sum
make verify
(cd /path/to/reviewed/development && go run ./cmd/spice-dev agent-extension check --root /path/to/extension)
```

`make materialize` is deliberately explicit and network-capable. It resolves
the exact graph, tidies the module, commits vendor contents, and runs released
Spice generation for `ExtensionProof`. Review every resulting dependency and
generated-code change before committing it. All ordinary repository gates and
`agent-extension check` run with workspace discovery and module network access
disabled.

`check` strictly rejects unknown or trailing manifest fields, stale or unknown
profiles, module/Go/tool/pin/sum drift, replace/exclude/retract/ignore
directives, unapproved Go tools, links, missing review documents, incomplete
vendor selection, and absent generated ownership or source-map structure. It
uses shell-free offline `go mod edit -json` for Go grammar and does not execute
extension initialization, tests, or application code. The repository-owned
`make verify` remains responsible for byte-identical Spice regeneration, vet,
race, fuzz, coverage, and behavioral tests.

## Compatibility and deletion

The authoring schema and profiles are v1alpha1. A checker accepts the exact
recorded supported profile; it never silently moves a preview5 module to
preview6 or any later profile. Changing the schema or dependency graph adds a
new profile and migration instead of permissive fields. Developer source and documentation remain author-owned;
`internal/spicegen` and `.spice` remain generator-owned.

Delete an extension by removing its application blank import and module
dependency, regenerating affected application targets, and deleting its
repository. No kernel state, registry entry, or runtime cleanup remains.

This architecture proof does not by itself satisfy Phase 8's clean-room
public-authoring criterion. That requires three separately versioned modules
created in repository-external directories from immutable released artifacts
and public documentation, with fresh module and build caches, `GOWORK=off`, no
workspace path or replacement directive, and Linux plus Windows vendor-offline
verification. Do not count this source-run template or an existing first-party
fixture as that evidence.
