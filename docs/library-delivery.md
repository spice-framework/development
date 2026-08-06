# Central library delivery

Spice libraries need one trusted release and common quality implementation,
not copied programs that slowly acquire different security and reproducibility
behavior.

## Current state

The release-builder migration is complete for all ten active starter
repositories. None retains a tracked repository-specific release command or
`internal/release` implementation. Each repository instead keeps:

- its own product quality gate and external-system acceptance;
- a vendored, immutable `spice-dev` tool selection for offline unsigned
  release rehearsal;
- a caller workflow pinned to one immutable `.github` reusable-workflow
  commit;
- a distinct committed Ed25519 public trust anchor and corresponding
  repository Actions secret;
- separate protected `release-signing` and `release-publish` approval
  environments; and
- release-tag creation restrictions plus immutable tag update/deletion rules.

The remaining `internal/qualitygate` packages are repository-owned gate
orchestrators, not release artifact builders. Some generic gate mechanics can
still be consolidated separately, but product-specific coverage, offline,
protocol, and real-service acceptance intentionally remain with each starter.
The infrastructure state above does not assert that every public preview run
and independent post-publication audit has completed.

## Authoritative contract

Local rehearsal uses the pinned
`github.com/spice-framework/development/cmd/spice-dev` Go tool selected through
each starter's `go.mod` tool directive and vendor tree. CI invokes it with
`GOWORK=off`, `GOPROXY=off`, and `-mod=vendor`; it never depends on a sibling
checkout and never receives signing material.

Production uses the organization-owned reusable workflow pinned by immutable
commit. That workflow checks out and builds the renderer/signer from the
separate trusted development commit and the independent verifier from a
separate trusted toolchain commit. It never builds release authority from the
candidate repository or its vendor tree.

The artifact implementation has three separated phases:

1. `library-release plan` performs read-only policy and source-identity
   validation and emits schema-versioned JSON.
2. `library-release render` consumes that exact plan and committed Git objects to create a
   deterministic source archive, SPDX SBOM, and checksums in a new staging
   directory. It performs no source rediscovery and no network access.
3. `library-release sign` consumes a production plan, adds the trusted public
   key and detached checksum signature, revalidates the exact production state,
   and atomically renames the staging directory. Publication and tagging stay
   outside the builder and are performed only by the final protected workflow
   job after independent verification.

The plan contains no absolute paths or wall-clock timestamps. It records the
catalog repository and module, canonical release version, full commit, commit
epoch, minimum/current Spice compatibility, required committed files, mode,
and exact artifact names. Production requires a clean checkout and the named
tag resolving to `HEAD`. Rehearsal is visibly unsigned and may be dirty or
untagged, but it still binds artifacts to committed source identity.

The reusable workflow adds two approval and permission boundaries around those
phases. Candidate validation runs without credentials or secrets. Signing runs
with `contents:read` behind `release-signing` and receives only the one named
caller secret. Independent verification receives no private key. Publication
runs behind `release-publish`, contains no signing secret, and is the only job
with `contents:write`. The workflow re-resolves the immutable tag before and
after publication and classifies canonical SemVer prerelease tags as GitHub
prereleases.

## Central implementation

`spice-dev library-release plan` implements phase 1. It:

- accepts only active or migrating catalog-governed `starter-*` Go modules;
- requires the checkout's HTTPS or standard Git-over-SSH `origin` to resolve
  to the catalog repository;
- validates strict central compatibility metadata and the direct core
  requirement without network access;
- rejects a `go.mod` module identity different from the catalog;
- resolves and validates a full 40- or 64-character Git `HEAD` object ID;
- derives the reproducible epoch from that commit and optionally checks an
  explicit `SOURCE_DATE_EPOCH` value;
- requires `LICENSE`, `README.md`, `go.mod`, `go.sum`,
  `vendor/modules.txt`, and compatibility metadata in the committed tree;
- requires the inspected `go.mod` and compatibility metadata to match that
  commit even for a deliberately dirty rehearsal;
- requires a clean exact-tagged checkout for production plans; and
- emits stable JSON describing the standard source archive, SPDX SBOM,
  checksum, and production signature files.

