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
while output is reported deterministically in catalog order.

The embedded compatibility catalog is human-readable at
[`internal/catalog/compatibility.json`](internal/catalog/compatibility.json).
Repository status and canonical/source locations are distinct during migration,
so the tool never presents a planned move as completed.
