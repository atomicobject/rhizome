---
type: TechnicalSpec
id: SPEC-0129
aliases:
  - SPEC-0129
  - Team API gateway
summary: "Atomic Object staff sign in once per computer with their Workspace Google account, and every Rhizome process on that account uses Voyage embeddings and Jev evaluation through an Atomic-run gateway that holds the shared provider keys. The gateway issues its own revocable sessions, enforces hard per-person and global admission limits, never replays an ambiguous paid request, and lets the distributed key bundle be retired. Direct provider keys keep working unchanged."
spec-status: proposed
last-updated: 2026-11-04
---

# Team API gateway

## Summary

Atomic pays for Voyage and TypeSafe (Jev) and shares those keys with staff through a 1Password Environment and an encrypted bundle compiled into internal releases ([credential policy](../../engineering/secrets.md), [teamkeys](../../../pkg/teamkeys/teamkeys.go)). A copied key cannot be taken back from one person, and rotating it affects everyone.

This spec replaces key distribution with a small gateway that Atomic runs in AWS. A person signs in once per OS account with their Atomic Google Workspace account. After that, the desktop app, the CLI, vault runtimes, and every repository on that account send Voyage and Jev calls to the gateway with a short-lived session token, and the gateway adds the provider key. The keys never reach a laptop. Disabling a person or revoking a session stops their paid calls within a minute, and hard admission limits bound what any one person, or everyone together, can spend in a day.

Direct provider keys remain a supported mode for public users and for client work that must bill a separate account. Without a session, Rhizome behaves exactly as it does today.

The [build plan](../../reference/analysis/rhizome-team-api-service-build-plan.md) holds the detailed design, the 30-row acceptance matrix, and the delivery batches. The [research note](../../reference/analysis/rhizome-team-api-service-2026-10-29.md) records the alternatives considered. This spec is the contract; where the two disagree, this spec wins and the plan is revised.

## Goals

- One sign-in per OS user serves every Rhizome process and every pinned version that understands sessions, and it renews without prompting for up to 30 days.
- No Atomic-paid provider key is stored on a developer machine.
- Access ends promptly and individually: one person, or one session, without rotating a key for everyone.
- Spending is bounded by enforced limits, not alerts, and retries never cause a second paid call for the same logical request.
- Existing request behavior is preserved: request bodies and options, response schemas, request IDs, retry guidance, and cancellation.
- The distributed bundle and the shared 1Password keys can be retired.

## Non-Goals

- Hosting repositories, indexes, or a remote vault runtime.
- A general provider proxy, arbitrary upstream URLs, or other providers (OpenAI, Anthropic, Cerebras). Voyage reranking (`RHIZOME_RERANK_PROVIDER=voyage`) keeps using direct keys.
- An admin web UI, self-service portal, VPN, mutual TLS, device attestation, or multiple regions.
- A team embedding cache. It is a later, separately gated phase (plan Batch 8).
- Managed sign-in on Linux and Windows in the first release. Those platforms keep direct keys until their secret-store adapters pass native checks.
- Automatic revocation when a person is suspended in Google Workspace. Offboarding disables the person in the gateway explicitly (Decisions).
- Changing behavior for anyone without a session.

## Key Entities

- **Person:** a verified Google identity (`sub`) with an email, hosted domain, status (`active` or `disabled`), and optional limit overrides.
- **Session:** one signed-in device for a person, with rotating access and refresh tokens, a 14-day idle limit, and a 30-day absolute limit.
- **Session record:** the client's copy of a session, stored in the OS secret store and keyed by gateway origin.
- **Logical request:** one paid provider call as the caller sees it, identified by an idempotency key that is stable across transport retries.
- **Receipt and reservation:** the gateway's durable record of a logical request and the budget it holds until the outcome is known.

## User Stories

### US1 - Sign in once per computer

- id:: ^SPEC-0129-US1
- summary:: An Atomic staff member signs in with Google once, and every Rhizome process on their OS account uses the session until it expires or they sign out.
- status:: draft

#### Acceptance Criteria

