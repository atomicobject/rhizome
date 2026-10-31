---
type: EffortNote
id: EFF-2026-10-31-12-30
aliases: [EFF-2026-10-31-12-30]
name: Team API gateway
created-at: 2026-10-31T16:29:31Z
plan-approved-by: Drew Colthorp
status: active
summary: "Deliver SPEC-0129: Google Workspace sign-in with one shared macOS session per OS user, an Atomic-run AWS gateway that holds the Voyage and TypeSafe keys and enforces hard admission limits, Rhizome client and desktop integration, a pilot, and retirement of the distributed key bundle. Approval currently covers Batch 0 and the Google-independent part of Batch 1."
governing-specs: ["[[team-api-gateway]]"]
---

# Team API gateway

## Scope

Implement [[team-api-gateway|SPEC-0129]] through the batches of the [build plan](../reference/analysis/rhizome-team-api-service-build-plan.md). Drew accepted the plan's default answers on 2026-10-31:

| Decision | Choice |
|---|---|
| Identity | Direct Google OIDC with gateway-issued sessions; no Cognito |
| macOS session storage | Login keychain through `/usr/bin/security`, readable by any process running as the user |
| Server code | This repository, as the nested module `services/team-api` (revised; was a private repository) |
| Local state | An ephemeral, gitignored SQLite file; the deployed store is chosen in Batch 5 |
| Admin | An admin API for admins named in server configuration, used through `rzm auth admin` |
| Local origin | Plain HTTP allowed only for loopback hosts |
| Live local runs | May use Drew's development keys from 1Password, injected only into the gateway |
| Offboarding | An explicit gateway disable step; Workspace suspension alone is not revocation |
| Hosting | One serving task with stop-and-replace deploys |
| Platforms | macOS first; Linux and Windows keep direct keys |

Excluded: SPEC-0129 Non-Goals, including Linux and Windows managed sign-in, Voyage reranking, and the team embedding cache (plan Batch 8, separately gated). The pre-existing issue that direct Voyage keys follow a repository-configured endpoint is a separate hardening task (Follow-ups).

## Spec Set (Frozen)

- [[team-api-gateway|SPEC-0129]], 2026-10-31 revision: US1–US8 and all requirements. Refrozen the same day after adding the admin API, local development, and in-repository server requirements (Deviations).

## Stories In Scope (Frozen)

All eight stories of SPEC-0129 (`^SPEC-0129-US1` through `^SPEC-0129-US8`).

## Spec Coverage Checklist

- [ ] US1 sign in once per computer (Batches 1 and 3).
- [ ] US2 Voyage and Jev without a provider key (Batches 2 and 3).
- [ ] US3 access ends when it should (Batches 2 and 6).
- [ ] US4 spending stays inside hard limits (Batches 2, 5, and 6).
- [ ] US5 desktop sign-in (Batch 4).
- [ ] US6 retire distributed keys (Batch 7).
- [ ] US7 admin usage and access management (local build).
- [ ] US8 the whole stack runs locally (local build).
- [ ] Identity, session, store, origin, gateway, admission, logging, and placement requirements (Batches 1–3, verified in 5–6).

## Plan

The plan is the [build plan](../reference/analysis/rhizome-team-api-service-build-plan.md) at its 2026-10-29 revision, with one change: managed sign-in ships on macOS only, which removes the Linux and Windows store adapters from Batch 3 and acceptance row A22 from this effort.

| Batch | Outcome | Gate |
|---|---|---|
| 0 | Prerequisites recorded, this spec and effort | X1–X9 owners named |
| 1 | Frozen v1 wire contract and session record; loopback sign-in against a local stub with a real Google test client (P1–P5, P7) | `foundation-review`, then re-estimate |
| 2 | Gateway server in the private repository, tested against fake Google, fake upstreams, and DynamoDB Local | Plan Batch 2 exit evidence |
| 3 | Rhizome client integration: `rzm auth`, the provider factory, seams, typed errors | `make check-full`, cross-repository e2e |
| 4 | Desktop sign-in | Packaged-app run |
| 5 | Staging stack with synthetic upstreams | A12, A15, P6, deploy and rollback drill |
| 6 | Production pilot with 2–3 allowlisted people | Live acceptance rows, independent security review |
| 7 | Rollout and bundle retirement | A25, no traffic on retired keys |

