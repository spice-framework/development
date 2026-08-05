# Spice ecosystem development contract

## Mission

Provide small, cross-platform, deterministic tooling for bootstrapping,
validating, and verifying the independently versioned Spice repositories.

## Invariants

- Go 1.26.5 is mandatory.
- Catalog entries are explicit and schema-versioned.
- Bootstrap never replaces an existing checkout or rewrites its remote.
- Workspace generation overwrites only files carrying its ownership marker.
- Commands use discrete arguments without a shell.
- Network access occurs only through an explicit non-offline bootstrap command.
- Repository gates remain owned by each repository; this project orchestrates
  them but does not weaken or silently skip them.
- Every commit must pass `make verify` before push to `main`.

Use bounded, reviewable commits. Keep this module standard-library-only unless
a dependency review demonstrates a material need.
