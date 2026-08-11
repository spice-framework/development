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
  "version": "v0.1.0-preview.6"
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

## Spice foundation preview.4 recovery authority

The catalog authorizes the next dependency-free Spice foundation source
release as `github.com/spice-framework/spice v0.1.0-preview.4`. The reviewed
product authority is core commit
`0e79bc4f3b294cd0a429598c4921391f2e4d10e2`, which adds Spice-native
structured logging and contains the canonical schema-two `CODE_STYLE.md` with
SHA-256
`09c014e2d7eb93bf2b395e24e4e6ff2466c05d164d4778a11cf7433164bffb76`.
Release-preparation metadata may advance the eventual tagged commit without
changing those reviewed bytes.

The immutable preview.3 tag object
`62f413c8b24e4d6580cd248f85ea8477400969d6` resolves to candidate commit
`ea7499ddea29cc7a74216ae3569cb26bc9633355`. Its
[release run](https://github.com/spice-framework/spice/actions/runs/31475650316)
passed exact tag and source validation, then failed before artifact rendering
because the candidate did not provide the reusable workflow's required
`make tools-bootstrap` target. Rendering, independent verification,
attestation, provenance authentication, protected publication, and every
deployment were skipped. GitHub contains no preview.3 Release or assets, so
the tag must never be moved or reused and preview.3 is not an authenticated
foundation release that downstream release policies can select.

Recovery advances Spice's own version and the Spice requirement of Toolchain
preview.3 and TUI preview.2 to preview.4. Those are the only three normalized
catalog changes; Toolchain and TUI retain their own release versions, TUI
retains Toolchain preview.3, and every other release and dependency policy is
unchanged. The graphless Spice module policy also remains exact. A future
preview.4 Spice candidate must explicitly satisfy the candidate-owned
bootstrap contract before tagging.

This is pre-tag policy authorization only. It does not create a preview.4
candidate, tag, module proxy version, attestation, approval, or release. The
existing immutable preview.2 release and its history remain unchanged.

```text
go-module-v1	spice	github.com/spice-framework/spice	v0.1.0-preview.4
```

Toolchain preview.3 and TUI preview.2 now require immutable Spice preview.4;
TUI continues to require Toolchain preview.3. Neither downstream candidate may
be tagged before its prerequisites exist. Agent, provider, coding-tools,
Coding distribution, extension-profile, starter, application, and editor
selections continue to use their recorded versions.

## Current Agent foundation authorization

The catalog currently authorizes `spice-agent`, `spice-agent-provider-openai`,
and `spice-agent-tools-coding` to select the immutable
`github.com/spice-framework/spice v0.1.0-preview.2` foundation release. Their
exact historical toolchain selection remains
`v0.1.0-preview.1.0.20260806203056-d0b9ac086bd6`.

The independently authorized TUI preview.2 policy instead selects Spice
preview.4 and Toolchain preview.3. Those are future immutable prerequisites,
not permission to tag TUI before both foundation releases complete.

This authorization is deliberately repository-scoped. `spice-agent` is now
published at `v0.1.0-preview.6` with the public
`VerifiedLauncher` boundary, the removable Phase 7/8 experiment evidence, and
the enforced pre-v1 API/protocol/durable/security compatibility policy. This
own-version advance does not silently move downstream graphs: the provider,
coding-tools, and distribution policies continue to select the published Agent
`v0.1.0-preview.4` exactly.
The provider and coding-tools own release versions remain
`v0.1.0-preview.1`; the TUI own release version is now authorized at
`v0.1.0-preview.2`. The separate `spice-agent-coding` own release version is
authorized at `v0.1.0-preview.4`; its toolchain, Agent preview.4, TUI preview.1
and remaining sibling selections, metadata, binary, payload, target, and
build-identity contracts are unchanged. See
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

Agent `v0.1.0-preview.4` was subsequently published as the architecture-proof
prerelease. The annotated `v0.1.0-preview.5` tag and public Go proxy module now
exist. Its release workflow
[31343998056](https://github.com/spice-framework/spice-agent/actions/runs/31343998056)
completed candidate validation, central rendering, independent verification,
keyless attestation, provenance authentication, and protected publication. The
non-draft GitHub prerelease contains the exact source archive, SPDX SBOM,
release metadata, checksums, and portable Sigstore provenance bundle.

The `v0.1.0-preview.6` catalog entry began as pre-tag policy authorization.
The later annotated tag object
`ee8436262fb755c4bf4897254650cd6d84e2e9fc` resolves to exact candidate commit
`f771caa3b150d87845417c4e26938e2a889441a6`. Release workflow
[31428824060](https://github.com/spice-framework/spice-agent/actions/runs/31428824060)
completed candidate verification, deterministic rendering, independent
verification, keyless attestation, provenance authentication, and protected
publication. The non-draft prerelease contains the exact five-asset module
set. Publication did not alter any downstream module selection.

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
  --version v0.1.0-preview.6 \
  --profile go-module-v1
```

The command validates the exact repository, module, version, and profile
against the embedded catalog and prints exactly:

```text
go-module-v1	spice-agent	github.com/spice-framework/spice-agent	v0.1.0-preview.6
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