**Local build (approved 2026-10-31).** Everything that needs neither Google nor AWS, built and verified on one machine:

| Lane | Scope | Owner |
|---|---|---|
| Contract | `pkg/teamapi/wire` additions (admin routes and types) | Lead |
| Server | `services/team-api`: config, development identity provider, OIDC verification, sessions and rotation, the SQLite store behind a store interface with a contract suite, proxy routes, admission and budgets, idempotency receipts, admin API and audit, fake upstreams with fault injection, diagnostics, `make team-api-dev` | Worker |
| Client | `pkg/teamapi/session` (record, lock, refresher, transport), `pkg/teamapi/login` (loopback PKCE), `cmd/auth.go` including `rzm auth admin`, the provider factory and seam changes, typed errors, client diagnostics | Worker |
| Integration | End-to-end script with the real `rzm` against the local stack, the adversarial suite, live runs with development keys, foundation review, documentation | Lead |
| Desktop | Batch 4 sign-in, verified through the bridge | After integration |

The foundation review runs as an independent review once sessions and admission work end to end; its findings are fixed in place unless they change the frozen contract.

**Escalation boundary.** Stop for Drew if P1 or P2 fails (the callback or audience design changes), if a provider says "no" to proxied team keys (X5), if the foundation review changes a frozen contract, or before any AWS resource, Google configuration, or provider key is created.

## Plan Approval

- 2026-10-31T16:29:31Z: Drew approved the plan's default decisions and the macOS-first change ("OK. Let's do it."), authorizing Batch 0 and the Batch 1 work that needs no Google project: the wire contract, the macOS keychain proof (P5), and the size and memory measurements (P7). Batch 1's Google prototypes (P1–P4) wait for X2 and X3. Batches 2 onward need Drew's go-ahead after the foundation review and re-estimate, as the plan requires.
- 2026-10-31T17:18:43Z: Drew approved the local build ("Build as much as you can"): the server, client, admin API, and local stack, with the revised decisions in Scope. AWS, Google configuration, the pilot, and key retirement remain outside this approval.

## Original Intended Delivery

An Atomic staff member runs `rzm auth login` once, or signs in from Rhizome Desktop, and every Rhizome process on their Mac uses Voyage and Jev through the gateway without holding a key. Disabling them stops their paid calls within a minute, spending stays inside enforced limits, and Atomic stops distributing provider keys.

## Actual Delivered

In progress. So far, on `t3/team-api-proxy-auth` (uncommitted):

- SPEC-0129 and this effort.
- `pkg/teamapi/wire`: the v1 routes, headers, error codes, JSON bodies, and request ceilings, with tests for its standard-library-only imports and for the size ceilings.
- `pkg/teamapi/session`: typed store errors and the macOS keychain adapter, with an opt-in round-trip test against a temporary keychain.

## Execution Notes

- 2026-10-31T16:29:31Z: effort opened from `dbc04f3a` on `t3/team-api-proxy-auth`, with the build plan and research note uncommitted on the same branch. `rzm agent current-user` is not configured in this worktree; approval is recorded by name as in earlier efforts.
- 2026-10-31T16:32:37Z, keychain probe on a temporary keychain:
  - An item created by `security add-generic-password` trusts `/usr/bin/security` by Apple's designated requirement (`identifier "com.apple.security" and anchor apple`), so the trust survives OS updates. `-U` updates the item and keeps its access list.
  - **Surprise:** `security -i` exits zero when a command fails, and a keychain path that does not exist silently falls back to the default (login) keychain. The bad-path probe wrote one dummy item (service `x`, account `y`, value `z`) to Drew's login keychain; its creation time matched the probe, and it was deleted and confirmed gone. The adapter therefore reads back every write and refuses a missing keychain path, and its test covers the missing path.
  - In argv mode, `find-generic-password` reports a missing item only through exit code 44 (the low byte of -25300), so the adapter matches the exit code.
