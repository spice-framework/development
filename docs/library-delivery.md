# Central library delivery

Spice libraries need one trusted release and common quality implementation,
not copied programs that slowly acquire different security and reproducibility
behavior.

## Current duplication

The ten active starter repositories each contain an `internal/qualitygate`
program between roughly 460 and 905 lines. They also contain a release command
and private release package. Five use the older release-builder generation and
five use the newer exact-commit generation. Within each generation, Git source
inspection, archive construction, SPDX generation, checksums, Ed25519 signing,
version parsing, CLI validation, and hundreds of test lines are copied. Several
files are byte-identical; most remaining differences are hard-coded repository,
module, temporary-directory, and artifact names.

Repository-specific acceptance is valuable and remains repository-owned.
Copies of formatting, module/vendor checks, vet, lint, security, generic tests,
coverage accounting, offline proof, compatibility orchestration, and release
rendering are not repository-specific.

## Target contract

The trusted path is the pinned
`github.com/spice-framework/development/cmd/spice-dev` Go tool. A library
selects the tool through its own `go.mod` tool directive and vendors it like
other build tools. CI and local development invoke the same command with
`GOWORK=off`; the command never depends on a sibling checkout.

The central path has three separated phases:

1. `library-release plan` performs read-only policy and source-identity
   validation and emits schema-versioned JSON.
2. `library-release render` consumes that exact plan and committed Git objects to create a
   deterministic source archive, SPDX SBOM, and checksums in a new staging
   directory. It performs no source rediscovery and no network access.
3. `library-release sign` consumes a production plan, adds the trusted public
   key and detached checksum signature, revalidates the exact production state,
   and atomically renames the staging directory. Publication and tagging stay
   outside the builder and require an explicit release workflow.

The plan contains no absolute paths or wall-clock timestamps. It records the
catalog repository and module, canonical release version, full commit, commit
epoch, minimum/current Spice compatibility, required committed files, mode,
and exact artifact names. Production requires a clean checkout and the named
tag resolving to `HEAD`. Rehearsal is visibly unsigned and may be dirty or
untagged, but it still binds artifacts to committed source identity.

`library verify` will eventually own the common Go quality profile. Catalog
policy will select the profile and retain only explicit repository-specific
acceptance commands. The shared profile will own toolchain enforcement,
formatting, module/vendor freshness, vet, lint, security, shuffled/race tests,
coverage, compatibility boundaries, offline build, and two-build release
reproducibility. External-service and protocol acceptance stays in the library.

## Implemented migration slices

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
Production plans remain rejected by this rehearsal-only command.

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
trust anchor rather than trust the release directory itself. Publication,
tag creation, hosted key custody, and a separate toolchain verifier remain
outside this slice.

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
corresponding private key as a protected GitHub Environment secret in a
separate administrative step; the repository contains only the public anchor.
Hosted automation should materialize that secret as a temporary owner-only
file outside the checkout and remove it after signing.

The parity fixtures are independent retained-builder oracles, not outputs
reconstructed by the central renderer. The `.txt` overlay harness in
`internal/libraryrelease/testdata/parity` was compiled inside each retained
`internal/release` package and invoked that package's exported `Build` function
against a clean, fixed SHA-1 fixture repository. Each `*-legacy.json` file
records the retained repository URL, builder commit, `internal/release` Git tree
object, Go toolchain/platform, overlay-harness hash, fixture commit, version,
epoch, and actual legacy artifact hashes. Both frozen records were produced with
Go 1.26.5 on `windows/amd64`; the shared harness hash is
`a7b69759421a2efe4b272285a1b92e76d9f5e515f0fc8f95d76b0507d15a18a1`.

The frozen provenance is:

| Generation | Retained builder commit | `internal/release` tree | Fixture commit |
|---|---|---|---|
| `starter-mysql` older working-tree v1 | `9227a40e1c9f4b4bd122fc60c9740002d978c744` | `9f8adfee5d01773efb11875fb6a5a8b9cbdba65d` | `b9b6ea7f8c42cbdc114b26ce998d107cf5f795d5` |
| `starter-oidc` newer exact-commit v2 | `30b138c178e31629f2f75289642ea18306693999` | `c09323590267ba69de6577d55a708c87130cc173` | `85f568b49a5a0071c69b8c59200dd2a073e59c0e` |

The oracle command was `go test -mod=vendor -overlay <overlay.json>
./internal/release -run ^TestGenerateLegacyParityOracle$ -count=1`, with
`SPICE_PARITY_FIXTURE` selecting `older.json` or `newer.json` and
`SPICE_PARITY_ORACLE` selecting the corresponding legacy record, and
`SPICE_PARITY_HARNESS` naming the checked-in overlay source. The overlay
maps a synthetic `internal/release/parity_oracle_test.go` to the checked-in
`.txt` harness, so the retained implementation is exercised without modifying
or copying it. Regeneration is accepted only from the recorded clean builder
commit and package tree.

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

## Migration sequence

1. Migrate one starter to a pinned `spice-dev` tool directive. Its existing
   builder remains temporarily as a parity oracle; CI compares both outputs.
2. After the documented parity contract is green on Windows and Linux, make the central builder
   authoritative for that starter and remove its copied release command and
   package.
3. Add the central common quality profile. Migrate the same starter by replacing
   copied generic checks with the shared profile while retaining its explicit
   integration/acceptance commands.
4. Repeat per starter, alternating old and new builder generations. A migration
   is complete only after deterministic artifacts, signature verification,
   SBOM contents, offline execution, and repository-specific acceptance match.
5. Remove the final copied implementations only after every active library uses
   the pinned central tool. Version the plan and artifact schemas independently
   and reject unknown versions rather than adding compatibility heuristics.
