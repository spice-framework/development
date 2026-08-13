# Spice ecosystem development

Unified documentation: [spiceframework.dev/project/ecosystem](https://spiceframework.dev/project/ecosystem/).

This repository owns cross-repository bootstrap, workspace, compatibility, and
verification tooling for the Spice Framework organization. It does not contain
framework runtime or compiler behavior.

## Quick start

With Go 1.26.5 installed:

```text
go run ./cmd/spice-dev catalog
go run ./cmd/spice-dev bootstrap --root ../spice-workspace
go run ./cmd/spice-dev workspace --root ../spice-workspace
go run ./cmd/spice-dev verify --root ../spice-workspace
go run ./cmd/spice-dev snapshot materialize --lock ecosystem.lock.json --root ../snapshot
go run ./cmd/spice-dev snapshot verify --lock ecosystem.lock.json --root ../snapshot --offline
go run ./cmd/spice-dev agent-extension init --directory ../example-agent-tool --module example.com/acme/agent-tool --tool-name acme.inspect --profile compiled-tool-autoconfigure/v1alpha1-preview6
go run ./cmd/spice-dev library-release plan --root ../starter-smtp --repo starter-smtp --version v1.2.3 --rehearsal > plan.json
go run ./cmd/spice-dev library-release render --root ../starter-smtp --plan plan.json --output dist/rehearsal
```

`bootstrap` and `snapshot materialize` are the only network-capable commands.
Add `--offline` to bootstrap to validate already-present checkouts without
fetching. Existing directories must be real
Git checkouts with the exact catalog remote; the command never replaces them or
rewrites their origin.

`workspace` generates an owned `go.work` and `spice.code-workspace`. It is
idempotent and refuses to overwrite files without its marker. Use `--check` in
automation to require freshness without writing.

`agent-extension init` is a deterministic, transactional, network-free source
scaffold for the current pre-stable
`compiled-tool-autoconfigure/v1alpha1-preview6` profile. The immutable preview5
profile remains checkable for existing modules. Initialization does not invoke Go,
Git, Spice, or module resolution and deliberately does not copy a vendor tree
or generated Go. The created repository owns an explicit `make materialize`
step for those network-capable operations. `agent-extension check` is the
read-only, offline structural and pin check for the resulting materialized
tree. This Development utility is not yet an immutable distributed tool, and a
locally generated fixture is not clean-room public-authoring evidence. See
[`docs/agent-extension-authoring.md`](docs/agent-extension-authoring.md).

`verify` runs repository-owned commands. Fast mode is the default; `--full`
runs complete gates. Selected repositories execute in dependency-ordered waves;
independent repositories within a ready wave run concurrently up to `--jobs`,
while output is reported deterministically in catalog order. Verification is
offline and shell-free: Go uses `GOPROXY=off`, Cargo uses offline mode, rustup
automatic toolchain installation is disabled, and Java-based repositories are
invoked through their checked-in Gradle wrapper JAR instead of an
operating-system-specific script. Bootstrap or install the pinned toolchains
and dependency caches explicitly before running a full gate.

`snapshot materialize` is the explicit network-capable boundary for consumers
that assemble exact multi-repository source snapshots. It validates every
repository and canonical remote against the embedded catalog, fetches only the
locked 40-character commit into a new detached checkout, rejects symlinks, and
publishes one deterministic ownership manifest only after the complete snapshot
succeeds. `snapshot verify --offline` performs no fetches: it requires that
manifest, the exact detached commits, canonical remotes, clean worktrees, and a
symlink-free tree. The snapshot command intentionally has no knowledge of
Markdown, Astro, documentation manifests, or another consumer-specific format.

Before a starter's repository-owned gate, `verify` applies the catalog's
central starter compatibility policy. Every active `starter-*` repository must
contain a strict `spice-compatibility.json`: its `current` boundary must equal
the catalog-selected core revision, and its `minimum` boundary must equal the
direct `github.com/spice-framework/spice` requirement reported by its own
`go.mod`. Missing, malformed, indirect, and stale declarations fail before the
starter gate runs. This preflight is read-only, shell-free, and offline; it uses
`go mod edit -json` with the same `GOWORK=off` and `GOPROXY=off` isolation as
the rest of verification.

The catalog separately authorizes generic `go-module-v1` and
`go-distribution-v1` release profiles for Spice core, Toolchain, and the five
Spice Agent repositories. Those entries fix the preview version, strict
`spice-release.json` file name, exact required module path-and-version
selections, and—for each distribution—the repository-specific binary,
six-platform target, payload, and build-identity allowlists. Spice core uses
the narrowly validated
dependency-free form: both root graph files are absent, the catalog selects no
modules, no `require`, `tool`, `replace`, or tracked `vendor/` content is
allowed, and offline read-only Go inspection must report only the main module.
This metadata cannot route a `starter-*` repository around the established
starter release path. Rendering, independent verification, attestation, and
publication remain separate fail-closed commands and workflows; catalog
authorization alone publishes nothing.

The first generic renderer is available through an isolated command boundary:

```text
spice-dev go-release policy-check --repo spice-agent --module github.com/spice-framework/spice-agent --version v0.1.0-preview.7 --profile go-module-v1
spice-dev go-release render --root ../spice-agent --repo spice-agent --version v0.1.0-preview.7 --output ../release
spice-dev go-release verify --root ../spice-agent --repo spice-agent --version v0.1.0-preview.7 --artifacts ../release
```

`policy-check` reads only the embedded catalog and emits one stable tab-separated
profile, repository, module, and version tuple. It does not inspect source,
require a tag or artifacts, or use the network, so the trusted renderer and an
independent verifier can compare authorization before creating an immutable
tag. A match is policy evidence only; it does not validate or publish a release.

The Agent preview.7 policy requires the published Spice preview.4 and Toolchain
preview.2 modules. It is pre-tag authorization only: this Development change
does not create an Agent candidate, tag, attestation, approval, or Release.
Provider, coding-tools, TUI, Coding distribution, extension-profile, and
published Toolchain preview.4 policies retain their recorded versions and
dependencies. The separate Toolchain preview.8 authority below advances only
Toolchain's next distribution identity.

It requires a clean tagged checkout, an exact catalog origin and module,
`go 1.26.0`, `toolchain go1.26.5`, a strict committed
`spice-release.json`, and either a complete committed vendor graph or the
fail-closed dependency-free form described above. Rendering is offline,
deterministic, outside the repository, and refuses to replace an existing
directory. The local verifier byte-compares a fresh rendering against the
artifact allowlist; it is not the independent post-attestation verifier used
to establish release trust. See [`docs/go-module-release.md`](docs/go-module-release.md).

Binary applications use the separate catalog-closed distribution boundary:

```text
spice-dev go-release policy-check --repo spice-agent-coding --module github.com/spice-framework/spice-agent-coding --version v0.1.0-preview.4 --profile go-distribution-v1
spice-dev distribution-release render --root ../spice-agent-coding --repo spice-agent-coding --version v0.1.0-preview.4 --output ../distribution
spice-dev distribution-release verify --root ../spice-agent-coding --repo spice-agent-coding --version v0.1.0-preview.4 --artifacts ../distribution
```

The same renderer now has a distinct pre-tag Toolchain preview.8
policy:

```text
spice-dev go-release policy-check --repo toolchain --module github.com/spice-framework/toolchain --version v0.1.0-preview.8 --profile go-distribution-v1
```

That policy requires the authenticated Spice preview.4 foundation and
selects only `cmd/spice`, `LICENSE`, and `README.md`; it does not change
Coding's two-binary, six-target policy or authorize a Toolchain tag, candidate,
caller, attestation, or publication by itself.

Toolchain preview.7 was published from annotated tag object
`5645e26fe2383713819554dccd1e92cfd03cc0bf`, which resolves to candidate commit
`e83e4ff8639ed6e3aa49c6dd8b2e3ba0d5174e08`. Successful
[release run](https://github.com/spice-framework/toolchain/actions/runs/31655704075)
used attestation deployment `5880057692` and publication deployment
`5880086379` to produce its exact ten-asset prerelease. Public proxy and
checksum-database resolution records module sum
`h1:XgNwiSCrnwh+iDxi3RJX8pbRTTpdL7NDiMedE861U6g=` and go.mod sum
`h1:nezzFkAq9TDdavVL5sYJm2nOKNWAu1p9VTz3XFihgUg=`. Preview.8 is a distinct
identity for the reviewed Toolchain product line through commit
`9568be77a3dcb7ebdf61c5510cc1475e9cffe002`. That bounded delta makes generated
logging scopes use the complete compiler-validated, recursively inventoried
local module identity set on every target while application, provider,
configuration, package, and dependency-edge composition remains host-selected.
This Development authority changes exactly Toolchain's own release version from
preview.7 to preview.8. Its Spice
preview.4 requirement is unchanged; TUI preview.2 remains on published
Toolchain preview.4, and every Agent, provider, coding-tools, Coding, and
extension-profile selection remains unchanged. It does not edit or validate a
Toolchain candidate, repin a caller, create a tag, approve an environment,
attest bytes, or publish assets.

Only a `go-distribution-v1` catalog entry can select that command. The catalog,
not command-line input or filesystem discovery, supplies every binary package,
target, and payload. See
[`docs/go-distribution-release.md`](docs/go-distribution-release.md).

`library-release plan` is the first executable part of the central library
delivery implementation. It performs no writes, downloads, signing, tagging,
or publication. It binds a catalog-governed library to its exact module,
compatibility boundaries, full Git commit, commit epoch, required committed
files, and standard artifact names. Production plans additionally require a
clean checkout and an exact tag at `HEAD`; `--rehearsal` deliberately omits
those two production checks. See
[`docs/library-delivery.md`](docs/library-delivery.md) for the completed
release-builder migration, trust boundaries, and historical parity evidence.
The hosted release wave paused during the 2026-08-06 GitHub Actions outage is
captured in [`docs/release-continuation.md`](docs/release-continuation.md),
including immutable candidate identities, run IDs, audited results, and the
safe resume procedure. Runs that remain queued under the current organization
billing/policy state are an unfinished, nonblocking mirror; local gates do not
claim that those jobs executed or passed.

`library-release render` consumes an already validated rehearsal plan and only
the selected commit's Git objects. It atomically creates a new directory with a
deterministic source archive, SPDX 2.3 SBOM, and SHA-256 checksums. It is
offline, refuses existing output, and rejects production plans.

`library-release sign` is the separate production boundary used by the
organization release workflow. It requires a production plan, exact clean
tagged checkout, output and private-key files outside the repository, a
canonical Ed25519 private key, and an independently trusted matching public
key. It signs the exact checksum bytes, revalidates the checkout immediately
before an atomic no-replace commit, and emits exactly the archive, SBOM,
checksums, canonical public-key PEM, and raw detached signature.

`library-release public-key` is the narrow trust-anchor bootstrap boundary. It
derives one canonical PKIX public-key PEM for review and commit from externally
created Ed25519 key material. The command never generates, prints, or persists
private material and atomically creates, but never replaces, the public output.
It is an administrative operation, not part of normal application development.

All ten active starter repositories now pin the same immutable organization
release workflow. Each owns a distinct committed Ed25519 public anchor and a
corresponding repository Actions secret, and uses separate protected
`release-signing` and `release-publish` approval environments plus restricted,
immutable release tags. Candidate verification is uncredentialed; the central
renderer/signer and independent toolchain verifier are built from separate
immutable commits; only the final approved publication job receives
`contents:write`. The repositories retain deterministic unsigned rehearsals,
but no longer contain copied release commands or `internal/release` packages.
This statement describes completed release infrastructure, not the completion
of every public preview run or post-publication audit.

## Compatibility and repository state

The embedded compatibility catalog is human-readable at
[`internal/catalog/compatibility.json`](internal/catalog/compatibility.json).
The catalog's `starter_compatibility.current_core` value is a deliberately
pinned consumer-policy baseline used by the offline starter preflight. It is
not a claim that the selected pseudo-version is the latest core repository
head. Likewise, application and editor compatibility fixtures pin reviewed
core/toolchain pairs as reproducible test inputs; they advance through an
explicit compatibility change rather than following either repository's
moving `main` branch.

The repository split is now explicit:

- `spice` owns the runtime and public SDK contracts.
- `toolchain` owns the compiler, generator, CLI, LSP, independent release
  verifier, and enforced compiler/generator/dev-loop performance budgets.
- ten independently versioned `starter-*` repositories own external-system
  integration and their real-service acceptance paths;
- `commerce` and `petclinic` own distinct minimum/current core and toolchain
  compatibility matrices, generated-code proof, and application acceptance;
- `goland` owns the packaged installed-IDE visual, navigation, complete-package
  Run, and native Debug proof on Windows and Linux;
- `zed` owns the independently versioned Rust extension and its canonical
  offline Spice fixture; and
- `chrome` owns the privacy-preserving GitHub presentation and reusable
  browser-neutral Spice syntax contract;
- `docs` owns the static, exact-commit documentation portal while product
  documentation remains canonical in each source repository;
- `spice-agent` owns the experimental Spice-native agent SDK, kernel,
  protocols, daemon/client contracts, and runtime-plugin host;
- `spice-agent-provider-openai`, `spice-agent-tools-coding`, and
  `spice-agent-tui` own independently versioned provider, coding-tool, and
  terminal UI extensions, while `spice-agent-coding` assembles their generated
  SDK-first reference distribution; and
- `.github` and this repository own organization workflows and cross-repository
  development policy respectively.

The five-repository dependency graph, exact 2026-08-06 commits and module pins,
dependency-ordered fast-check command, and honest macOS cross-compile boundary
are recorded in
[`docs/spice-agent-phase-0.md`](docs/spice-agent-phase-0.md). That snapshot does
not duplicate or replace the canonical implementation ledger.

For compiled Spice Agent code, the generated Spice provider graph is the
extension graph. The ecosystem does not maintain a second compiled container,
runtime registry, or reflection-based lookup layer. Only negotiated
out-of-process plugins have a dynamic generation, and an active run retains an
immutable lease on the generation with which it started.

Repository status, compatibility baselines, source locations, and public
release status are separate facts. A green compatibility or installed-IDE gate
does not by itself claim that a new public version has been published.