- `rzm auth login` opens the system browser to Google, receives the code on a loopback address (`127.0.0.1`, random port) with PKCE, `state`, and `nonce`, and stores the session. `--no-browser --port <p>` prints the URL for SSH port forwarding.
- The CLI, the desktop sidecar, MCP and vault runtimes, and two repositories pinned to different session-aware versions all use the one stored session with no keychain prompt.
- The session renews itself in the background. Twenty processes refreshing at once produce one server rotation and no reuse error.
- `rzm auth status` reports the signed-in email, origin, and expiry without printing a token. `rzm auth logout` revokes the server session and clears the local record; offline, it clears the record and says remote revocation is incomplete.
- A late browser callback or refresh cannot recreate a session after logout.

### US2 - Use Voyage and Jev without a provider key

- id:: ^SPEC-0129-US2
- summary:: With a session and no direct key, indexing, search, and Jev evaluation work through the gateway, with the same results and errors as direct access.
- status:: draft

#### Acceptance Criteria

- Voyage request bytes reach the upstream unchanged, and status, body, `Retry-After`, `retry-after-ms`, and `x-typesafe-request-id` come back unchanged.
- Switching an existing index from a direct key to the gateway does not re-embed anything; index identity remains provider, model, and dimensions.
- Gateway-originated errors are distinguishable from upstream errors and map to typed client errors (Requirements, Gateway contract).
- With no session, every existing test of direct-key behavior passes unchanged.

### US3 - Access ends when it should

- id:: ^SPEC-0129-US3
- summary:: Only eligible Atomic accounts can sign in, and disabling a person or revoking a session stops new paid calls within 60 seconds.
- status:: draft

#### Acceptance Criteria

- A non-Workspace account, an unaccepted hosted domain, an unverified email, a disabled person, and a person outside the pilot allowlist are each rejected at sign-in and at refresh.
- `person disable` stops that person's new paid calls within 60 seconds; `session revoke` stops one session and leaves the person's other sessions working.
- Atomic's offboarding checklist includes the gateway disable step, and a drill shows it working.

### US4 - Spending stays inside hard limits

- id:: ^SPEC-0129-US4
- summary:: Per-person and global limits hold under concurrency and deployments, and no retry turns into a second paid call.
- status:: draft

#### Acceptance Criteria

- Concurrency limits hold under load and across a deployment; a request that waits more than 5 seconds for a slot gets `rate_limited`.
- One hundred concurrent requests near the daily limit never over-admit.
- Voyage and TypeSafe have independent daily currency allowances, with atomic conservative reservations and no cross-provider budget borrowing. Published pricing and context assumptions remain visible; these are gateway accounting limits rather than guarantees against vendor price changes or unrelated account charges.
- Retrying an idempotency key after a lost response never dispatches again; reusing a key with a different body is rejected.
- An ambiguous failure after dispatch is terminal for that logical request in both providers, including Voyage's adaptive batch splitting.
- The kill switch blocks new paid calls within 15 seconds.

### US5 - Sign in from the desktop app

- id:: ^SPEC-0129-US5
- summary:: Rhizome Desktop shows the team account in Settings and setup, and signs in, cancels, and signs out through its sidecar.
- status:: draft

#### Acceptance Criteria

- Settings groups provider access with the signed-in email or a **Sign in with Atomic Object** button; setup offers the same button. Signing in does not silently change a provider's route.
- Voyage and Jev each offer **Atomic Object server** or **Your key**. Users can add, replace or clear saved keys, or import them from a 1Password Environment. Importing keys does not change the selected route. Existing keys are never returned for display.
- Settings distinguishes the app's next-run defaults from the actual connections reported by each running repository. It shows pending Voyage restarts and reports unknown routing for older runtimes instead of inferring it from sign-in.
- Sign-in runs `rzm auth login --json` through the bridge and can be cancelled. Sign-out runs logout.
- The packaged app on macOS completes all three against a local gateway.

### US6 - Retire distributed keys

- id:: ^SPEC-0129-US6
- summary:: After the pilot, Atomic stops distributing provider keys and rotates the ones already copied.
- status:: draft

