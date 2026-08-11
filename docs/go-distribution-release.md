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
  "version": "v0.1.0-preview.4"
}
```

The catalog exclusively supplies exact `required_modules` path-and-version selections, `binaries`, `targets`,
`payload_files`, and a typed `build_identity` containing exactly one version
string symbol and one commit string symbol from the released module. There is
no arbitrary linker-flags escape hatch and there are no path, binary, or target flags. Unknown
metadata fields, starter repositories, `go-module-v1` repositories, and
uncataloged sources fail before a build.

Before any caller or tag change, the catalog-only policy comparison is:

```text
spice-dev go-release policy-check \
  --repo spice-agent-coding \
  --module github.com/spice-framework/spice-agent-coding \
  --version v0.1.0-preview.4 \
  --profile go-distribution-v1
```

It performs no source, tag, artifact, or network operation and emits exactly:

```text
go-distribution-v1	spice-agent-coding	github.com/spice-framework/spice-agent-coding	v0.1.0-preview.4
```

The independent verifier must authorize the same ordered tuple before tag
creation. Agreement is policy evidence only and publishes nothing.

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

## Toolchain preview.3 pre-tag authority

The catalog now contains a separate repository-keyed Toolchain distribution
policy for a later `github.com/spice-framework/toolchain v0.1.0-preview.3`
candidate. It requires the planned immutable Spice foundation preview.3
selection and authorizes exactly:

- binary `spice` from `./cmd/spice`;
- payloads `LICENSE` and `README.md`;
- darwin, Linux, and Windows on amd64 and arm64; and
- linker identities
  `github.com/spice-framework/toolchain/internal/cli.Version` and
  `github.com/spice-framework/toolchain/internal/cli.Commit`.

The renderer produces six archives, release metadata, an SPDX SBOM, and
checksums: nine attestation subjects. A later organization workflow may add
the authenticated `provenance.sigstore.json` bundle as the tenth published
asset. The policy comparison output is exactly:

```text
go-distribution-v1	toolchain	github.com/spice-framework/toolchain	v0.1.0-preview.3
```

This Development change does not edit or validate a Toolchain candidate, add
its required identity variables, create release metadata, change a caller,
approve an environment, create a tag, or publish assets. The existing
Toolchain preview.1 and preview.2 tags and preview.2 release remain historical
facts. Before an immutable preview.3 tag, an independently committed Toolchain
verifier must authorize the byte-identical tuple and complete policy, and the
candidate must satisfy the ordinary source and installed-byte contracts.
Publication order remains Spice preview.3, Toolchain preview.3, then TUI
preview.2; combined catalog authorization does not collapse those immutable
release boundaries.

Coding remains independently authorized at preview.4 with both binaries, all
seven payloads, the same six targets, and every existing dependency selection.
Nothing in the Toolchain policy changes or substitutes that published
distribution contract.

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

Preview.2 was subsequently published as the architecture-proof distribution.

The immutable `spice-agent-coding v0.1.0-preview.3` tag records a failed
pre-attestation execution attempt, not a published distribution. Its
[release run](https://github.com/spice-framework/spice-agent-coding/actions/runs/31345003119)
completed candidate validation, rendering, and independent verification. The
Linux installed-byte job then rejected the correct preview.3 artifact set
because the candidate's acceptance fixture still expected preview.2 names. The
Windows job separately received a mixed Git Bash and native Windows artifact
path, which the candidate correctly rejected as noncanonical. Both execution
jobs failed, so attestation, provenance authentication, and publication were
skipped; GitHub contains no release or release assets for preview.3. The
immutable tag must never be moved or reused.

Recovery advances only the distribution's own release version to
`v0.1.0-preview.4`. Its installed-archive fixture and policy tuple advance with
that own version. The required module graph, toolchain, siblings, metadata
filename, binaries, targets, payload files, and build-identity symbols remain
unchanged. In particular, the distribution continues to require Agent
`v0.1.0-preview.4`; Agent preview.5 is not selected implicitly. Catalog
authorization alone does not repin the caller, create a tag, or publish a
release.

The corrected candidate was later tagged without rewriting preview.3. Release
run
[31349650978](https://github.com/spice-framework/spice-agent-coding/actions/runs/31349650978)
completed candidate validation, deterministic rendering, independent
verification, installed-byte execution on Linux and Windows, keyless
attestation, provenance authentication, and protected publication. The
non-draft prerelease contains the exact six platform archives, release
metadata, SPDX SBOM, checksums, and portable Sigstore provenance bundle: ten
assets in total.
