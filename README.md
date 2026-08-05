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

The embedded compatibility catalog is human-readable at
[`internal/catalog/compatibility.json`](internal/catalog/compatibility.json).
The core repository is resolved from `github.com/spice-framework/spice`; catalog
tests pin both its Git clone URL and Go module path to prevent a return to the
retired personal namespace. Repository status and canonical/source locations
remain distinct fields so future migrations are never presented as completed
before their own acceptance gates pass. The active SMTP starter is an
independently versioned Go module with its own complete quality gate, vendor
proof, and authenticated verified-STARTTLS Mailpit acceptance path. The active
PostgreSQL starter is independently versioned as well; it owns migrations,
transactions, batch operations, durable outbox behavior, SQL test slices, and
a pinned real-PostgreSQL acceptance path. The independent MySQL starter owns
secure pool configuration, cancellation and recovery behavior, and a pinned
real-MySQL acceptance path without adding its driver to core. The independent
Redis starter owns authenticated client configuration, independent pools,
typed JSON cache operations, expiry, cancellation, and a pinned real-Redis
acceptance path without adding its client graph to core. Go repository linters
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
Schema 2 also records safe per-command working directories, allowing the active
Zed repository to verify both its locked Rust extension and nested canonical
Spice fixture without a shell or a repository-local path assumption.
The active GoLand repository is likewise verified from the shared workspace
against the catalog-selected core and Petclinic checkouts. Its installed-IDE
visual and interaction suite remains a repository-owned Windows and Linux CI
gate because it launches a real pinned GoLand distribution.