#### Acceptance Criteria

- Session users are unaffected when the bundle is removed and the old keys are rotated.
- Older pinned binaries fail with a clear provider 401, and the remedy (advance the repository pin) is documented.
- Provider dashboards show no traffic on the retired keys.

### US7 - Admins check usage and manage access

- id:: ^SPEC-0129-US7
- summary:: An admin sees usage against limits and changes limits, people, sessions, and switches from `rzm`, and every change is audited.
- status:: draft

#### Acceptance Criteria

- `rzm auth admin usage` shows today's global and per-person usage against limits.
- An admin can disable a person, revoke sessions, change limits, flip the kill switch, and change eligibility; each change appears in `rzm auth admin audit`.
- A signed-in non-admin gets `not_admin` from every admin command.

### US8 - Run the whole stack locally

- id:: ^SPEC-0129-US8
- summary:: A developer runs the gateway, a development sign-in service, and fake providers with one command, signs in, and indexes, searches, and runs Jev through it, with diagnostics to explain any failure.
- status:: draft

#### Acceptance Criteria

- `make team-api-dev` starts the stack; `rzm auth login --server http://127.0.0.1:<port>` signs in through the development identity provider in a browser.
- With no direct keys, `rzm index`, search, and Jev evaluation succeed on the test vault through the gateway.
- The same stack runs against the real providers with the development keys.
- Server and client diagnostics show per-phase timings for each request.

## Requirements

### Identity

- MUST use Google OIDC directly with a Desktop OAuth client in an Atomic-owned Google Cloud project whose consent screen audience is Internal. Scopes are `openid email`.
- MUST exchange the authorization code on the gateway, verify the ID token's signature against Google's keys, and check `iss`, `aud`, `exp`, `nonce`, `email_verified`, and `hd` against a server-side accepted-domain list. The `hd` request parameter is a hint only.
- MUST NOT retain Google refresh tokens, or overwrite a disabled person's status during sign-in.
- MUST check person and session status on every refresh and paid request, with authorization caches of at most 30 seconds.

### Sessions

- MUST issue opaque tokens (`rzt_a_<session>.<secret>`, `rzt_r_<session>.<secret>`, 32 random bytes each), store only their SHA-256 hashes, and compare them in constant time.
- MUST expire access tokens after 10 minutes and rotate both tokens on every refresh. Sessions end after 14 idle days or 30 days from creation; rotation never extends the absolute limit.
- MUST keep the previous refresh hash for a fixed 120-second grace period so a lost refresh response can be recovered, without extending the grace period. Reuse of the known predecessor after the grace period revokes the session; an unknown token never revokes anything.
- MUST distinguish `access_refresh_required` (reload the shared record, refresh if needed, replay once) from revocation, and never clear a healthy shared session because an older in-memory access token was rejected.

### Local session store

- MUST store one record per OS user and origin in the OS secret store. On macOS this is a login-keychain generic password (service `rhizome-team-session`, account = origin) written and read only through `/usr/bin/security`, with the record passed on standard input, never in argv.
- MUST NOT fall back to a plaintext file. A missing or locked store returns a typed unavailable error, and direct-key mode remains available.
- MUST coordinate refresh across processes with a lock file in the user's Rhizome config directory, rereading the record after taking the lock.
- MUST NOT store the session in `config.yml`, and MUST NOT overwrite a record whose version it does not understand.
- MUST resolve the store and lock location from the real OS account, never from repository-supplied configuration.

### Trusted origin and precedence

