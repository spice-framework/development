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
  "version": "v0.1.0-preview.1"
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
