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
go run ./cmd/spice-dev library-release plan --root ../starter-smtp --repo starter-smtp --version v1.2.3 --rehearsal > plan.json
go run ./cmd/spice-dev library-release render --root ../starter-smtp --plan plan.json --output dist/rehearsal
go run ./cmd/spice-dev library-release public-key --signing-key ../release-keys/starter-smtp.key --output ../starter-smtp/security/release/ed25519-public.pem
go run ./cmd/spice-dev library-release sign --root ../starter-smtp --plan production-plan.json --output ../releases/starter-smtp-v1.2.3 --signing-key ../release-keys/starter-smtp.key --trusted-public-key ../starter-smtp/security/release/ed25519-public.pem
```

`bootstrap` is the only network-capable command. Add `--offline` to validate
already-present checkouts without fetching. Existing directories must be real
Git checkouts with the exact catalog remote; the command never replaces them or
rewrites their origin.

`workspace` generates an owned `go.work` and `spice.code-workspace`. It is
idempotent and refuses to overwrite files without its marker. Use `--check` in
automation to require freshness without writing.

`verify` runs repository-owned commands. Fast mode is the default; `--full`
runs complete gates. Independent repositories run concurrently up to `--jobs`,
while output is reported deterministically in catalog order. Verification is
offline and shell-free: Go uses `GOPROXY=off`, Cargo uses offline mode, rustup
automatic toolchain installation is disabled, and Java-based repositories are
invoked through their checked-in Gradle wrapper JAR instead of an
operating-system-specific script. Bootstrap or install the pinned toolchains
and dependency caches explicitly before running a full gate.

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
delivery path. It performs no writes, downloads, signing, tagging, or release
publication. It binds a catalog-governed library to its exact module,
compatibility boundaries, full Git commit, commit epoch, required committed
files, and standard artifact names. Production plans additionally require a
clean checkout and an exact tag at `HEAD`; `--rehearsal` deliberately omits
those two production checks. See
[`docs/library-delivery.md`](docs/library-delivery.md) for the artifact-builder
and quality-gate migration sequence.

`library-release render` consumes an already validated rehearsal plan and only
the selected commit's Git objects. It atomically creates a new directory with a
deterministic source archive, SPDX 2.3 SBOM, and SHA-256 checksums. It is
offline, refuses existing output, and rejects production plans.

`library-release sign` is the separate production boundary. It requires a
production plan, exact clean tagged checkout, output and private-key files
outside the repository, a canonical Ed25519 private key, and an independently
trusted matching public key. The public trust anchor may be a reviewed
committed file. It signs the exact checksum bytes, revalidates the checkout
immediately before an atomic no-replace commit, and emits exactly the archive,
SBOM, checksums, canonical public-key PEM, and raw detached signature.

`library-release public-key` is the narrow trust-anchor bootstrap boundary.
The maintainer creates and retains the Ed25519 private key through a separate
external key-management process, then uses this command to derive one canonical
PKIX public-key PEM for review and commit before the release tag is created.
The command never generates, prints, or persists private material and atomically
creates, but never replaces, the public output. Configure any GitHub Environment
signing secret separately after reviewing the committed public anchor; never
put the private key in the repository, command output, or release artifacts.

The embedded compatibility catalog is human-readable at
[`internal/catalog/compatibility.json`](internal/catalog/compatibility.json).
The core repository is resolved from `github.com/spice-framework/spice`, and
the active compiler/CLI/LSP repository is resolved from
`github.com/spice-framework/toolchain`. Catalog tests pin both clone URLs and
Go module paths and require applications/editors to depend explicitly on the
toolchain rather than a retired core `cmd` path. The toolchain compatibility
pair is core `v0.0.0-20260805222830-a2ecd56df246` with standalone toolchain
`v0.0.0-20260805230546-150f8ae62c13`; editor and application fixtures must pin
both exact public versions without a local replacement. Repository status and
canonical/source locations remain distinct fields so migrations are never
presented as completed before their acceptance gates pass. The active SMTP starter is an
independently versioned Go module with its own complete quality gate, vendor
proof, and authenticated verified-STARTTLS Mailpit acceptance path. The active
PostgreSQL starter is independently versioned as well; it owns migrations,
transactions, batch operations, durable outbox behavior, SQL test slices, and
a pinned real-PostgreSQL acceptance path. The independent MySQL starter owns
secure pool configuration, cancellation and recovery behavior, and a pinned
real-MySQL acceptance path without adding its driver to core. The independent
Redis starter owns authenticated client configuration, independent pools,
typed JSON cache operations, expiry, cancellation, and a pinned real-Redis
acceptance path without adding its client graph to core. The independently
versioned OpenTelemetry starter keeps tracing and metrics providers caller
owned, the OAuth2 client starter fails closed around transport and redirect
policy, and the OIDC resource-server starter validates signed tokens against a
deterministic TLS/JWKS acceptance service. Each publishes strict minimum and
current Spice compatibility boundaries without moving its dependency graph
back into core. The WebSocket starter adds authenticated, bounded, verified
TLS client/server behavior with deterministic local interoperability evidence
and no ambient listener, connection, or credential ownership. The gRPC starter
likewise owns verified TLS/mTLS channels, health, message limits, observations,
and graceful/forced server shutdown outside core. The Kafka starter owns
authenticated, ordered, manually committed broker delivery against a pinned
Redpanda acceptance environment. Go repository linters
serialize on
golangci-lint's shared process lock, so concurrently orchestrated repository
gates remain deterministic without oversubscribing the host. The active
Petclinic repository is the
cross-platform reference application and owns its complete generated-code,
security, race, coverage, offline, PostgreSQL, and MySQL verification contract.
The active Commerce repository is the production-shaped modular application;
it independently proves configuration, generated DI, HTTP, security,
transactions, PostgreSQL, mail through the standalone SMTP module, lifecycle,
and zero-network development defaults against immutable dependency versions.
Schema 3 also records the central starter compatibility policy and safe
per-command working directories, allowing the active
Zed repository to verify both its locked Rust extension and nested canonical
Spice fixture without a shell or a repository-local path assumption.
The active GoLand repository is likewise verified from the shared workspace
against the catalog-selected core and Petclinic checkouts. Its installed-IDE
visual and interaction suite remains a repository-owned Windows and Linux CI
gate because it launches a real pinned GoLand distribution.