- MUST take the gateway origin only from an interactive `rzm auth login`: a compiled-in default for release builds, public and internal, or `--server` with a root URL that is HTTPS, or plain HTTP only on a loopback host (`127.0.0.1`, `localhost`, `[::1]`) for local development. Repository configuration, dotenv files, and tool input cannot set or read it.
- MUST send session tokens only to the session's origin, over HTTPS except for a loopback origin, refuse redirects, and take proxy and TLS settings from trusted user or OS configuration, not repository dotenv. In session mode, a custom Voyage endpoint fails with `team_session_endpoint_conflict`.
- MUST record the signed-in origin in one non-secret, user-global selector file (`team-session.json` beside the session lock), written by login and cleared by logout under the session lock, and resolve every consumer's origin from it: an explicit `--server`, then (development builds only) `RHIZOME_TEAM_API_ORIGIN` naming an origin that already has a stored session, then the selector, then the default origin.
- MUST refuse to route provider calls to a loopback gateway that reports fake upstreams unless `RHIZOME_TEAM_API_ORIGIN` names it explicitly, so a selector left pointing at a fake stack cannot put fake vectors in a real index.
- MUST run `rzm auth` on the invoked executable, bypassing repository delegation, pin installation, and dotenv loading.
- MUST respect an explicit user-global `provider_access` choice independently for Voyage and TypeSafe. `server` uses only the team session, overriding caller-supplied, environment and saved keys; an unavailable session fails without trying another billing source. `key` uses caller-supplied, environment and saved keys in that order, without a team-session or legacy-bundle fallback. Repository configuration cannot supply this preference.
- MUST preserve existing resolution when no choice is saved: an explicit direct key, then the team session, then the legacy bundle until it is retired. Development builds continue to prefer a signed-in loopback session in this automatic mode. A gateway failure never falls back to another billing source.
- MUST NOT start sign-in from CI or any unattended call; those return a typed sign-in-required error.

### Provider access settings and reporting

- MUST report route decisions through the same resolver used to construct providers, without keys, tokens, partial secrets or credential fingerprints. Reports may identify the provider, selected preference, route, key provenance/name, saved-key presence, gateway origin and bounded unavailability reason.
- MUST save route preferences and direct keys only in private user-global configuration, atomically and while preserving unrelated settings. Desktop writes share the existing global setup/import lock. Key values travel through stdin JSON and never appear in arguments, logs, results or failure messages.
- MUST distinguish a runtime's constructed Voyage connection and last constructed Jev client from its current next-construction decision. A changed Voyage route requires a runtime restart. Before the first Jev construction, report that no evaluation client has been created rather than inventing an active route.
- MUST carry optional runtime reports through the existing identity-verified loopback health/status path. Missing reports mean unknown, including older pinned runtimes. Desktop restart actions retain existing headless/attached ownership rules.
- MUST keep health responses independent of credential-store and configuration I/O by returning the latest cached provider snapshot while refreshing it in the background. A pending initial report says checking; a locked keychain must not make a healthy runtime fail its liveness probe.

### Gateway contract

- MUST implement the v1 routes, headers, error codes, and bodies defined in [`pkg/teamapi/wire`](../../../pkg/teamapi/wire/wire.go). Changes within v1 are additive.
- MUST forward only to compile-time upstream URLs, strip the caller's `Authorization`, cookies, and hop-by-hop headers, and redact the provider key from passed-through error bodies.
- MUST reject, before admission: unknown routes and methods; disallowed models (Voyage `voyage-4-lite`; TypeSafe `jev-latest` or numeric release IDs `jev-MAJOR.MINOR.PATCH`, at most 64 characters with no leading zeroes); more than 1,000 Voyage inputs; Voyage `output_dimension` above 1,024; bodies over 32 MiB (Voyage) or 2 MiB (TypeSafe); unknown cost-affecting options; and malformed or multiple JSON documents. Jev releases such as `jev-1.13.0` do not require an exact allowlist entry or inclusion in the provider's model list. Upstream decides whether a syntactically valid release exists.
- MUST make at most one upstream attempt per admitted idempotency key, with the receipt and reservation persisted before dispatch. A crash around dispatch is recorded as an unknown outcome, never as permission to resend.
- MUST propagate client cancellation upstream, without claiming that cancellation refunds inference.

### Admission and budgets