`spice-dev library-release render` implements the unsigned phase-2 boundary for
rehearsals. It strictly loads a bounded schema-1 plan, reads the exact Git tree
and blobs named by the plan, validates committed module/vendor/checksum state,
and creates a stable PAX tar/gzip source archive, SPDX 2.3 dependency document,
and SHA-256 checksum file. It writes through a new staging directory and an
atomic rename, refuses existing output, performs no network access, and never
reads working-tree source bytes. Two independent renders are byte-identical.
Production plans remain rejected by this rehearsal-only command; the reusable
workflow invokes the production signer after constructing its own exact plan.

Renderer/v1 bounds compatibility metadata to 64 KiB, committed `go.sum` to
16 MiB, and the emitted SPDX SBOM to 1 MiB. Its independent verifier enforces
the same versioned limits, so production signing cannot create an artifact
outside the verifier's accepted contract.

Renderer/v1 also defines a portable source-tree name contract. Every byte in a
committed source path and symbolic-link target must be printable ASCII
(`0x20`-`0x7e`) and valid UTF-8. Existing traversal, absolute-path, Windows
reserved-name, forbidden-character, trailing-dot, and trailing-space checks
still apply. Paths that differ only by ASCII case are rejected as a Git-tree
collision, preventing one signed source tree from extracting differently on
case-sensitive and case-insensitive filesystems. Long printable-ASCII paths
remain supported through deterministic PAX headers. This byte contract is part
of `renderer/v1`; accepting Unicode names or changing normalization behavior
requires a new renderer identity and matching independent-verifier support.

`spice-dev library-release sign` implements phase 3 as a distinct production
boundary. It accepts only a production plan and requires the current checkout
to remain clean, at the exact planned commit, with the exact planned tag and
commit epoch. The production output directory and private key must be outside
the source repository. The independently trusted public-key file may be a
reviewed, committed trust anchor in the clean release tree. Private keys are
bounded regular files with final-component symlinks rejected; on Unix they must
grant no group or other permissions. Accepted key encodings are canonical
Ed25519 PKCS#8 PEM and canonical standard-base64 Ed25519 seed or private-key
bytes. The trusted public key must be canonical Ed25519 PKIX PEM and match the
private key before signing.

The command signs the exact LF-terminated `checksums.txt` bytes, verifies its
own raw Ed25519 signature, and emits exactly five deterministic files: the
source archive, SPDX SBOM, `checksums.txt`, `checksums.txt.pem`, and
`checksums.txt.sig`. The checksum file covers only the archive and SBOM, so it
is nonrecursive. All files are written to same-filesystem staging; the complete
production state is checked again after those writes and immediately before an
atomic no-replace directory commit. Existing output is never replaced and a
failure removes staging. The emitted public key records the authenticated key
used for signing, but verifiers must compare it to an independently distributed
trust anchor rather than trust the release directory itself. The reusable
workflow performs that independent toolchain verification before its separately
approved publication job receives the five-artifact set.

### Bootstrapping a production trust anchor

Private-key creation and custody are intentionally outside Spice. A maintainer
must create the Ed25519 private key using an approved external key-management
process, keep its file outside the source checkout, and restrict it to the
owner on Unix. Spice accepts canonical PKCS#8 PEM or canonical standard-base64
seed/private-key bytes; it never generates or prints private material.

Before creating the release tag, derive the reviewable public trust anchor:

```text
spice-dev library-release public-key \
  --signing-key ../release-keys/starter-smtp.key \
  --output security/release/ed25519-public.pem
```

The output parent must already be a real directory. The command writes a
canonical Ed25519 PKIX PEM through same-directory staging and an atomic
no-replace commit. Existing files, directories, and final-component symlinks
are rejected. Review the public key through an independent tool and reviewer,
commit it, and only then create the production tag and plan. Configure the
corresponding private key as the one repository Actions secret accepted by the
reusable workflow; the repository and both protected environments contain no
private-key copy. Hosted automation materializes the secret only as a temporary
owner-only file outside the checkout for the signing job and removes it before
that job exits.