- 2026-10-31T16:35:00Z, P5 on the login keychain with a synthetic origin (`https://p5.invalid`): two programs built from different sources (ad-hoc signatures, CDHash `9a19e29a…` and `bd2f94ae…`) saved, read, overwrote, and deleted each other's item through `session.Keychain`, each under a 20-second timeout, with no prompt. The item was gone afterward. **P5 passes for the CLI.** Not yet covered: the packaged desktop sidecar (its `rzm` does not contain this code until Batch 3; it calls the same `/usr/bin/security`, so no difference is expected), and behavior with a locked keychain.
- 2026-10-31T16:36:00Z, P7:
  - **Voyage request:** Rhizome packs at most 1,000 inputs and 900,000 estimated tokens at four bytes per token (`pkg/embeddings/provider_voyage.go`, `batcher.go`), so at most 3.6 MB of raw text. Text made entirely of characters Go escapes to six bytes serializes to 21,603,083 bytes (20.6 MiB), under the 32 MiB ceiling.
  - **Voyage response:** at 25 bytes per float, 1,000 vectors of 1,024 dimensions are at most 24,628,024 bytes (23.5 MiB). At 2,048 dimensions it could reach about 49 MiB, so SPEC-0129 caps `output_dimension` at 1,024, the size Rhizome uses by default. People who configured 2,048 dimensions keep direct keys.
  - **Gateway memory:** buffering a worst-case request with `io.ReadAll` and decoding the validated fields allocated 147.2 MiB in total, mostly from buffer doubling. At 8 concurrent requests that still fits the planned 2 GB task, but Batch 2 should size the buffer from `Content-Length` and count inputs without decoding every string. Recheck peak memory in AWS (Batch 5).
  - **Jev:** no Rhizome caller bounds request size (`pkg/typesafe`, `pkg/app/mcp/tool_evaluate*.go`, the compounding scripts), so the 2 MiB ceiling is a new limit. It stays proposed until TypeSafe's documented maximum is known (X7; SPEC-0129 Open Questions).
- 2026-10-31T16:37:20Z, verification:
  - `GOCACHE=$PWD/.gocache go test -count=1 ./pkg/teamapi/...` passed.
  - `TEAMAPI_KEYCHAIN_TEST=1 GOCACHE=$PWD/.gocache go test -count=1 -v ./pkg/teamapi/session/` passed.
  - `CGO_ENABLED=0 go build ./pkg/teamapi/...` and `GOOS=linux CGO_ENABLED=0 go build ./pkg/teamapi/...` passed (A26).
  - `go vet ./pkg/teamapi/...` and `gofmt -l pkg/teamapi` were clean.
  - `make check-fast` stopped at `web-deps` because this worktree has no `web/node_modules`; no web files changed. Its Go and content targets, `make lint go-analysis file-size-check vet test-content`, passed (golangci-lint: 0 issues).
  - `./scripts/rzm validate` reported 0 issues. `rzm agent validate frozen-scope-drift` reported 2 issues, both in older efforts (EFF-2026-10-01-10-06, EFF-2026-10-03-18-33), and only a same-day note for this one.

## Deviations

- 2026-10-31T17:18:43Z: Drew moved the server into this repository, chose SQLite for local state, and added an admin API (US7) and the local stack (US8). SPEC-0129 was revised and refrozen the same day. frozen-scope-drift acknowledged for SPEC-0129: the spec set is refrozen at this revision.
- 2026-10-31T16:29:31Z: macOS-only managed sign-in, approved by Drew with the plan defaults. Acceptance row A22 (Linux and Windows stores) leaves this effort.

## Closure Checklist

- [ ] Required quality gates pass.
- [ ] Alignment and actual outcomes are verified.
- [ ] Specs and documentation are reconciled.
- [ ] Follow-ups are triaged.

## Compounding Follow-ups

- **Repository-configured Voyage endpoint:** today a direct `VOYAGE_API_KEY` is sent to whatever `embeddings.endpoint` a repository configures (`pkg/embeddings/config.go`). Track as its own hardening task.
- **Local key broker (Drew, 2026-10-31):** run the same gateway code as a per-user local service, started by `rzm` or Rhizome Desktop, holding a person's own provider keys so they leave environment variables and config files. It prevents accidental key exposure to agents, not access by a deliberate process running as the same user. A separate effort after this one.
- **Linux and Windows managed sign-in:** Secret Service and Credential Manager adapters, with native checks (plan A22).

## Status

Active, in Batch 1. Done: the wire contract, the macOS keychain adapter, P5 for the CLI, and P7. Next, blocked on Atomic IT: a Google Cloud project under Atomic's organization with an Internal consent screen and a Desktop OAuth client, test accounts, and the accepted domain list (X2, X3). Those unblock the loopback sign-in spike and P1–P4. Then the foundation review and a re-estimate before Batch 2.