- MUST admit paid calls in this order: authenticate, person status, kill switch, route and size validation, per-person slot, global slot (5-second wait), budget reservation, forward, settle, release.
- MUST start with 4 concurrent paid calls per person and 8 globally, revised only from measurements and provider limits.
- MUST enforce daily per-person and global limits on request count and input bytes, and reserve a conservative charge for each allowed model before dispatch, atomically with the receipt (one store transaction). A model without a defensible charge bound stays disabled for live use.
- MUST settle each receipt once, against the UTC day it was admitted on, and keep the full reservation when the outcome or actual usage is unknown.
- MUST enforce separate per-provider daily USD allowances using integer nanodollars. Reserve the reviewed maximum billable request cost atomically with the receipt, person/global credit counters, provider counter, and process lease condition. Missing or zero provider allowances disable paid admission for that provider. Record the pricing/accounting version on the receipt; keep full currency holds on uncertain outcomes and release only verified unused charges. Keep existing shared credit controls.
- MUST expose provider allowance, reserved/settled currency, unknown outcomes, and reviewed price/context assumptions through the authenticated admin API and ops reports. Runtime provider allowances cannot exceed startup ceilings. Keep requested Jev IDs and original request bytes unchanged; a changed provider price/context contract requires reviewing the accounting assumptions. All accepted Jev releases use the same reviewed currency reservation and rate. A release without a configured shared-credit charge inherits `jev-latest`'s reservation and settlement rate.
- MUST migrate provider accounting with paid calls off and no old active requests. Refuse to silently assign zero currency usage to previously admitted legacy requests. Disable paid calls and drain before rollback to a binary that does not enforce provider allowances.
- MUST keep receipts for as long as their idempotency keys can be presented; this build never deletes them.
- MUST give each serving process a unique instance ID and a renewable shared-store lease, and record the owner on every receipt. Admission and receipt transitions require a live owner lease; dispatch and settlement also require matching receipt ownership. Recovery runs at startup and periodically, permanently fences only expired owners, and changes only their in-progress receipts to unknown with reservations kept. Lease renewal and recovery fencing MUST be atomic against each other. An expired or fenced owner cannot renew, and late settlement after recovery MUST change neither the receipt nor its counters. Task clocks MUST track UTC.
- MUST bound how many request bodies are buffered at once, separately from paid admission, so uploads waiting to be validated cannot exhaust memory.
- MUST recheck the kill switch and provider switches immediately before reservation.
- MUST run one serving task at steady state. During the pilot, the brief old/new overlap of a canary deployment is accepted: daily limits and charge reservations stay exact because they are atomic in the shared store, while the in-process concurrency slots can briefly allow up to twice their limits. Before rollout (Batch 7), and before ever running more than one steady-state task, the concurrency slots MUST move into the shared store.

### Admin API

- MUST serve the admin routes in [`pkg/teamapi/wire`](../../../pkg/teamapi/wire/wire.go) only to a valid session whose person is active and named in the server's admin list. Others get `not_admin` (403) without learning whether the target exists.
- MUST take the admin list and accepted domains from server configuration; the API can read them but never change them.
- MUST let admins read daily usage against limits (global and per person), people, sessions, runtime configuration, and the audit log; and change person status and limit overrides, revoke one session or all of a person's sessions, toggle the kill switch and per-provider switches, and change eligibility mode, the allowlist, and default limits.
- MUST append an audit entry for every admin change, naming the acting admin, action, and target.
- MUST expose the same operations through `rzm auth admin` subcommands with `--json`, so people and agents use one authenticated path.

### Logging and operations

- MUST log one structured record per request (request ID, person `sub`, session ID, route, model, counts, sizes, statuses, duration, client version) and never bodies, headers, tokens, query strings, upstream error bodies, or email addresses. Retention is 30 days.
- MUST record per-phase timings for each paid request (authentication, admission wait, reservation, upstream call, settlement), and for sign-in and refresh.
- MUST write diagnostics through `pkg/diagnostics` under the server's data directory in local runs, separate from any vault's diagnostics, and as JSON lines on stdout when deployed.
- MUST record client-side gateway events (refresh, lock waits, gateway calls with timings and error codes) in `rzm`'s existing diagnostics.
- MUST keep provider keys in Secrets Manager, out of images and CI, and deploy only from the protected main branch through a protected environment with GitHub OIDC.

### Code placement

