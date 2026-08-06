# Release continuation after the GitHub Actions outage

This document is the handoff for the Spice ecosystem release wave paused on
2026-08-06. The locally controlled multi-repository migration, security
hardening, candidate creation, immutable tagging, and local verification are
complete. The only unfinished work is hosted execution and independent audit
of releases that GitHub Actions had not published before the outage.

At the 2026-08-06 13:01 EDT snapshot, the official
[GitHub Actions incident](https://www.githubstatus.com/incidents/qcvjkzcs7j74)
was `investigating` with `critical` impact. Do not repeatedly rerun workflows
while that incident remains active.

## Invariants that must be preserved

- Do not move, delete, recreate, or force-update any candidate tag.
- Do not replace a committed release public key or its corresponding signing
  secret to resume an existing candidate.
- Do not publish artifacts by hand or bypass either protected environment.
- Do not approve `release-signing` until candidate validation and planning are
  green.
- Do not approve `release-publish` until signing and the independent verifier
  are green.
- Treat a queued or waiting run as unfinished, not failed and not released.
- A GitHub release is complete only after a fresh download has passed the
  independent verifier and the tag, assets, and committed public key have been
  audited.

The reusable starter workflow is pinned to organization `.github` commit
`9ae80e32f64b29697acd9ebe629468850b4ae9f2`. Its trusted renderer and signer
come from development commit
`4c308d1b9fda11cb2b045f2e0d9e1616d32d007d`; its independent verifier comes
from toolchain commit `71211498297c9ab77cc05c4844db5e64e0170896`.

## Framework, toolchain, and editor candidates

| Repository | Immutable tag | Candidate commit | Release run at snapshot |
| --- | --- | --- | --- |
| `spice` | `v0.1.0-preview.1` | `170dcb865be90f9679a8a9fc3020bbae79d527de` | [queued, attempt 1](https://github.com/spice-framework/spice/actions/runs/31120480703) |
| `toolchain` | `v0.1.0-preview.1` | `770e243ce28b2cdc01bc0044252ebc8efa6c1373` | [queued, attempt 1](https://github.com/spice-framework/toolchain/actions/runs/31120527225) |
| `goland` | `v0.2.0` | `f14f80b0da1e72293b3e2571401f67c3c04c5e0b` | [queued, attempt 1](https://github.com/spice-framework/goland/actions/runs/31120914981) |
| `zed` | `v0.2.0` | `9e9933ddb969443193ac7f85bc4dd0dbde137263` | [queued, attempt 1](https://github.com/spice-framework/zed/actions/runs/31120917791) |

The annotated tag objects are respectively
`58ccbbd92571c369de8b79fd8e46b2e7c172b608`,
`a5f4283f9040fd22db0dfc66feedb99e0f9e1733`,
`6c0126de70e9c08f4b2b74e31f8cbba8a80fd3df`, and
`ab285c737dff98cea6ab62f359e71a5aeb7e51a8`.

## Starter candidates

| Repository | Immutable tag | Candidate commit | Release run at snapshot |
| --- | --- | --- | --- |
| `starter-postgres` | `v0.1.0-preview.1` | `b39b0d86454f0bb8a559d5d81350b888d319fab7` | [published and green](https://github.com/spice-framework/starter-postgres/actions/runs/31116252187) |
| `starter-oidc` | `v0.1.0-preview.5` | `de9a5a1267e31008dfce832b41fd635c379b8dfc` | [queued, attempt 3](https://github.com/spice-framework/starter-oidc/actions/runs/31116251123) |
| `starter-smtp` | `v0.1.0-preview.1` | `ac44f694bdd139d131c1a11cea89f916c0ddc8b8` | [failed, attempt 4](https://github.com/spice-framework/starter-smtp/actions/runs/31116255782) |
| `starter-mysql` | `v0.1.0-preview.1` | `1f0da249b5e2b06c6525a44d1ade364dae3dcadb` | [queued, attempt 2](https://github.com/spice-framework/starter-mysql/actions/runs/31116249399) |
| `starter-redis` | `v0.1.0-preview.1` | `bc3cd0b83cb32f652c10741b2f9a7702b4a49531` | [queued, attempt 2](https://github.com/spice-framework/starter-redis/actions/runs/31117508330) |
| `starter-otel` | `v0.1.0-preview.1` | `400b0f452cf43316bba471e4a1848c2738848ac4` | [queued, attempt 2](https://github.com/spice-framework/starter-otel/actions/runs/31117509454) |
| `starter-oauth2client` | `v0.1.0-preview.1` | `98144f2c26ad886483a8dba27928e7cf4c178832` | [waiting, attempt 1](https://github.com/spice-framework/starter-oauth2client/actions/runs/31117506399) |
| `starter-websocket` | `v0.1.0-preview.1` | `da547166a28e72749d0180b2e0d601d43948d331` | [queued, attempt 4](https://github.com/spice-framework/starter-websocket/actions/runs/31116254355) |
| `starter-grpc` | `v0.1.0-preview.1` | `c1259129b8ad4e57b0e19375857ea5879ff2e664` | [queued, attempt 3](https://github.com/spice-framework/starter-grpc/actions/runs/31116252990) |
| `starter-kafka` | `v0.1.0-preview.1` | `262476f7feb661a1314dfc95d006de2a8c6fc62f` | [waiting, attempt 4](https://github.com/spice-framework/starter-kafka/actions/runs/31116253846) |

`starter-smtp` failed while GitHub's action download service was unavailable,
before candidate checkout. Inspect its failed log after incident recovery and
rerun only the failed jobs if it still classifies as infrastructure failure.

`starter-postgres v0.1.0-preview.1` is the only release in this wave already
fully published and independently audited. It has exactly five expected
assets; the published key is byte-identical to the committed anchor; annotated
tag object `ee9fda1a6b8c51fb06559d42d94fa668b03141db` targets the candidate commit;
and the independent verifier reported:

```text
Spice library release github.com/spice-framework/starter-postgres@v0.1.0-preview.1 verified: 5 artifacts at b39b0d86454f0bb8a559d5d81350b888d319fab7.
```

The earlier OIDC `v0.1.0-preview.4` pilot was also published and audited, but
it does not replace the queued `v0.1.0-preview.5` candidate.

## Safe resume procedure

1. Confirm the official GitHub Actions incident is resolved.
2. Query every run above again. Preserve successful jobs, approvals, and
   artifacts; do not start replacement release runs merely because a run was
   queued for a long time.
3. For a completed failure, inspect `gh run view RUN_ID --repo
   spice-framework/REPOSITORY --log-failed`. Use `gh run rerun RUN_ID --repo
   spice-framework/REPOSITORY --failed` only for a confirmed transient hosted
   failure.
4. Query the run's pending deployments before approval. Approve
   `release-signing` only after validate and plan are green, and approve
   `release-publish` only after sign and independent verification are green.
   Require the API to report that the current user can approve the deployment.
5. After publication, download the release into a clean temporary directory.
   Verify the exact expected asset set, checksum signature, committed public
   anchor, annotated tag target, source archive, SPDX SBOM, and candidate
   commit using the independent verifier at toolchain commit
   `71211498297c9ab77cc05c4844db5e64e0170896`.
6. Record the audit result. Only then describe that repository's release as
   complete.
7. Advance catalog or consumer version pins only in separate, locally verified
   commits after the corresponding releases are public and audited.

## Deliberately deferred maintainability work

Centralizing copied starter quality-gate orchestration is optional follow-up,
not a missing part of this completed migration. A partial implementation was
discarded before this handoff so no unfinished production code remains.

If revisited, preserve repository-owned database, broker, mail, protocol,
coverage, and real-service acceptance suites. The audit found four concrete
parity items for a future central profile: Kafka, MySQL, PostgreSQL, and SMTP
release verification does not invoke the full quality gate; Redis release
verification omits compatibility; four starters lack release concurrency;
and the central verifier must bootstrap only from committed vendor contents.
Those findings must be addressed without weakening any existing local gate.