The parity fixtures are frozen historical oracles, not live copies or outputs
reconstructed by the central renderer. Before the copied builders were
retired, the `.txt` overlay harness in
`internal/libraryrelease/testdata/parity` was compiled inside two representative
historical `internal/release` packages and invoked each package's exported
`Build` function against a clean, fixed SHA-1 fixture repository. Each
`*-legacy.json` file records the source repository URL, historical builder
commit, `internal/release` Git tree object, Go toolchain/platform,
overlay-harness hash, fixture commit, version, epoch, and actual legacy artifact
hashes. Both frozen records were produced with Go 1.26.5 on `windows/amd64`;
the shared harness hash is
`a7b69759421a2efe4b272285a1b92e76d9f5e515f0fc8f95d76b0507d15a18a1`.

The frozen provenance is:

| Generation | Historical builder commit | `internal/release` tree | Fixture commit |
|---|---|---|---|
| `starter-mysql` older working-tree v1 | `9227a40e1c9f4b4bd122fc60c9740002d978c744` | `9f8adfee5d01773efb11875fb6a5a8b9cbdba65d` | `b9b6ea7f8c42cbdc114b26ce998d107cf5f795d5` |
| `starter-oidc` newer exact-commit v2 | `30b138c178e31629f2f75289642ea18306693999` | `c09323590267ba69de6577d55a708c87130cc173` | `85f568b49a5a0071c69b8c59200dd2a073e59c0e` |

The historical oracle command was `go test -mod=vendor -overlay <overlay.json>
./internal/release -run ^TestGenerateLegacyParityOracle$ -count=1`, with
`SPICE_PARITY_FIXTURE` selecting `older.json` or `newer.json` and
`SPICE_PARITY_ORACLE` selecting the corresponding legacy record, and
`SPICE_PARITY_HARNESS` naming the checked-in overlay source. The overlay
maps a synthetic `internal/release/parity_oracle_test.go` to the checked-in
`.txt` harness. Reproduction therefore requires checking out the recorded clean
historical commit and package tree; no active starter repository retains that
implementation. Normal development verification validates the committed
provenance and hashes without executing a former starter builder.

`TestRendererGoldenParityContracts` validates that provenance, pins the central
hashes separately, and mechanically compares every central hash with its actual
legacy hash. The intended results are:

| Artifact | Older generation | Newer generation |
|---|---|---|
| Source archive | Not byte-identical: legacy root is `repository-version/`; central uses `repository_version/` | Byte-identical for the equivalent committed tree |
| SPDX SBOM | Not byte-identical: working-tree graph, epoch namespace, no `DESCRIBES`, repository creator/tool | Not byte-identical: central document/tool identity and schema-bound namespace are organization-owned |
| Checksums | Not byte-identical because covered payloads differ | Not byte-identical because the SBOM differs |

The central renderer deliberately adopts the safer exact-commit archive model
and one organization-owned SBOM identity. SBOM namespaces bind the independent
artifact schema version (`v1`), while creator provenance names
`github.com/spice-framework/development/cmd/spice-dev library-release
renderer/v1`; future byte-contract changes must advance that identity. The
renderer does not infer a legacy profile from repository names or silently
claim parity that does not exist.

## Completed migration and remaining boundaries

The completed migration was deliberately incremental: representative old and
new builders first established historical parity contracts; the central
renderer became authoritative; every starter adopted deterministic offline
rehearsal; distinct trust anchors and protected workflow boundaries were
provisioned; and the final copied release command/package was removed.

The following work is separate from that completed release-builder migration:

1. Public preview runs and independent downloaded-artifact audits must be
   recorded per repository; workflow readiness alone is not publication.
2. Generic quality-gate mechanics may move to a central profile only when each
   starter retains its explicit product and real-service acceptance.
3. Plan, renderer, and artifact schemas must continue to version independently
   and reject unknown versions instead of adding heuristic compatibility.
4. Any trusted development, toolchain, or reusable-workflow commit update is a
   security-sensitive migration that requires fresh caller review and pinning.