- MUST keep `pkg/teamapi/wire` standard-library only and keep `pkg/teamapi` free of `pkg/app`, `pkg/vault`, SQLite, and CGO; it builds with `CGO_ENABLED=0`.
- MUST put the server in this repository as a nested Go module, `services/team-api`, with its own `go.mod` and `go.sum` and no vendor directory, so OIDC, SQLite-server, and later AWS dependencies never enter `rzm`'s module. It imports this module's `pkg/teamapi/wire` and `pkg/diagnostics` through a local `replace`. Account-specific deployment configuration stays out of the repository.
- MUST keep server state behind narrow store and ledger interfaces, each with a shared contract test suite. Local runs use a SQLite file in a gitignored data directory, deleted by `make team-api-dev-reset`. The deployed store is DynamoDB (Batch 5) and must pass the same suite. Store operations are atomic business operations with explicit preconditions, never caller-composed transactions.
- MUST let server configuration cap the limits the admin API can set, so a session readable by same-user processes cannot raise limits without bound.

### Local development

- MUST start the whole local stack with `make team-api-dev`: the gateway, a development identity provider, and fake Voyage and TypeSafe upstreams, on loopback ports only.
- MUST implement the development identity provider as a real OIDC issuer: discovery, a JWKS endpoint, RS256-signed ID tokens, PKCE, and one-time codes, with a sign-in page that offers synthetic identities (an Atomic member, an admin, an outsider, an unverified email, a wrong hosted domain). The gateway verifies its tokens with the same code path it uses for Google; only the issuer, keys, and client ID differ.
- MUST make the development identity provider and fake upstreams unreachable in a deployed build: the production configuration accepts only Google's issuer and the fixed provider URLs, and refuses to start otherwise.
- MUST give the fake upstreams deterministic responses and fault injection (slow responses, errors after dispatch, dropped responses).
- MUST make a development build of `rzm` (any build the release tooling did not stamp with `version.Channel=release`) prefer a stored session for a loopback origin over direct keys, and start the local gateway when it is not running: one per OS user, live upstreams, keys read from the `rhizome-development` keychain items. An auto-started gateway never uses fake upstreams, so fake vectors cannot enter a real index. Release builds, and development builds with no such session, keep the normal precedence.
- MAY run the local gateway against the real providers with the development keys from the 1Password Environment (`scripts/secrets/run.py development`). The keys go only to the gateway process, never to the `rzm` under test.
- MUST route every production provider construction through one application factory that chooses direct, session, or bundle access; provider libraries never read the session store.

## Edge Cases

- **Locked keychain or headless session:** the store returns its unavailable error; the person signs in again from an unlocked session or uses direct keys.
- **Older pinned binary:** works through the bundle or a direct key until the bundle is retired, then fails with a provider 401.
- **Two gateway origins:** each has its own record; changing origin requires a new sign-in and never carries tokens across.
- **Response lost after a successful paid call:** a retry with the same key returns `result_unavailable`; the gateway does not store response bodies.
- **Embedding batch rejected as too large:** adaptive splitting creates new child keys. Ambiguous failures never trigger splitting.

## Dependencies and Assumptions

