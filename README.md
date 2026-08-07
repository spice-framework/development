# Spice ecosystem development

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
