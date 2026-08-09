# Generic Go module release contract

`go-module-v1` is the catalog-authorized release profile for ordinary Spice Go
modules that are not starters. It does not replace, wrap, or infer the existing
`library-release` path: every `starter-*` repository is rejected before source
inspection.

## Committed intent

The catalog names the metadata file. Version 1 is a closed JSON object:

```json
{
  "schema": 1,
  "profile": "go-module-v1",
  "repository": "spice-agent",
  "module": "github.com/spice-framework/spice-agent",
  "version": "v0.1.0-preview.4"
}
```

Unknown and missing fields fail. The catalog is the sole owner of required
module path-and-version selections; the metadata does not duplicate that list.
Each selection must have that exact canonical version in a `require` entry in
the committed `go.mod` and must match committed vendor metadata. Merely naming
the expected module or selecting another canonical version is insufficient.
The entry may be marked indirect when
Go derives it from a `tool` directive. Releases reject every `replace`
directive, including remote replacements.

## Current Agent foundation authorization

The catalog currently authorizes `spice-agent`, `spice-agent-provider-openai`,
`spice-agent-tools-coding`, and `spice-agent-tui` to select the immutable
`github.com/spice-framework/spice v0.1.0-preview.2` foundation release. Their
exact toolchain selection remains
`v0.1.0-preview.1.0.20260806203056-d0b9ac086bd6`.

This authorization is deliberately repository-scoped. `spice-agent` is
authorized at `v0.1.0-preview.4`; the provider, coding-tools, and distribution
policies select that exact Agent version. The provider, coding-tools, and TUI
release versions remain `v0.1.0-preview.1`. The separate `spice-agent-coding`
distribution release advances to `v0.1.0-preview.2` after its immutable
preview.1 attempt failed before rendering. Its distinct toolchain, remaining
sibling, metadata, binary, payload, and target contracts are unchanged. See
[the distribution release history](go-distribution-release.md#immutable-preview1-recovery).

The immutable `spice-agent v0.1.0-preview.1` tag records a failed pre-artifact
workflow attempt, not an authorized or published Spice release. Its
[release run](https://github.com/spice-framework/spice-agent/actions/runs/31318421427)
stopped in candidate-owned verification before the central renderer,
independent verifier, attestation, provenance authentication, or publisher ran;
GitHub contains no release or release assets for that tag. The tag must never
be moved or reused.

The immutable `spice-agent v0.1.0-preview.2` tag records a separate failed
pre-attestation workflow attempt, not a published release. Its
[release run](https://github.com/spice-framework/spice-agent/actions/runs/31322858420)
completed candidate validation and central rendering, then the independent
verifier job failed while cleaning its isolated module cache. Attestation,
provenance authentication, and publication were skipped, and GitHub contains no
release or release assets for that tag. That tag also must never be moved or
reused. Recovery advances the Agent release version and its downstream
selections to `v0.1.0-preview.3`.

The immutable `spice-agent v0.1.0-preview.3` tag records another failed
pre-attestation attempt, not a published release. Its
[release run](https://github.com/spice-framework/spice-agent/actions/runs/31325588169)
completed candidate validation and central rendering, then the independent
verifier rejected a release-policy mismatch. Attestation, provenance
authentication, and publication were skipped, and GitHub contains no release
or assets for that tag. That tag must never be moved or reused. Recovery
advances the Agent release version and its downstream selections to
`v0.1.0-preview.4`.

The sole graphless exception is a catalog entry with no required modules whose
root module genuinely selects no dependencies. In that mode both root
`go.sum` and `vendor/modules.txt` must be absent. The renderer rejects a
partial pair, every tracked `vendor/` path, and every `require`, `tool`, or
`replace` directive. It then runs offline `go list -mod=readonly` for the
packages and module graph and requires the graph to contain exactly the
unversioned main module. A missing graph file never downgrades a normal
vendored release into this mode.

## Pre-tag policy comparison

```text
spice-dev go-release policy-check \
  --repo spice-agent \
  --module github.com/spice-framework/spice-agent \
  --version v0.1.0-preview.4 \
  --profile go-module-v1
```

The command validates the exact repository, module, version, and profile
against the embedded catalog and prints exactly:

```text
go-module-v1	spice-agent	github.com/spice-framework/spice-agent	v0.1.0-preview.4
```

It does not read a checkout, tag, or artifact and performs no network access.
The trusted renderer and independently versioned Toolchain verifier can run
their corresponding policy checks before tag creation and require the emitted
tuples to be byte-identical. Unknown repositories, starter or distribution
profiles, stale versions, and module/profile drift fail closed. This check
proves policy agreement only; it does not replace candidate verification,
artifact verification, attestation, provenance authentication, or publication.

## Rendering

```text
spice-dev go-release render \
  --root <clean-checkout> \
  --repo <catalog-name> \
  --version <catalog-version> \
  --output <new-directory-outside-checkout>
```

The version tag must resolve to `HEAD`. The origin, module, metadata, Go and
toolchain directives, required files, dependency graph, and portable regular
tracked files are validated without network access. Symlinks and special Git
modes are rejected. The output directory is atomically
committed without replacement and contains exactly:

- `checksums.txt`;
- `<repository>_<version>_release.json`, binding source, commit, epoch,
  toolchain, and artifact digests;
- `<repository>_<version>_sbom.spdx.json`, a deterministic SPDX 2.3 module
  graph;
- `<repository>_<version>_source.tar.gz`, built from committed Git objects with
  a fixed prefix, timestamp, ordering, permissions, and gzip platform marker.

No host paths, current timestamps, credentials, mutable branch names, or
ambient workspace state enter an artifact.

Repository roots and existing output parents are resolved through filesystem
links before containment checks. A path that looks external but traverses a
symlink or junction back into the repository is rejected before staging.

## Local verification and trust boundary

```text
spice-dev go-release verify \
  --root <same-exact-clean-checkout> \
  --repo <catalog-name> \
  --version <catalog-version> \
  --artifacts <existing-directory>
```

Verification rejects extra, missing, non-regular, oversized, or changed files
and byte-compares a new deterministic render from the tagged commit. This is a
renderer-owned reproducibility gate. Release authenticity, keyless signing,
provenance, fresh-download verification, and publication are deliberately
separate organization-workflow and independent-toolchain responsibilities.

`go-distribution-v1` uses the separate profile-specific command documented in
[`go-distribution-release.md`](go-distribution-release.md). Catalog
authorization never selects a different renderer implicitly or publishes an
artifact.