- External prerequisites X1–X9 in the [build plan](../../reference/analysis/rhizome-team-api-service-build-plan.md#external-prerequisites). The Google Cloud project, Desktop client, and accepted domain list (X2, X3) block the remaining Batch 1 prototypes. The AWS account (X1) blocks deployment.
- Dedicated gateway keys for Voyage and TypeSafe, with provider terms that allow one organization key to serve all staff through a proxy (X5).
- Verified 2026-10-31 on macOS (EFF-2026-10-31-12-30):
  - Two ad-hoc signed binaries with different code hashes saved, read, overwrote, and deleted each other's login-keychain item through `/usr/bin/security` with no prompt. The item's access list trusts `/usr/bin/security` by Apple's designated requirement.
  - `security -i` exits zero when a command fails, and a missing keychain path silently falls back to the default keychain. The adapter reads back every write and rejects a missing path.
  - The largest Voyage request Rhizome can build serializes to at most 20.6 MiB, and the largest response at 1,024 dimensions is at most 23.5 MiB. At 2,048 dimensions the response could exceed 32 MiB, hence the 1,024 cap.

## Delivery Phases

The plan's batches apply: Batch 0 (prerequisites, this spec, the effort), Batch 1 (contract and session foundation, ending in a foundation review), Batches 2–3 (server and client), Batch 4 (desktop), Batch 5 (staging deployment), Batch 6 (security verification and pilot), Batch 7 (rollout and key retirement), and the gated Batch 8 (team embedding cache).

## Decisions

Drew approved these on 2026-10-31, accepting the plan's defaults and then revising code placement and local state:

- **Direct Google OIDC, not Cognito.** Cognito documents loopback callbacks as testing-only, and the gateway needs its own revocation check either way.
- **Keychain trust through `/usr/bin/security`.** Any process running as the user can read the session while the keychain is unlocked, the same exposure the repository already accepts for development keys. Revocation and budgets bound the damage. Developer ID signing with a keychain access group is the upgrade path.
- **Server in this repository (revised 2026-10-31).** A nested module, `services/team-api`, keeps server dependencies out of `rzm` while one checkout runs the whole stack. This replaces the earlier private-repository decision; security does not rely on the server source being private.
- **SQLite for local state, DynamoDB when deployed (2026-10-31).** An ephemeral, gitignored SQLite file backs local runs and tests. Both council reviewers recommended DynamoDB for the Fargate deployment: ECS-managed EBS volumes do not survive task replacement, and SQLite warns against network filesystems such as EFS. The store interfaces are therefore atomic operations with explicit preconditions, which DynamoDB conditional writes can implement, and the DynamoDB store must pass the same contract suites in Batch 5.
- **Admin API (2026-10-31).** Admins named in server configuration manage usage, limits, people, sessions, and switches through authenticated API routes and `rzm auth admin`.
- **Loopback HTTP (2026-10-31)** for local development only.
- **Development keys (2026-10-31).** Local live runs may use Drew's development Voyage and TypeSafe keys from 1Password.
- **Offboarding disables the person in the gateway explicitly.** No Google refresh tokens are held, so Workspace suspension alone does not end a gateway session.
- **ECS Express Mode on its AWS hostname (revised 2026-11-01).** The gateway runs as one Fargate task behind Express Mode's load balancer, at `https://<service>.ecs.<region>.on.aws` with an AWS-issued certificate, so no Atomic DNS record or IT change is needed. Express Mode deploys are always canary, which replaces the earlier stop-and-replace decision and the slot overlap it allows; see the placement requirement in Admission and budgets. The hostname belongs to the service, so the service is treated as permanent; moving to a custom domain later means a new compiled-in origin and one more sign-in per person. Express Mode adds no charge beyond the task, load balancer, and public IPv4 addresses (about $60–75 a month for one task).
- **macOS first.** Managed sign-in ships on macOS; Linux and Windows keep direct keys until their store adapters pass native checks.

## Open Questions

- TypeSafe's own request-size limit and pricing rules (X7). Rhizome's Jev callers do not bound request size today, so the 2 MiB gateway ceiling is a new limit; confirm it against TypeSafe's documented maximum before the pilot.
- The exact JSON number format Voyage uses for embeddings. The 25-bytes-per-float bound is conservative; the Batch 5 live smoke test should confirm it.
- Pilot values for daily request and byte limits, and the conservative charge basis per model (X7, X8).

## Documentation Plan

- `services/team-api/README.md` for running the local stack, the development identity provider, live runs, the admin commands, and where diagnostics go (Batch 2).
- `pkg/teamapi/CONTEXT.md` for the trusted origin, precedence, no-fallback billing, and store-format rules (Batch 3).
- Updates to [secrets.md](../../engineering/secrets.md), the CLI, search, MCP, and agent-surface subsystem notes for changed contracts, and `CHANGELOG.md` (Batch 3).
- `services/team-api/RUNBOOK.md` for operations (Batch 5).
- [RELEASING.md](../../RELEASING.md) and secrets.md again when the bundle is retired (Batch 7).
