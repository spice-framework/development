# Central library delivery

Spice libraries need one trusted release and common quality implementation,
not copied programs that slowly acquire different security and reproducibility
behavior.

## Current duplication

The ten active starter repositories each contain an `internal/qualitygate`
program between roughly 460 and 905 lines. They also contain a release command
and private release package. Five use the older release-builder generation and
five use the newer exact-commit generation. Within each generation, Git source
inspection, archive construction, SPDX generation, checksums, Ed25519 signing,
version parsing, CLI validation, and hundreds of test lines are copied. Several
files are byte-identical; most remaining differences are hard-coded repository,
module, temporary-directory, and artifact names.

Repository-specific acceptance is valuable and remains repository-owned.
Copies of formatting, module/vendor checks, vet, lint, security, generic tests,
coverage accounting, offline proof, compatibility orchestration, and release
rendering are not repository-specific.

## Target contract

The trusted path is the pinned
`github.com/spice-framework/development/cmd/spice-dev` Go tool. A library
selects the tool through its own `go.mod` tool directive and vendors it like
other build tools. CI and local development invoke the same command with
`GOWORK=off`; the command never depends on a sibling checkout.

The central path has three separated phases:

1. `library-release plan` performs read-only policy and source-identity
   validation and emits schema-versioned JSON.
2. A renderer consumes that exact plan and committed Git objects to create a
   deterministic source archive, SPDX SBOM, and checksums in a new staging
   directory. It performs no source rediscovery and no network access.
3. Production signing adds the public key and detached checksum signature,
   then atomically renames the staging directory. Publication and tagging stay
   outside the builder and require an explicit release workflow.

The plan contains no absolute paths or wall-clock timestamps. It records the
catalog repository and module, canonical release version, full commit, commit
epoch, minimum/current Spice compatibility, required committed files, mode,
and exact artifact names. Production requires a clean checkout and the named
tag resolving to `HEAD`. Rehearsal is visibly unsigned and may be dirty or
untagged, but it still binds artifacts to committed source identity.

`library verify` will eventually own the common Go quality profile. Catalog
policy will select the profile and retain only explicit repository-specific
acceptance commands. The shared profile will own toolchain enforcement,
formatting, module/vendor freshness, vet, lint, security, shuffled/race tests,
coverage, compatibility boundaries, offline build, and two-build release
reproducibility. External-service and protocol acceptance stays in the library.

## Implemented migration slice

`spice-dev library-release plan` now implements phase 1. It:

- accepts only active or migrating catalog-governed `starter-*` Go modules;
- requires the checkout's HTTPS or standard Git-over-SSH `origin` to resolve
  to the catalog repository;
- validates strict central compatibility metadata and the direct core
  requirement without network access;
- rejects a `go.mod` module identity different from the catalog;
- resolves and validates a full 40- or 64-character Git `HEAD` object ID;
- derives the reproducible epoch from that commit and optionally checks an
  explicit `SOURCE_DATE_EPOCH` value;
- requires `LICENSE`, `README.md`, `go.mod`, `go.sum`,
  `vendor/modules.txt`, and compatibility metadata in the committed tree;
- requires the inspected `go.mod` and compatibility metadata to match that
  commit even for a deliberately dirty rehearsal;
- requires a clean exact-tagged checkout for production plans; and
- emits stable JSON describing the standard source archive, SPDX SBOM,
  checksum, and production signature files.

This slice deliberately creates no files and handles no secret key. It can be
adopted and compared with existing builders before the artifact implementation
becomes authoritative.

## Migration sequence

1. Add golden parity fixtures from one older-generation starter and one
   newer-generation starter. Implement the central renderer against those
   committed trees and require byte-identical repeated builds.
2. Migrate one starter to a pinned `spice-dev` tool directive. Its existing
   builder remains temporarily as a parity oracle; CI compares both outputs.
3. After parity is green on Windows and Linux, make the central builder
   authoritative for that starter and remove its copied release command and
   package.
4. Add the central common quality profile. Migrate the same starter by replacing
   copied generic checks with the shared profile while retaining its explicit
   integration/acceptance commands.
5. Repeat per starter, alternating old and new builder generations. A migration
   is complete only after deterministic artifacts, signature verification,
   SBOM contents, offline execution, and repository-specific acceptance match.
6. Remove the final copied implementations only after every active library uses
   the pinned central tool. Version the plan and artifact schemas independently
   and reject unknown versions rather than adding compatibility heuristics.
