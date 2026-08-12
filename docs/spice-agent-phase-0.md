# Spice Agent Phase 0 compatibility evidence

This is a dated compatibility and verification snapshot, not a second
implementation roadmap. Product status and sequencing remain owned by the
[canonical Spice Agent implementation ledger](https://github.com/spice-framework/spice-agent/tree/main/docs/implementation).

The current release catalog preserves
`github.com/spice-framework/spice v0.1.0-preview.2` for provider,
coding-tools, and Coding distribution contracts. Agent preview.7 separately
requires published Spice preview.4 and Toolchain preview.2. TUI preview.2
requires published Spice preview.4 and Toolchain preview.4. The immutable
Toolchain preview.3 tag-only attempt passed validation,
rendering, independent verification, and Linux installed execution, then
[failed Windows installed execution](https://github.com/spice-framework/toolchain/actions/runs/31501018109)
because the candidate correctly rejected a mixed-separator artifact directory
as noncanonical. The failure occurred before attestation or any deployment and
produced no GitHub Release. Toolchain preview.4 was subsequently published by
successful release run
[31522099046](https://github.com/spice-framework/toolchain/actions/runs/31522099046)
with its exact ten-asset prerelease. Toolchain preview.5 was then published
from candidate `3ed984b56faed8972ed9964c672b7fc2d42a5150` by successful release
run [31557699706](https://github.com/spice-framework/toolchain/actions/runs/31557699706)
with the same exact ten-asset contract. Toolchain preview.6 followed from tag
object `8a8fc61aa7e713704135be75690d46017e047e1d` and candidate
`8d1a1ed744d7ed77ed0b013318c8588e69f8177b` in successful release run
[31632016018](https://github.com/spice-framework/toolchain/actions/runs/31632016018).
Its attestation deployment `5876035930` and publication deployment `5876071036`
produced the exact ten-asset prerelease; public module and go.mod sums are
`h1:aChpRT/e2DH7SC+FzL06FPzJxqLQ/jYQDP+xpXWlctI=` and
`h1:nezzFkAq9TDdavVL5sYJm2nOKNWAu1p9VTz3XFihgUg=`. Current preview.7 authority
advances only Toolchain's own next distribution version for the reviewed
product line through `73d2189ee512c4988f1a223aa0b6afdf10bfb260`; TUI remains
pinned to preview.4 and every Agent, provider, coding-tools, and Coding policy
remains unchanged. The immutable Spice preview.3 tag-only attempt failed
candidate bootstrap before rendering, attestation, or deployment and produced
no GitHub Release, so current recovery policy does not treat it as a downstream
foundation release.
After the immutable `spice-agent v0.1.0-preview.1` tag failed before
artifact rendering and the immutable `v0.1.0-preview.2` attempt failed in the
independent verifier before attestation. The immutable `v0.1.0-preview.3`
attempt also completed rendering but failed independent policy verification
before attestation. Agent preview.4 was subsequently published. The annotated
Agent preview.5 tag and public module now exist, while release run
[31343998056](https://github.com/spice-framework/spice-agent/actions/runs/31343998056)
completed keyless attestation, provenance authentication, and protected
publication of the five-asset non-draft prerelease. The independently published
`spice-agent v0.1.0-preview.6` release run
[31428824060](https://github.com/spice-framework/spice-agent/actions/runs/31428824060)
completed the same protected five-asset publication contract. Preview.6 carries
`VerifiedLauncher`, the Phase 7/8 experiment evidence, and the enforced pre-v1
compatibility policy. The current catalog separately authorizes an Agent
preview.7 candidate to require published Spice preview.4 and Toolchain
preview.2; that pre-tag authority does not claim a tag or Release. The
provider, coding-tools, and distribution graphs remain on Agent
`v0.1.0-preview.4`; the provider and coding-tools own release
versions remain `v0.1.0-preview.1`, while the TUI own release version is now
authorized at `v0.1.0-preview.2`. Distribution preview.2 was also published,
and distribution preview.4 was subsequently published by successful release
run `31349650978` after passing the corrected Linux and Windows installed-byte
gates. The catalog continues to authorize only that own release version. Its
immutable preview.1 attempt
[failed in candidate validation](https://github.com/spice-framework/spice-agent-coding/actions/runs/31333877865)
before rendering or artifacts when the candidate lacked `make verify-release`.
Its immutable preview.3 attempt
[failed in installed-archive execution](https://github.com/spice-framework/spice-agent-coding/actions/runs/31345003119)
after candidate validation, rendering, and independent verification: Linux
still expected preview.2 artifact subjects, while Windows passed a mixed-path
artifact directory that the candidate correctly rejected as noncanonical.
Attestation, provenance authentication, and publication were skipped, and no
release was created.
Every distribution toolchain, sibling, metadata, binary, payload, target, and
build-identity selection is unchanged, including Agent preview.4. The catalog
authorizations alone did not create tags, repin callers, approve environments,
or publish releases; the separately protected Agent and distribution workflows
performed those later publications.
The dated source evidence below remains
an accurate record of the earlier commits and is not the current release
policy. Development's tag-independent `go-release policy-check`
provides a deterministic tuple for pre-tag comparison with the independent
Toolchain policy, preventing another immutable tag from being used before both
authorities agree.

## Exact source inputs

The catalog dependency graph and generated workspace include all five Spice
Agent repositories. On 2026-08-06, `origin/main` resolved to:

| Repository | Exact commit |
|---|---|
| `spice-agent` | `1f072842707a5609d811eef3e4858badcc73e7ea` |
| `spice-agent-provider-openai` | `b0d4099d2754f05cc5e8d363e9c853085414e95b` |
| `spice-agent-tools-coding` | `d06a11929ddbc9a9c005eeae69b894c9b2f64b10` |
| `spice-agent-tui` | `7eff972015e69367492fbaf99f8d179d25aadcab` |
| `spice-agent-coding` | `16244f51cecdc66a86ab4721dc276edbc78c47ae` |

The selected module boundaries at those commits are explicit rather than
inferred from moving branches:

- `spice-agent` and `spice-agent-tui` select Spice core
  `v0.1.0-preview.1.0.20260806200749-524424a04df0` and toolchain
  `v0.1.0-preview.1.0.20260806203056-d0b9ac086bd6`.
- `spice-agent-provider-openai` and `spice-agent-tools-coding` still select
  Spice core/toolchain `v0.1.0-preview.1` and Spice Agent
  `v0.0.0-20260806191411-841edd3d47ad`. That older reviewed boundary is a
  recorded compatibility fact, not permission to silently upgrade it.
- `spice-agent-coding` selects the same result-facts core/toolchain pair as
  `spice-agent`, Spice Agent
  `v0.0.0-20260806204214-1f072842707a`, provider
  `v0.0.0-20260806204218-b0d4099d2754`, and coding tools
  `v0.0.0-20260806202006-d06a11929ddb`. Its current module graph does not yet
  select `spice-agent-tui`; catalog ownership and a compiled distribution
  dependency are intentionally reported as separate facts.

## Fast-check ordering

`spice-dev verify` executes the selected dependency graph in topological waves.
Repositories within one ready wave run concurrently up to `--jobs`; a dependent
does not start until all selected dependencies complete successfully. A focused
selection treats unselected ancestors as externally satisfied, so this command
checks the five-repository slice without rerunning the complete framework:

```text
go run ./cmd/spice-dev verify --root .. --jobs 4 --repo spice-agent --repo spice-agent-provider-openai --repo spice-agent-tools-coding --repo spice-agent-tui --repo spice-agent-coding
```

## macOS evidence boundary

The exact commits above are cross-compiled with Go 1.26.5, `CGO_ENABLED=0`,
`GOWORK=off`, `GOPROXY=off`, and the committed vendor graph for both
`darwin/amd64` and `darwin/arm64`. This proves that the selected Go packages
compile for those targets; it is not a macOS execution result.

The 2026-08-06 evidence run used clean detached temporary checkouts at every
commit in the table. The dependency-ordered five-repository fast command passed
(`spice-agent`, then the concurrent provider/tools/TUI wave, then
`spice-agent-coding`), followed by successful
`go build -mod=vendor -trimpath ./...` for every repository on both Darwin
architectures. No product worktree
or module graph was changed by the evidence run.

A real macOS runner remains mandatory for the race detector, process and signal
lifecycle, terminal/UI behavior, filesystem semantics, and executable runtime
acceptance. Those claims must never be inferred from cross-compilation.

## Hosted mirror status

Local repository-owned verification is the delivery gate. Hosted GitHub Actions
runs that remain queued because of the organization billing/policy state are an
unfinished, nonblocking durability mirror: they are neither failures nor green
runs, and this snapshot does not claim they executed. The historical release
queue and its immutable candidates remain recorded in
[`release-continuation.md`](release-continuation.md).
