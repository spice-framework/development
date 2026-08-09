# Generic Go distribution release contract

`go-distribution-v1` builds a catalog-authorized set of Go commands for a
closed catalog target matrix and packages only the catalog payload allowlist.
It is deliberately separate from `go-release` and the established starter
`library-release` path.

## Source intent and selection

The committed `spice-release.json` is the same closed identity shape used by
other generic profiles:

```json
{
  "schema": 1,
  "profile": "go-distribution-v1",
  "repository": "spice-agent-coding",
  "module": "github.com/spice-framework/spice-agent-coding",
  "version": "v0.1.0-preview.2"
}
```

The catalog exclusively supplies exact `required_modules` path-and-version selections, `binaries`, `targets`,
`payload_files`, and a typed `build_identity` containing exactly one version
string symbol and one commit string symbol from the released module. There is
no arbitrary linker-flags escape hatch and there are no path, binary, or target flags. Unknown
metadata fields, starter repositories, `go-module-v1` repositories, and
uncataloged sources fail before a build.

## Rendering

```text
spice-dev distribution-release render \
  --root <clean-tagged-checkout> \
  --repo <catalog-name> \
  --version <catalog-version> \
  --output <new-directory-outside-checkout>
```

Rendering requires:

- an exact current catalog origin without an explicit port, clean `HEAD`, and
  version tag;
- a complete portable tagged tree containing only regular Git blobs, with no
  symlinks, gitlinks, unsafe paths, or case-colliding path components;
- committed canonical metadata and regular-file payloads;
- `go 1.26.0`, `toolchain go1.26.5`, and an executing Go 1.26.5 binary;
- the exact catalog-required module versions in both `go.mod` and committed
  vendor metadata, no `replace` directives, and a
  consistent committed vendor graph;
- portable, case-insensitively unique archive paths.

The renderer materializes the exact tagged commit into a private scratch tree;
module inspection, package listing, and every build use that tree rather than
the mutable checkout. It resolves one absolute regular Go executable, requires
an exact `go version go1.26.5 <host>/<arch>` response, and reuses only that path
for module inspection, builds, and `go tool nm`.

Each command is built separately for every catalog target in a closed
environment. Ambient Go, CGO, compiler, linker, and package-tool variables are
discarded. The renderer sets `GOWORK=off`, `GOPROXY=off`, `GOSUMDB=off`,
`GOTOOLCHAIN=local`, `GOENV=off`, empty module privacy overrides, empty
`GOFLAGS`, `CGO_ENABLED=0`, exact `GOOS`/`GOARCH`, isolated `GOCACHE`,
`GOMODCACHE`, `GOPATH`, and `GOTMPDIR`, `GOAMD64=v1`, or `GOARM64=v8.0` as
appropriate. Builds use `-mod=vendor`, `-trimpath`, `-buildvcs=false`, and an
empty build ID.

The only linker assignments are the catalog-authorized identity symbols: the
version receives the tag without its leading `v`, and the commit receives the
full source commit. Every binary must expose each exact data symbol once under
`go tool nm`. Every host-target binary must also return exactly
`<binary> <version> (<commit>)` from `--version`; byte searching alone is not
accepted as proof.

Unix targets produce deterministic `.tar.gz` archives; Windows targets produce
deterministic `.zip` archives and add `.exe` only to binary names. Every archive
has one target-specific root and contains exactly all catalog binaries plus all
catalog payload files. Regular payload modes are `0644`; binaries are `0755`.
Entries are sorted, timestamped from the source commit, use forward-slash paths,
and contain no symlinks or inferred files.

No archive may exceed 512 MiB. The size is enforced before an archive enters
the artifact map or is written to the output directory.

The output directory is atomically committed without replacement and contains
exactly:

- one target archive per catalog target;
- `<repository>_<version>_release.json` binding source, commit, epoch,
  Go/toolchain, deterministic build policy, targets, payload hashes, and
  artifact hashes;
- `<repository>_<version>_sbom.spdx.json` containing the deterministic SPDX 2.3
  module graph;
- `checksums.txt` covering every target archive, metadata, and SBOM.

JSON and checksum text use canonical LF output. No host paths, current times,
credentials, workspace files, or mutable branch names enter an artifact.

## Local verification

```text
spice-dev distribution-release verify \
  --root <same-exact-clean-checkout> \
  --repo <catalog-name> \
  --version <catalog-version> \
  --artifacts <existing-directory>
```

The local verifier rebuilds every target with the same hostile-environment
scrubbing, isolated caches, immutable tagged source, identity proof, and offline
vendor contract, rerenders all archives and metadata, and
byte-compares the exact top-level allowlist. Missing, extra, non-regular,
oversized, or changed artifacts fail. It rechecks origin, clean source, `HEAD`,
tag, commit, and epoch after the builds so a concurrent source change cannot be
published.

This remains renderer-owned reproducibility evidence. Keyless signatures,
provenance attestations, fresh-download verification, and publication belong to
the organization workflow and independent toolchain verifier.

## Immutable preview.1 recovery

The immutable `spice-agent-coding v0.1.0-preview.1` tag records a failed
pre-artifact workflow attempt, not a published distribution. Its
[release run](https://github.com/spice-framework/spice-agent-coding/actions/runs/31333877865)
stopped in candidate-owned validation because that commit did not expose the
required `make verify-release` target. The central renderer, independent
verifier, attestation, provenance authentication, and publisher never ran, and
GitHub contains no release or release assets for the tag. The tag must never be
moved or reused.

Recovery advances only the distribution's own release version to
`v0.1.0-preview.2`. Its required module graph, toolchain, metadata filename,
binaries, targets, payload files, and build-identity symbols remain unchanged.
