---
type: ReferenceDoc
summary: "Staged build plan for the Atomic Object team API service: Google Workspace sign-in, one shared per-user session for desktop and CLI, server-held Voyage and TypeSafe keys, hard admission limits, and migration away from distributed keys."
reference-kind: analysis
last-verified: 2026-10-29
status: accepted
---

# Team API service: build plan

Drew accepted this plan's default decisions on 2026-10-31, with one change: managed sign-in ships on macOS first. [[team-api-gateway|SPEC-0129]] is the contract, and it wins where the two disagree (for example, it caps Voyage `output_dimension` at 1,024). [EFF-2026-10-31-12-30](../../efforts/2026-10-31-12-30-team-api-gateway.md) records the approved scope and progress. The plan itself authorizes no cloud resources, Google configuration, or credential changes. It builds on the [research note](rhizome-team-api-service-2026-10-29.md) and changes two of its recommendations, explained in [What changed from the research note](#what-changed-from-the-research-note).

Evidence basis: source inspection at `dbc04f3a`, the engineering policy documents, and the official documentation cited inline, read on 2026-10-29. Opus 5.5 at high effort drafted the plan; the parent independently checked and revised its security and integration contracts. The parent built the local CLI with `NO_WEB=1 make build`, started a minimal Rhizome session, and read the live ReferenceDoc authoring guide. A combined indexed-context request timed out; code findings below come from direct source inspection. The local AWS CLI session had expired, so effective account permissions remain unverified.

## Decision summary

Settled direction (from the user, not reopened here):

- A shared server in AWS holds Atomic's shared paid Voyage and TypeSafe/Jev keys. Those keys never reach developer machines; separately configured personal/client-provider keys remain a supported direct mode.
- Sign-in uses Google accounts in Atomic's Workspace.
- One automatically renewing session per OS user is shared by the desktop app, the CLI, and every repository.
- Security should be real but proportionate. The service is written in Go. Embedding caching comes after the MVP.

Recommended choices made by this plan (each can be challenged, but each has a default):

| Topic | Choice | Main reason |
| --- | --- | --- |
| Identity | Use Google OIDC directly. The gateway issues its own opaque sessions. **No Cognito.** | Google supports loopback redirects for desktop apps. Cognito calls loopback callbacks "for testing purposes only." The gateway needs its own revocation check either way. |
| Callback | Loopback `http://127.0.0.1:<random port>` with PKCE and `state`. The gateway exchanges the code. | No custom HTTPS handoff, no URL scheme, and no client secret on laptops. |
| Workspace eligibility | Internal OAuth audience, verified Google identity and `hd`, and server-controlled person status. Gateway disablement is a required step in Atomic's offboarding process. | No unsupported promise that Google suspension revokes a separate gateway session. No Google refresh tokens retained by this service. |
| Tokens | Opaque random tokens, stored hashed in DynamoDB. Ten-minute access tokens and rotating refresh tokens. Sessions expire after 14 idle days or 30 days total. | Request-time session checks provide revocation. Google ID tokens are verified at login; the gateway does not issue JWTs. |
| Local session store | macOS login keychain through `/usr/bin/security`; Windows Credential Manager; Linux Secret Service. Cross-process refresh locking. No automatic plaintext fallback. | Prove the macOS utility's actual prompt/ACL behavior with ad-hoc builds. Other platforms retain direct-provider access if no supported secret store is available. |
| Hosting | One ECS Fargate service behind an ALB, defined in CloudFormation, with a deliberately serialized stop-and-replace deployment. | Fits synchronous multi-MB calls. A single serving task keeps admission simple; brief deployment downtime is an explicit tradeoff. |
| State | One on-demand DynamoDB table holding people, sessions, usage counters, and runtime config. | Conditional updates enforce hard budgets with no extra service. |
| Secrets | Two dedicated provider keys injected by ECS from Secrets Manager. Rotation redeploys the task. | The public Google Desktop client needs no confidential server credential. Keep secrets out of images and CI. |
| Budgets | Atomic per-person and global admission counters and conservative charge reservations, with strict model/option allowlists. Provider-enforced currency caps wherever available. | Approximate token counts are telemetry, not a hard spending bound. Unknown charges retain their reservation. |
| Code placement | Client and wire contract here as small leaf packages; server and deployment in a proposed private repository. | Keeps AWS dependencies and deploy permissions independent. Verify the dependency graph rather than assuming any shared package is stdlib-only. |
| Reranking (`RHIZOME_RERANK_PROVIDER=voyage`) | Excluded from the MVP. It keeps using direct keys and is disabled when none is set. | It is an environment-gated experiment, not a default path. |

Blocking external facts and who must resolve them: see [External prerequisites](#external-prerequisites). Three items block live work and cannot be answered from the repository: which AWS account to use and what it may do, the Google Cloud project and OAuth client under Atomic's organization, and new dedicated provider keys whose terms allow team-wide proxy use.

## What changed from the research note

1. **Cognito is replaced by direct Google OIDC with gateway-issued sessions.** The note preferred Cognito unless a prototype showed its constraints cost more than they saved. The documentation already shows that:
   - Cognito documents loopback callbacks as testing-only ([CreateUserPoolClient](https://docs.aws.amazon.com/cognito-user-identity-pools/latest/APIReference/API_CreateUserPoolClient.html)). Supporting a standalone CLI would require a platform URL handler or a separately reviewed hosted handoff.
   - Google's installed-app guidance directly supports desktop loopback redirects and PKCE. Desktop applications are public OAuth clients, so an embedded client secret is not an application-authentication boundary ([Google native apps](https://developers.google.com/identity/protocols/oauth2/native-app)).
   - Revoked Cognito JWTs "will still be valid if they are verified using any JWT library that verifies the signature and expiration" ([Cognito revocation](https://docs.aws.amazon.com/cognito/latest/developerguide/token-revocation.html)). The gateway therefore needs a server-side session lookup regardless.
   - Cognito's own token refresh does not ask Google whether the person still belongs to the Workspace.

   Direct Google OAuth is the recommendation for this bounded Google-only service. It removes federation/callback machinery while making gateway-session correctness our responsibility. Cognito would still supply managed token lifecycle features; a session table alone does not make those features equivalent. Batch 1 must prove the custom session contract before downstream implementation.
2. **The macOS Keychain item trusts `/usr/bin/security` rather than Rhizome's code signature.** The desktop app ships with an ad-hoc signature (`"signingIdentity": "-"` in [tauri.release.conf.json](../../../desktop/src-tauri/tauri.release.conf.json), and the Gatekeeper "Open Anyway" flow in [RELEASING.md](../../RELEASING.md)). Repositories pin many `rzm` versions ([repo delegation](../../../cmd/repo_delegate.go)). Without a Developer ID team identifier there is no stable keychain access group. A legacy ACL keyed to an ad-hoc code hash would prompt again after every update and for every pinned version, and it fails in headless runtimes. The repository already accepts this trust level in [scripts/secrets/run.py](../../../scripts/secrets/run.py), which stores development keys in the login keychain. As [secrets.md](../../engineering/secrets.md) notes, "any process running as the user can read those two items while the keychain is unlocked."

The note's other points stand: fixed operations only, keys only on the server, enforced admission rather than alerts, no prompt or body logging, a trusted origin, and keeping direct-provider configuration available.

## Outcomes

1. An Atomic Workspace member runs `rzm auth login`, or uses **Sign in with Atomic Object** in desktop setup or Settings, once per OS user. After that, desktop, CLI, headless vault runtimes, and every repository on that account use Voyage embeddings and Jev evaluation without holding a provider key.
2. The session renews itself for up to 30 days, or until it sits idle for 14 days. Signing out on one machine ends only that machine's session.
3. A person outside the accepted Workspace domains, a disabled person, or a revoked session cannot start a paid provider request. New admissions stop within 60 seconds of gateway disablement. Google-only suspension is not claimed to revoke an existing gateway session; Atomic's offboarding process must invoke gateway disablement.
4. Per-person and global admission, reservation, and concurrency limits hold across requests and deployment transitions. Exhaustion produces a typed error. Actual usage is measured separately; the service does not label a characters-divided-by-four estimate a hard token or currency cap.
5. Requests keep their current wire behavior: upstream bodies and options, response schemas, request IDs, retry guidance, cancellation, and the rule that ambiguous TypeSafe failures are not replayed.
6. The internal key bundle and the shared provider keys can be retired and rotated without redistributing any retired key.

## Non-goals

- Hosting repositories, indexes, a knowledge graph, or a remote vault runtime.
- A general provider proxy, arbitrary upstream URLs, OpenAI, Cerebras, Anthropic, or Voyage reranking.
- An admin web UI, a self-service portal, a custom security dashboard, a VPN, mutual TLS, device attestation, or multi-region deployment.
- A team embedding cache in the MVP. It is gated behind measurements in Batch 8.
- Rewriting the provider clients. Add explicit dynamic-auth and logical-request identity hooks, with focused changes to retry classification for gateway outcomes. Preserve direct-provider behavior.
- Changing public-user behavior. Without a session, Rhizome resolves credentials exactly as it does today.

## End-to-end flow

```text
Sign-in (once per OS user)
  rzm auth login [--server https://rhizome-api.<atomic domain>]   (desktop runs the same command through its sidecar)
    1. GET  {origin}/v1/meta                     -> google_client_id, api_version, min_client_version
    2. bind 127.0.0.1:<random>; create PKCE verifier, S256 challenge, state, and nonce
    3. open the system browser to Google's authorization endpoint
         (client_id, redirect_uri=http://127.0.0.1:<port>, scope="openid email",
          code_challenge, state, nonce, hd=<primary domain> as a hint only)
    4. accept exactly one GET with a matching state; show a "return to Rhizome" page; close the listener
    5. POST {origin}/v1/auth/google {code, code_verifier, redirect_uri, nonce, device_name}
         server: exchange the code at Google's token endpoint using the public Desktop client;
                 verify the signed ID token and claims (iss, aud, exp, nonce, email_verified, hd);
                 upsert the person without overwriting disabled status; check eligibility;
                 create a gateway session; discard Google tokens after verification
       <- {session_id, access_token, access_expires_at, refresh_token, session_expires_at, email, limits}
    6. under the cross-process lock: atomically publish the record in the OS secret store

Paid call (any rzm process: CLI, MCP, vault runtime, desktop sidecar)
  provider (Voyage or TypeSafe, explicit auth and request-ID hooks) -> protected session transport
    read the record (cached in memory) -> if the access token expires within 60 s: lock, reread, refresh, save, unlock
    set Authorization: Bearer <access token>; send to {origin}/v1/providers/<provider>/<upstream path>
  gateway: authenticate (session and person lookup, cached 30 s) -> kill switch -> route and model
           allowlist -> body size and input count -> per-person, then global, semaphore (wait at most 5 s)
           -> atomic request receipt and budget reservation in DynamoDB -> forward the original bytes to the fixed
              upstream with the server key -> settle usage -> release
  the response passes through (status, body, Retry-After, upstream request ID); gateway-originated
  errors carry the Rhizome-Gateway-Error header

Refresh: POST /v1/auth/refresh {refresh_token}
  server: hash lookup; idle and absolute expiry; person and session status;
          compare-and-swap rotation with bounded predecessor-token recovery
```

## Authentication and session design

### Google configuration

- A Google Cloud project owned by Atomic's Google Cloud Organization. The OAuth consent screen audience is **Internal**, which Google says limits authorization to members of the organization; outsiders get `org_internal` ([Google audience](https://support.google.com/cloud/answer/15549945)).
- One OAuth client of type **Desktop app**. Its client ID is public and served by `/v1/meta`. Treat any Desktop client secret as public protocol metadata, never as proof of the application or user. Use the current documented public-client exchange fields, proven in P1. Do not add a third secret merely to hide a public client value.
- Scopes are `openid email` only.

### Eligibility

The Google checks run at login. Every gateway refresh and paid request also checks the person and session status:

1. Verify the Google ID token signature with an established OIDC library and Google's keys. Check `iss`, `aud` (the configured Desktop client), `exp`, and the nonce from the initiating flow. Do not accept a client-supplied user ID, arbitrary issuer, or token-provided key URL ([Google OIDC](https://developers.google.com/identity/openid-connect/openid-connect)).
2. `email_verified` is true and `hd` is in the server's accepted-domain list. The `hd` request parameter is only a hint, and an email suffix is not proof of membership ([backend auth](https://developers.google.com/identity/sign-in/web/backend-auth)).
3. The person's record is `active`, and the runtime config `eligibility_mode` is either `all`, or `allowlist` with this verified identity on the list. The production pilot uses allowlist mode; staging is a separate stack with no production keys. Never overwrite disabled status during login upsert.
4. **Offboarding.** Retain no Google refresh token. Gateway disablement and session revocation are a required, owned offboarding action, not an optional fallback. P4 proves the disablement drill and records the distinction from Google-only suspension. If Atomic requires automatic propagation from Workspace, add an explicitly authorized Directory-based reconciliation with a defined interval before broad rollout. Do not infer such a guarantee from successful OAuth refresh or from Google's RISC service, which excludes Workspace accounts ([RISC](https://developers.google.com/identity/protocols/risc)).

### Tokens and sessions

- **Format.** Access tokens look like `rzt_a_<session_id>.<32 random bytes base64url>` and refresh tokens like `rzt_r_<session_id>.<…>`. The server stores SHA-256 hashes and compares them in constant time. A session-ID pointer resolves the person/session record without a secondary index; account for both reads in capacity estimates. Session creation and its pointer are one transaction.
- **Lifetimes (proposed).** Access tokens last 10 minutes. Every refresh rotates both tokens. Sessions expire after 14 idle days or 30 days from creation; rotation never extends the absolute lifetime. Use 32 cryptographically random bytes per secret and keep access and refresh purposes distinct ([RFC 9700 §2.2.2](https://www.rfc-editor.org/rfc/rfc9700.html)).
- **Crash between rotation and save.** The server retains the immediately previous refresh-token hash with a fixed 120-second grace deadline. A recovery using that predecessor may issue a new pair while retaining the original predecessor and deadline; it must not extend the grace period. Rotation, recovery, and logout use conditional updates so competing requests cannot overwrite a newer or revoked state. Matching the known predecessor after its grace deadline revokes the session. An arbitrary nonmatching token returns invalid credentials and cannot revoke a session merely by naming its ID. Batch 1 must test repeated lost responses and this state machine.
- **Access-token rotation.** A process may still have an older access token in memory when another process refreshes. Distinguish `access_refresh_required` from explicit session revocation. On that pre-admission response, take the shared lock and reread the current store before deciding whether to refresh; replay the rejected request at most once. Never clear a healthy shared session merely because an older access hash was rejected.
- **Revocation.** New admissions stop after the per-task authorization cache expires, at most 30 seconds. Already-dispatched work can still finish. Logout serializes with refresh, revokes the server session, and clears local state without allowing a late refresh to recreate it. Offline logout clears local state and explicitly reports that remote revocation remains incomplete. State-read failures fail closed after any still-valid bounded cache entry expires.

### Local session store

One record per OS user per origin:

```json
{
  "v": 1, "origin": "https://…", "session_id": "…", "email": "…",
  "access_token": "…", "access_expires_at": "…",
  "refresh_token": "…", "session_expires_at": "…",
  "limits": {"voyage_concurrency": 4, "typesafe_concurrency": 4}
}
```

- **macOS.** Login keychain, generic password, service `rhizome-team-session`, keyed to the complete canonical origin including port. Base64url-encode the record before supplying it to `security -i` on standard input; tokens never appear in argv. Capture reads from `security find-generic-password -w` through a private pipe, never diagnostics. The proposed `/usr/bin/security` ACL allows ad-hoc Rhizome builds to share an item, but P5 must prove creation/update/delete and upgrade prompt behavior on a clean keychain. Any process running as that user can then request the credential through the trusted utility. A locked/unavailable keychain returns a typed error without falling back to a file.
- **Linux and Windows.** Use Secret Service and Windows Credential Manager through narrow platform adapters. No automatic plaintext-file fallback. If the store is absent or locked, return `secure_store_unavailable`; explicit direct-provider mode remains available. macOS is the initial desktop acceptance platform; CLI adapters must pass native-platform checks before that platform advertises managed sign-in.
- **Lock.** `<rzm config dir>/team-session.lock`, using `flock` on Unix and `LockFileEx` on Windows, following the existing patterns in [schema_lock_unix.go](../../../pkg/sqliteutil/schema_lock_unix.go) and [guard_windows.go](../../../pkg/vault/indexlock/guard_windows.go). The refresh sequence: read the record without the lock. If the access token is still valid for more than 60 seconds, use it. Otherwise take the lock (with a 30-second timeout), read again, and if another process has not already refreshed, refresh and write before releasing the lock.
- **Config isolation.** The store never lives in `config.yml`, which older pinned binaries rewrite and can drop fields from ([secrets.md](../../engineering/secrets.md)).
- **Real user ownership.** Resolve the credential-store identity and lock directory from the real OS account at the application boundary, not a repository-supplied `Env.HomeDir`. Dependency injection for tests remains explicit and cannot become a production environment override.
- **Versioning.** A client that sees a record version it does not understand never overwrites it. It reports "a newer Rhizome manages this session; update." Format changes must be additive.
- **Login and logout races.** Starting login does not hold the refresh lock while a user is in the browser. Publication checks an account generation under the lock so a cancelled login or a completed logout cannot be undone by a late callback. Changing origin requires explicit sign-in and never carries an existing token to the new origin.
- **Upgrade path.** If Atomic later adopts Developer ID signing with a team keychain access group, only the macOS backend changes.

### Trusted origin

- The origin is stored in the session record, set only by an interactive `rzm auth login`. Internal builds compile in a default origin; any other origin requires `--server` and must be an HTTPS root with no path, query, user info, or fragment. Tests inject transports instead of relying on an insecure runtime flag.
- Repository configuration, `.envrc` files, and tool input cannot change the origin or read the session. Rhizome never sends session credentials to a repository-configured endpoint: in session mode, a Voyage `embeddings.endpoint` other than the default fails with `team_session_endpoint_conflict` instead of being followed.
- `rzm auth` stays on the invoked executable and bypasses repository delegation, pin installation, and dotenv loading. Add the exception to both [main.go](../../../main.go) and [repoexec trust/selection](../../../pkg/app/repoexec/trust.go). Ordinary provider calls also use a credentialed transport whose proxy, TLS roots, and origin come from trusted application/OS configuration captured independently of repository dotenv. A repository cannot install a proxy or trust root for these requests.
- A pre-existing issue, separate from this plan: today a direct `VOYAGE_API_KEY` *is* sent to whatever endpoint the repository configures ([embeddings config](../../../pkg/embeddings/config.go) keeps a custom Voyage endpoint). It needs its own hardening task.

### Credential precedence (client)

1. An explicit direct provider key: `VOYAGE_API_KEY`/`RHIZOME_VOYAGE_API_KEY`, or `TYPESAFE_API_KEY`, from the environment or user config. This preserves public users and client work that must use separate provider accounts, typically set per repository through direnv.
2. The team session, when a valid record exists.
3. The legacy internal bundle ([teamkeys](../../../pkg/teamkeys/teamkeys.go)), until Batch 7 removes it.
4. Otherwise unavailable, with the existing messages plus the hint `rzm auth login`.

A gateway failure never falls back to another billing source. `rzm auth login` offers to remove direct keys that were imported from the team 1Password Environment, which is identified by the recorded `CredentialSource`, so that pilot users actually move onto the session.

### Headless, CI, Linux, and Windows

- **CI and unattended provider calls.** Never start browser login, prompt, or silently change billing sources. Return a typed sign-in-required error when managed access needs interaction. CI uses explicitly configured direct keys from protected environments ([secrets.md](../../engineering/secrets.md)), or no provider. Only an explicit `rzm auth login` or native sign-in action may initiate login.
- **Headless developer host over SSH.** `rzm auth login --no-browser --port <p>` prints the URL. The user forwards the port (`ssh -L p:127.0.0.1:p`) and completes sign-in in a local browser; Google accepts any loopback port for Desktop clients. The Device Authorization Grant is deferred: it adds a second OAuth client type and a user-code phishing surface for a rare case.
- **Linux and Windows desktops.** The CLI uses its native secure store and browser opener after platform acceptance. The packaged desktop acceptance in this effort is macOS. Do not advertise unattended Linux sign-in when no unlocked Secret Service exists.

## Gateway contract (v1)

Base: `https://<origin>`. JSON throughout. Every response carries `Rhizome-Request-Id`. Clients send `User-Agent: rzm/<version>`.

Every paid logical request also carries one client-generated idempotency key that stays stable across transport retries. Bind it server-side to the authenticated person, operation, and exact request-body hash; a reused key with different input is a conflict. The service stores only receipt metadata, not prompt or response bodies.

| Route | Auth | Behavior |
| --- | --- | --- |
| `GET /healthz` | none | Process liveness only, for the ALB. |
| `GET /v1/meta` | none | `{api_version, google_client_id, min_client_version}` |
| `POST /v1/auth/google` | none | Code exchange and eligibility check; returns the session. |
| `POST /v1/auth/refresh` | refresh token in the body | Rotates the tokens. |
| `POST /v1/auth/logout` | Bearer access token | Revokes this session; `204`. |
| `GET /v1/session` | Bearer | `{email, session_id, device_name, expires, limits, usage_today}` |
| `POST /v1/providers/voyage/v1/embeddings` | Bearer | Forwards to `https://api.voyageai.com/v1/embeddings`. |
| `POST /v1/providers/typesafe/v1/systemone` | Bearer | Forwards to `https://api.typesafe.ai/v1/systemone`. |
| `GET /v1/providers/typesafe/v1/models` | Bearer | Forwards to `https://api.typesafe.ai/v1/models`. |

**Pass-through rules.**

- Upstream URLs are compile-time constants; redirects are refused; environment proxy settings are ignored.
- The gateway validates the outer request and every cost-affecting option before admission:
  - **Voyage:** an explicit model allowlist, initially the current `voyage-4-lite`; `input` is a string or at most 1,000 strings. Proposed request-body ceiling: 32 MiB, with the existing model/input limits still enforced. P7 must cover JSON escaping, Unicode, and maximum dimensions rather than infer wire bytes from raw text length.
  - **TypeSafe:** exact approved model IDs and a separately controlled `jev-latest` alias, not a broad `jev-*` prefix. Proposed body ceiling: 2 MiB, subject to P7 against all existing Rhizome paths. Bound question counts and any billable options.
- Forward admitted request bytes unchanged, including supported `input_type`, dimensions/type, encoding, and truncation options ([Voyage API](https://docs.voyageai.com/reference/embeddings-api)). Unknown or newly cost-affecting options require a contract update before admission; do not silently bypass the bound with a future option.
- It removes the caller's `Authorization`, cookies, and hop-by-hop headers, and sets the provider key.
- Responses pass through status, body, `Content-Type`, `Retry-After`, `retry-after-ms`, and `x-typesafe-request-id`. In non-2xx bodies, any exact occurrence of the upstream key is replaced with `[REDACTED]`.
- Proposed response ceiling: 32 MiB. Use bounded reads and avoid unnecessary copies. P7 tests peak memory at the global admission limit before these ceilings or the task size become final.
- Reject ambiguous framing, unsupported content encodings, and malformed/multiple JSON documents before any paid call. Do not forward arbitrary client headers. Login and refresh have smaller body/rate limits and never consume paid admission slots.

**Gateway-originated errors.** Every gateway-originated error carries `Rhizome-Gateway-Error: <code>` and `{"error":{"code","message","retryable"}}`. Upstream responses never carry that header.

| Code | Status | Client transport mapping |
| --- | --- | --- |
| `access_refresh_required` | 401 | Lock and reread the shared store, refresh only if necessary, then replay once. Safe because nothing reached the upstream. |
| `session_revoked`, `session_expired`, `refresh_reused`, `not_eligible` | 401/403 | Clear the local record and return the typed error `ErrSignInRequired`, whose message contains "credential" for [compounding's detector](../../../pkg/app/cli/init/templates/starters/compounding/agents/skills/compounding/scripts/lib/calls.mjs). |
| `rate_limited` (admission wait expired) | 429 + `Retry-After` | Passed through. The providers' existing 429 handling applies. |
| `budget_exhausted` | 429 + `Retry-After` set to the reset time | Typed `ErrTeamBudgetExhausted{ResetAt}`, not retried. |
| `service_disabled` (kill switch) | 503 | Typed `ErrTeamServiceDisabled`, not retried. |
| `upstream_unavailable` (nothing was sent) | 503 | Passed through, so it is retryable as today. |
| `upstream_outcome_unknown` or `result_unavailable` | 409 | Typed terminal error for this logical request in both providers. Do not turn it into a generic network error or automatically split/retry an embedding batch. Recomputing an identical embedding can still incur a second charge. |
| `request_in_progress` | 409 + `Retry-After` | A status/retry of the same idempotency key may observe progress but never creates a second upstream call. |
| `idempotency_conflict` | 409 | Reject a reused key with different operation or body. |
| `model_not_allowed`, `request_too_large`, `bad_request` | 400/413 | Passed through; the providers report them as non-retryable. |
| `client_upgrade_required` | 426 | Typed error naming `min_client_version`. |

**Retries and cancellation.**

- The gateway makes at most one upstream attempt per admitted idempotency key. Persist the receipt and reservation before dispatch. A crash around dispatch is conservatively unknown, not permission to send the request again.
- Client retry behavior distinguishes failures before admission from failures after possible dispatch. An upstream HTTP 5xx is not proof that inference was free or did not run. Preserve safe pre-admission retries and existing direct-provider behavior; do not replay an ambiguous paid request in gateway mode.
- Each provider creates the key once per logical HTTP batch outside its retry loop. A deliberate later user retry creates a new key and can be billed again. Adaptive splitting after a definitive size rejection creates distinct child keys; ambiguity does not trigger splitting.
- Upstream requests use the inbound request context. Cancellation closes our connection but does not prove the provider stopped or refunded inference. Unknown outcomes retain their conservative charge reservation.
- Completed duplicate requests return `result_unavailable` if their response was lost. Replaying response bodies would require a separate retention decision; the MVP does not persist them.
- Prototype check P6 measures whether the ALB propagates client disconnects promptly.

**Timeouts.** Proposed upstream deadlines are 150 s for Voyage and 60 s for TypeSafe, with incoming cancellation propagated. Start with 5 s for request headers, 30 s for body reads, 180 s for responses, and 360 s for server connection idleness. ALB idle timeout is 300 s. P6/P7 verify these against maximum uploads, the existing client deadlines, and shutdown behavior rather than treating them as tested values.

## Admission, budgets, and limits

- **Order.** Authenticate → person status → kill switch → route, model, and size validation → per-person semaphore → global semaphore (maximum wait 5 s, then `rate_limited`) → budget reservation → forward → settle → release.
- **Concurrency.** One combined pool, initially 8 active paid calls globally and 4 per person, with provider sublimits no larger than those totals. These are conservative pilot values, finalized by P7 and provider rate limits. Advertise client ceilings and preserve bounded waits. A stop-and-replace deployment must prove the old task stopped before enabling admissions in the replacement; rolling overlap is not allowed. Multiple serving tasks require shared admission coordination before rollout.
- **Hard admission limits.** Track paid request count and input bytes per person and globally per UTC day. These are exact quantities we can enforce without approximating a tokenizer. Determine their pilot values and the provider-specific charge ceiling with the owner before enabling live calls.
- **Charge reservations.** Each allowed model/option set has a reviewed conservative maximum charge basis, covering billable input limits, output limits, provider-added overhead, and prices. Reserve integer credit units before dispatch. If no defensible bound is available, do not advertise a hard currency cap; keep that model disabled for the live pilot until X7 settles the rule. Provider-enforced currency caps remain the final protection against provider pricing/contract changes.
- **Atomicity.** Use one DynamoDB `TransactWriteItems` for the request receipt and conditional decrements of available person/global counters. Use supported numeric comparisons such as `available >= reservation`; do not implement two independent reservations with best-effort compensation. DynamoDB's client token alone expires after ten minutes, so the persisted receipt owns longer retry identity. [DynamoDB transactions](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/transaction-apis.html)
- **Settlement.** Apply a receipt-guarded transaction once. Record actual provider usage separately and release only a provably unused part of the reservation. Missing/ambiguous usage retains the full reservation. Duplicate settlement cannot refund twice. A crash can conservatively exhaust the day's available budget; operator reconciliation is safer than guessing that an unobserved call was free. If actual charges exceed a configured bound, disable the affected route and alert; already-incurred provider charges cannot be undone.
- **Runtime config item** (cached for 15 s): `paid_calls_enabled`, per-provider enable flags, `eligibility_mode`, the allowlist, accepted domains, default limits, and per-person overrides.

## Data model (one DynamoDB table, on-demand, PITR on, TTL attribute `expires_at`)

| Key (`pk` / `sk`) | Attributes |
| --- | --- |
| `PERSON#<google sub>` / `PROFILE` | email, hd, status (`active`/`disabled`), created_at, limits override; no Google refresh token |
| `PERSON#<sub>` / `SESSION#<session_id>` | device_name, created_at, last_used_at, absolute_expires_at, access_hash, access_expires_at, refresh_hash, prev_refresh_hash, prev_refresh_deadline, generation, revoked_at, client_version, expires_at (TTL) |
| `SESSION#<session_id>` / `LOOKUP` | person sub (a pointer, so a token resolves with one key read) |
| `USAGE#<sub or GLOBAL>` / `<date>` | exact admission counters, available charge credits, reserved/settled credits, actual usage; proposed 90-day retention |
| `REQUEST#<sub>#<idempotency key>` / `RECEIPT` | operation, body hash, reservation, dispatch/outcome/settlement state, timestamps; no bodies; retained beyond the maximum retry window |
| `CONFIG` / `GLOBAL` | runtime config |

TTL is cleanup only. Enforce idle/absolute expiry and revocation in application reads even when DynamoDB has not deleted a row. Keep revoked session state through its original validity window; reads must never recreate a revoked session. Prevent routine operators from re-enabling a disabled person by editing a cached profile or replaying login.

## Logging, retention, and support

- **What is logged.** One structured JSON line per request to stdout, sent to CloudWatch Logs with **30-day retention**. Fields: request_id, person sub, session_id, route, model, input count, estimated and actual tokens, upstream status, gateway code, duration, bytes in and out, and client version.
- **What is never logged.** Bodies, headers, tokens, query strings, upstream error bodies, or email addresses. The person table maps a sub to an email for operators.
- **Pilot measurement.** Start with request/byte/latency/usage counts. Any duplicate-input probe requires explicit bounded memory and retention limits; per-person duplicate counts do not establish cross-person cache savings. Do not call a same-machine measurement proof of team-wide reuse.
- **Alarms (SNS to the owner's email).** Elevated 5xx rate, upstream 401/403 (a revoked key), global budget above 80%, unhealthy target, and task restarts. An AWS Budgets alert on the account provides visibility only; it does not enforce anything.
- **Admin CLI** (server repository, operator AWS SSO credentials, direct DynamoDB access): `person list|disable|enable|limits`, `session list|revoke|revoke-all <email>`, `kill-switch on|off [--provider]`, `usage report [--date]`, `eligibility mode|allow|deny`.
- **Hard stop.** The kill switch rejects new paid admissions within 15 seconds. Scaling the service to zero terminates gateway work after ECS stops the task; neither action proves an already accepted provider inference was cancelled or unbilled.
- **Support model.** Proposed business-hours support, with Drew confirming the accountable owner before the pilot, plus a runbook in the server repository covering:
  - disabling a person;
  - stopping all paid calls;
  - rotating a provider key (update the secret, then force a new deployment);
  - rolling back an image;
  - restoring the table from point-in-time recovery.

## Infrastructure and ownership

- **AWS account.** Recommended: a dedicated production account under Atomic's AWS Organization, if account vending is routine (X1). Otherwise use separate scoped roles in the release account; audit any existing administrator roles rather than claiming the account is already isolated. The ECS execution role reads the two exact secret ARNs and writes the log group; the application task role accesses only its table and required operations. End users never receive AWS credentials.
- **Region.** Proposed `us-east-2`, unless the chosen account's existing resources argue otherwise.
- **CloudFormation template** (server repository `infra/team-api.yaml`):
  - networking: a VPC with two public subnets and no NAT; the task gets a public IP and a security group that admits traffic only from the ALB;
  - the ALB: HTTPS listener with an ACM certificate, idle timeout 300 s, HTTP→HTTPS redirect, `/healthz` target health check;
  - compute: an ECS cluster and a Fargate service (proposed 1 vCPU / 2 GB, desired count 1), with a pipeline-controlled stop-and-replace procedure; no period with two serving tasks;
  - registries and storage: an ECR repository (immutable tags, scan on push) and the DynamoDB table;
  - secrets: two Secrets Manager secrets (`voyage-api-key`, `typesafe-api-key`) with values set out of band; the public Google Desktop client ID is non-secret configuration;
  - operations: the log group (30 days), alarms with SNS, and the GitHub OIDC deploy role.
- **Why not ECS Express Mode.** It is a fine way to create these resources quickly ([Express Mode](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/express-service-overview.html)). But the custom domain and idle timeout must be changed on the resources it manages, which causes drift for reviewed infrastructure. Use it only for a throwaway spike, if at all.
- **Rejected hosting alternatives.**
  - Lambda Web Functions are real and stream for long requests. However, their endpoints "do not support custom domain names," so production needs CloudFront in front ([Web Function networking](https://docs.aws.amazon.com/lambda/latest/dg/web-functions-networking.html)). Their many execution environments would also push hard concurrency into distributed DynamoDB leases.
  - API Gateway REST with response streaming still leaves Lambda's request-size limit, and needs streaming integration work for 1,000-vector responses.
  - An HTTP API with buffered Lambda fails the 30-second and 6 MB limits outright ([research note](rhizome-team-api-service-2026-10-29.md#aws-deployment-choice)).
  - App Runner is closed to new customers.
- **Rough monthly running cost (estimate):** Fargate at 1 vCPU / 2 GB is about $36; the ALB is about $18 plus LCUs; public IPv4 is about $11; DynamoDB, Secrets Manager, and logs are a few dollars. That puts infrastructure at roughly $70 per month before provider usage. Confirm against the chosen region's prices.
- **Images and CI exposure.**
  - The image is built with `CGO_ENABLED=0` on a distroless static base. It contains no secrets, is pushed by digest, and is deployed by updating the stack's image-digest parameter.
  - Run as non-root with a read-only root filesystem and bounded writable temporary storage. Disable ECS Exec unless an explicitly authorized incident requires it. Keep the Go version and base image on supported, pinned releases.
  - Pull-request workflows get no AWS credentials. Deployment runs from the protected main branch through the protected production environment. Restrict OIDC audience and subject to the actual repository/environment, including immutable repository/owner IDs if that repository uses the newer subject format; do not paste a legacy example blindly. See [GitHub OIDC in AWS](https://docs.github.com/en/actions/how-tos/secure-your-work/security-harden-deployments/oidc-in-aws).
  - Provider keys are never present in CI.
  - Before replacing a task, close admission, drain bounded work, scale to zero, and verify the old task is stopped. Update the digest, start one task, verify health, then reopen admission. A failed replacement uses the same sequence with the previous digest. Limit deploy permissions separately from bootstrap/IAM administration.
  - Disable raw ALB access logs for auth routes unless the configured destination can omit query strings; OAuth codes and state must not enter request-URL logs. Use the application’s sanitized route-level logs. Do not expose an unauthenticated metrics/debug endpoint.

The rough $70/month figure is per continuously running stack, excludes provider usage, and is not an account quote. Keep staging temporary or budget it separately. Confirm Fargate, load-balancer, IPv4, storage, and log charges in the chosen region before deployment. [Fargate pricing](https://aws.amazon.com/fargate/pricing/), [load-balancer pricing](https://aws.amazon.com/elasticloadbalancing/pricing/), [VPC pricing](https://aws.amazon.com/vpc/pricing/)

## Code placement and seams

### This repository (public, the same module `github.com/atomicobject/rhizome`)

The wire package is stdlib-only. Session/login packages remain independent of `pkg/app`, `pkg/vault`, SQLite, and CGO, with small platform adapters and reviewed dependencies where needed for OS secret stores. The server's production build imports only the wire package. A separate application composition package connects account state to Rhizome providers.

| Path | Responsibility |
| --- | --- |
| `pkg/teamapi/wire` | Route constants, header names, error-code enum, auth and session JSON types, limits. Shared by the client and the server. |
| `pkg/teamapi/session` | Record, platform secure stores, lock, refresher, protected HTTP transport, and typed errors. Receives explicit trusted account paths and network policy; production callers derive them from the real OS user. |
| `pkg/teamapi/login` | Loopback listener, PKCE and state, `/v1/auth/google` exchange, browser launch through an injected opener. |
| `pkg/teamapi/CONTEXT.md` | Invariants: trusted origin, precedence, no fallback billing, store format versioning. |
| `pkg/app/provideraccess` | One application factory for direct versus managed provider access, readiness, transport injection, provider limits, and close ownership. Provider libraries do not read the account store themselves. |

Changes to existing seams (every provider construction site found at `dbc04f3a`):

| Seam | Change |
| --- | --- |
| [`embeddings.NewProviderForConfig`](../../../pkg/embeddings/provider_helpers.go) and [`ResolveAPIKeyForProvider`](../../../pkg/embeddings/provider_helpers.go) | Retain direct-provider resolution in the library. The application factory chooses direct key, managed account, or the temporary bundle. In managed mode it injects the approved endpoint, transport, limits, and an explicit dynamic authorization callback. Do not use a fake API key to satisfy constructor validation. |
| [`NewVoyageProvider`](../../../pkg/embeddings/provider_voyage.go) | Accept explicit HTTP transport and dynamic authorization, retaining the 120 s timeout. Require either a real direct key or the explicit managed-auth mechanism. Allocate a logical request ID before each physical batch's retry loop; preserve it on retries and give accepted split children their own IDs. |
| Construction sites in [bootstrap/live.go](../../../pkg/app/bootstrap/live.go), [codex_semantic.go](../../../pkg/app/bootstrap/codex_semantic.go), [indexing/unified_semantic.go](../../../pkg/app/indexing/unified_semantic.go), [unifiedsearch/resources.go](../../../pkg/app/unifiedsearch/resources.go), [web/server.go](../../../pkg/app/web/server.go), [cmd/code_analytics.go](../../../cmd/code_analytics.go) | Route production construction through the new application factory, preserving provider/cache close ownership. Metadata-only construction at `live.go` lines 1473 and 1501 must use the resolved non-secret provider identity, never a session token. |
| Index identity | No change is needed. Stored index metadata is provider, model, and dimensions ([metadata.go](../../../pkg/embeddings/metadata.go)), so switching between direct and gateway access does not force re-embedding. The endpoint-bearing metrics fingerprint and `LaneKey` change harmlessly. A test pins this. |
| [`mcp.newEvaluationClient`](../../../pkg/app/mcp/tool_evaluate.go) | Delegate to the application factory. Add an explicit dynamic-authorization option to TypeSafe rather than a sentinel key; validate that key-based and managed modes are unambiguous. Set the managed base URL and protected client. Allocate one request ID before the retry loop and classify terminal gateway outcomes without losing their typed cause. This seam covers MCP evaluation/batches, agent evaluation, code mode, [compounding](../../../pkg/app/cli/init/templates/starters/compounding/agents/skills/compounding/scripts/lib/calls.mjs), and [repo-audit](../../../scripts/repo-audit/run.mjs). |
| [`code_contract_evaluate.go`](../../../pkg/app/agentapi/code_contract_evaluate.go) | Update the credential sentence in the contract description. |
| First-run and credential checks: [configure_embed.go](../../../pkg/app/cli/init/configure_embed.go), [credentials.Session.Satisfied / teamCovered](../../../pkg/app/credentials/session.go), [desktop CredentialStatus](../../../pkg/app/desktop/credentials.go) | A valid session satisfies the Voyage and TypeSafe needs. `CredentialStatus` gains `team: {signedIn, email, origin, expiresAt}`. |
| CLI | New `cmd/auth.go`: login/status/logout. Register the family, then add the invoking-executable and no-dotenv exceptions in `main.go` and `pkg/app/repoexec`. Tests must invoke the real binary inside an untrusted, older-pinned fixture. Status reports routing without exposing credentials. |
| Desktop | Add bridge operations `team-sign-in` (runs `rzm auth login --json`, cancellable like `credential-import` in [bridge.rs](../../../desktop/src-tauri/src/bridge.rs)), `team-status`, and `team-sign-out`; add a Settings "Team account" section and a setup-sheet button in [Settings.tsx](../../../desktop/src/Settings.tsx) and [SetupSheet.tsx](../../../desktop/src/SetupSheet.tsx). |
| Not routed in the MVP | [`rerank.NewFromEnv`](../../../pkg/search/rerank/rerank.go) keeps explicit direct keys only and its documented optional fallback. Managed readiness/status must make this limitation visible. TypeSafe does receive the focused auth/request-identity changes above. |

**Cross-repository integration.** Production imports only `pkg/teamapi/wire` at a reviewed public module version; server tests may import the client packages. During development use an explicit local module replace or a pinned review commit for integration tests, never silently depend on the newest main branch. Remove local replacements before publishing. Tag publication is a separate authorized release step, not a prerequisite to test both branches together. Verify that neither the wire dependency graph nor the server binary brings in SQLite, CGO, or vault/runtime packages. The service does not import `pkg/embeddings` or `pkg/typesafe`; it forwards validated original bytes.

### New private repository `rhizome-team-api` (proposed name; owner Atomic)

`cmd/team-api` (server), `cmd/team-api-admin`, `internal/{auth,proxy,admission,store,config,logging}`, `infra/team-api.yaml`, test/deploy workflows, cross-repository `e2e/`, opt-in live smoke tests, and `RUNBOOK.md`. Dependencies include AWS SDK for Go v2 for DynamoDB, OAuth/OIDC libraries for Google code exchange and signature/claim verification, and the pinned Rhizome wire module. Use a supported Go release for the new service; do not silently expand scope to a root toolchain upgrade.

**Rejected:** putting the server in this module. That adds the AWS SDK to `rzm`'s vendor tree and license surface, and puts an Atomic deploy trust and account wiring into public product source. A nested module in this repository is workable but gains nothing over a separate repository, while complicating vendoring and CI.

## Acceptance matrix

Layer key: **U** is a unit test, **I** is an in-process integration test (httptest gateway plus fake upstreams), **E** is a cross-repository end-to-end test with synthetic upstreams, **D** is a packaged desktop run, and **L** is a live, separately authorized check.

| ID | Obligation | Evidence | Layer | Batch |
| --- | --- | --- | --- | --- |
| A1 | One sign-in serves the desktop sidecar, the CLI, and two repositories pinned to different session-aware versions under one OS user, with no keychain prompt. | Scripted run plus a packaged app. | I, D, L | 3, 4, 6 |
| A2 | Concurrent refresh from 20 processes yields exactly one server rotation and no `refresh_reused`. | Multi-process test against a fake gateway. | I | 3 |
| A3 | Repeated lost refresh responses recover only within the original 120 s grace; known predecessor reuse afterward revokes. Random tokens cannot revoke another session. | Fault-injection and clock-controlled tests. | U, I | 1, 2, 3 |
| A4 | A non-member account, a wrong `hd`, `email_verified=false`, a disabled person, and allowlist mode without membership are all rejected at login and at refresh. | Fake Google token endpoint; one live non-Atomic account. | I, L | 2, 6 |
| A5 | Admin disable stops paid calls within 60 s; revoking one session leaves the others working. | Clock-controlled test plus a live drill. | I, L | 2, 6 |
| A6 | Atomic's offboarding action disables the gateway account and prevents new admissions within 60 s; Google-only suspension is not advertised as sufficient. | Named owner and a live disablement drill. | L | 1, 6 |
| A7 | Voyage request bytes reach the upstream byte-for-byte, including every optional field; the response body, status, and headers pass through. | Recording fake upstream. | I | 2 |
| A8 | TypeSafe `systemone` and `models` round-trip, and `x-typesafe-request-id` is preserved. | Fake upstream, plus the existing TypeSafe client against the gateway. | I, E | 2, 3 |
| A9 | Ambiguous upstream failures never replay paid work automatically in either provider, including Voyage splitting. | Attempt-counting fake upstream and terminal error checks. | E | 2, 3 |
| A10 | Retrying the same idempotency key after a lost response does not dispatch again; a different body with that key conflicts. | Concurrent duplicate requests and response-loss injection. | I, E | 2, 3 |
| A11 | A client cancel cancels the upstream request within 1 s in-process; the ALB measurement is recorded. | Test plus P6. | I, L | 2, 5 |
| A12 | Per-person/global concurrency holds under load and task replacement; bounded waiting then returns `rate_limited`. | Load generator and observed deployment transition. | I, L | 2, 5 |
| A13 | Request receipts and person/global reservations are atomic; duplicate settlement cannot refund twice; unknown charges stay reserved. | DynamoDB Local concurrency tests and deployed DynamoDB smoke proof. | I, L | 2, 5 |
| A14 | The kill switch blocks new admissions within 15 s and in-flight requests finish; scaling to zero stops everything. | Test plus a live drill. | I, L | 2, 6 |
| A15 | A maximum batch (1,000 inputs, the full lite token budget) and a deliberately slow 110 s upstream succeed through the deployed ALB and task. | Synthetic upstream in AWS. | L | 5 |
| A16 | Repository endpoint, dotenv proxy/CA/path overrides, or tool input cannot redirect credentials or replace account identity. | Real-binary hostile-repository fixture with synthetic credentials. | U, I | 3 |
| A17 | Session credentials go only to the session's origin over HTTPS; redirects are refused; unknown routes and models are rejected. | Tests. | U, I | 2, 3 |
| A18 | Logs contain no seeded canary text, body, token, header, upstream error body, or key; upstream keys are redacted from passed-through error bodies. | Log scan over a synthetic corpus. | I, L | 2, 6 |
| A19 | Direct keys still take precedence; with no session, behavior is unchanged for public users. | Existing tests plus new precedence tests. | U | 3 |
| A20 | Switching an existing index from direct to gateway access does not trigger re-embedding. | Indexing test with the deterministic provider. | I | 3 |
| A21 | A locked keychain or a missing store gives `keychain_unavailable` or sign-in-required, never a file fallback on macOS. | Tests with an injected command runner. | U | 3 |
| A22 | Native Linux/Windows stores protect the record and coordinate refresh. Missing stores fail closed without plaintext fallback. | Platform CI plus native store smoke checks. | U, L | 3 |
| A23 | `rzm auth logout` revokes the server session and clears local state; offline logout clears local state and warns. | Tests. | I | 3 |
| A24 | Rotating a provider key causes no client change; the old key fails upstream after redeployment. | Live drill. | L | 6 |
| A25 | Retiring the internal bundle leaves session users unaffected; old pinned binaries fail with a clear provider 401 and the advance-pin guidance is documented. | Release check plus a pinned fixture. | L | 7 |
| A26 | `pkg/teamapi` has no forbidden dependencies and builds with `CGO_ENABLED=0`. | Dependency check. | U | 1 |
| A27 | The desktop app shows signed-in state, signs in, cancels, and signs out from Settings and setup in the packaged app. | Packaged app run. | D | 4 |
| A28 | `rzm auth` runs on the invoked executable without dotenv loading, repository trust, or pin bootstrap. | Real binary and an untrusted older-pinned sentinel fixture. | I | 3 |
| A29 | A late login/refresh callback cannot undo logout; an old in-memory access token reloads shared state without destroying a healthy account session. | Multi-process and callback-race tests. | I, D | 3, 4 |
| A30 | Wire-size limits cover escaped text, Unicode, maximum vectors, and all existing Jev paths; peak memory fits the selected task at the global limit. | Synthetic payload and memory measurements. | I, L | 1, 5 |

## Delivery batches

Effort ranges are engineer-day estimates for one engineer familiar with the codebase. They exclude the elapsed time spent waiting on external prerequisites and the pilot.

### Batch 0: external prerequisites and approvals (elapsed: days; 0.5–1 day of hands-on work)

- **Outcome.** Resolve prerequisites needed by the next batch, draft the normative spec from the accepted requirements, allocate the effort through live Rhizome authoring, and record approval of the exact scope/plan revision. Do not freeze the research note as though it were already a product specification. External setup can proceed alongside independent local implementation after approval.
- **Exit.** A written record of the account, region, domain, Google project and client, accepted domains, provider-key owners, and budget sign-off. Proof that `aws sts get-caller-identity` works for the operator role (run by Drew).
- **Escalation.** Any "no" on dedicated keys, the Internal OAuth audience, or the account choice changes the design. Stop and revise.

### Batch 1: contract and session foundation (3–5 days), foundation review exit

- **Outcome.** A frozen v1 wire contract and session record; working loopback sign-in against a local gateway stub backed by a real Google test client (P1–P4 answered).
- **Scope.** `pkg/teamapi/wire` and a spike of `pkg/teamapi/login` in this repository; a server-repository skeleton with `/v1/meta`, `/v1/auth/google`, `/v1/auth/refresh`, and an in-memory store.
- **Prototype questions** (each with a stopping condition):
  - **P1.** Does Google accept random-port loopback for the Desktop client, with the code exchanged by the server? Answered by one successful round trip.
  - **P2.** Does the Internal audience block a non-org account? Answered by one rejected attempt.
  - **P3.** Is the `hd` claim present for every accepted domain?
  - **P4.** Can the accountable operator disable a gateway account as part of offboarding and demonstrate denial within 60 seconds? Record that Google-only suspension is not an automatic gateway revocation mechanism. If automated propagation is required, obtain and test a scoped Directory integration before changing that promise.
  - **P5.** Do keychain items written by `security`, then read by two differently built `rzm` binaries and the packaged desktop sidecar, produce no prompt?
  - **P6** belongs to Batch 5: cancellation through the real load balancer.
  - **P7.** Measure maximum serialized requests, responses, and peak working memory using synthetic escaped/Unicode text and maximum supported vectors. Agree on caps and concurrency before implementation relies on them; verify again in AWS in Batch 5.
- **Exit evidence.** P1–P5 and P7 recorded in the effort; `GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/teamapi/...`; a dependency check proves the wire package has no application, vault, SQLite, or CGO imports, while client adapters have only their reviewed platform dependencies. `CGO_ENABLED=0 go build ./pkg/teamapi/...` passes where the chosen platform supports it. **Run `foundation-review`** on identity verification, token rotation/recovery, idempotency, admission accounting, and ownership before Batches 2 and 3 build on them.
- **Escalation.** Give each prototype at most one working day of investigation before reporting the specific failure and choosing an alternative. If P1 or P5 fails, the auth/store design returns to review rather than silently weakening the storage or callback contract. The whole Batch 1 range includes these proofs and the foundation review.

### Batch 2: gateway server (6–10 days, server repository)

- **Outcome.** A complete server, tested against fake Google and fake upstreams and DynamoDB Local.
- **Work.** Auth endpoints with signature verification, rotation, fixed grace, and replay-safe state changes; session/person cache; fixed proxy routes; admission semaphores; atomic request receipts and reservations; runtime config/kill switch; sanitized logging; typed errors; admin CLI; hardened Dockerfile. Fake upstreams must reproduce lost successful responses, provider failures after dispatch, and settlement failures.
- **Exit evidence** (proposed commands in the new repository): `go test -race ./...`, `go vet ./...`, `govulncheck ./...`, `gitleaks detect --redact`, and `docker build .`. Covers A3–A5, A7, A8, A11–A14, A17, and A18 with I-layer tests.
- **Escalation.** A material change to the contract frozen in Batch 1.

### Batch 3: Rhizome client integration (5–8 days, this repository; can overlap Batch 2 once Batch 1 exits)

- **Outcome.** The CLI and all provider seams use the session; behavior without a session is unchanged.
- **Work.**
  - `pkg/teamapi/session` and `pkg/teamapi/login` completed.
  - `cmd/auth.go`.
  - The application factory and seam changes listed above, native secure stores, no-dotenv/invoking-executable dispatch, trusted proxy/CA configuration, stable logical request IDs, credential checks, and error mapping.
  - Documentation: new `pkg/teamapi/CONTEXT.md` and application-factory guidance; provider/client context notes; the [CLI](../subsystems/cli.md), [search](../subsystems/search.md), [mcp-server](../subsystems/mcp-server.md), and [agent-surface](../subsystems/agent-surface.md) notes for changed contracts, with `last-verified` updates; owning bootstrap/indexing notes where needed; the code-mode reference template and compounding template docs; [secrets.md](../../engineering/secrets.md); `CHANGELOG.md` `[Unreleased]`.
  - Load the owning CLI, search, indexing, vault-core, MCP, and agent-surface notes/skills for the seams actually changed. Update the corresponding notes in the same change; do not broaden unrelated subsystem behavior.
- **Exit evidence.**
  - `make check-fast`.
  - `GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/teamapi/... ./pkg/app/provideraccess/... ./pkg/typesafe/... ./pkg/embeddings/... ./pkg/app/mcp/... ./pkg/app/credentials/... ./pkg/app/cli/init/... ./cmd/...` (loopback tests need local socket access, which must be recorded).
  - `make check`, then `make check-full`, which is required because the change touches indexing and provider paths.
  - `make build && ./scripts/rzm init && ./scripts/rzm init --check`.
  - `./scripts/rzm validate`.
  - `make credential-check`.
  - The server repository's `e2e` suite run against this branch (A1, A2, A9, A10, A16, A19–A23).
- **Escalation.** Changing direct-provider retry semantics, the approved managed retry contract, or stored index compatibility requires revisiting the foundation decision. The managed auth/request-identity changes above are in scope.

### Batch 4: desktop sign-in (2–4 days)

- **Outcome.** Sign-in, status, cancel, and sign-out in the packaged app (A27).
- **Exit evidence.** `make desktop-deps`, `make desktop-check`, `make desktop-build`, and an exercise of the packaged app against a local gateway with a fixture folder, following the desktop verification notes in [quality gates](../../engineering/quality-gates.md).

### Batch 5: infrastructure and synthetic deployment (4–7 days; needs Batch 0)

- **Outcome.** An isolated staging stack runs the Batch 2 image with controlled synthetic upstreams and a test domain. Staging gets no production provider keys. Production builds use only their fixed approved upstreams; fixture routing cannot be changed by a client request.
- **Work.** The CloudFormation template; the OIDC deploy role; the ECR repository; secrets created with placeholder values; alarms; runbook draft; `cfn-lint` (proposed); a deploy through the protected environment.
- **Exit evidence.**
  - A15: a maximum batch and a 110 s slow request through the ALB.
  - P6: the time for a client cancel to propagate through the ALB.
  - Stop-and-replace deployment and rollback under load, proving no overlapping serving tasks and conservative handling of unknown provider work.
  - A12: the load generator holds the limits.
  - Measured serialized request and response bytes and task memory at global concurrency, used to right-size the task.
  - IAM access-analyzer findings reviewed.
- **Escalation.** If the measured memory or time breaks the chosen limits, revise them. If the ALB is unsuitable, the hosting decision returns to planning.

### Batch 6: security verification and pilot (3–5 days of work; 1–2 weeks elapsed)

- **Outcome.** A separate production stack has dedicated live provider keys, allowlist mode with 2–3 pilot users, and completed drills. Use a distinct origin and session namespace from staging.
- **Work.**
  - Deploy the verified image with production-only fixed upstream configuration. Do not repurpose a fixture-enabled stack with production keys.
  - Run the separately authorized tiny live smoke test (proposed `TEAMAPI_LIVE_TEST=1 go test -tags live ./live/...`, synthetic text only, modeled on the [TypeSafe live test](../../../pkg/typesafe/live_test.go)).
  - Run the adversarial suite below against production.
  - Run the drills: admin disable/offboarding (A5–A6), kill switch and scale to zero (A14), key rotation (A24), image rollback, duplicate requests, and uncertain-charge recovery.
  - Scan the logs for canaries (A18).
  - Review usage, latency, limits, and any explicitly approved duplicate-input measurement.
  - Get an independent security review of the server diff and infrastructure, in addition to Greptile.
- **Exit.** All acceptance rows marked L are recorded; limits are adjusted from pilot data; Drew decides to widen access.

### Batch 7: rollout and migration (2–4 days, spread over weeks)

- **Outcome.** Everyone at Atomic is on sessions, and the legacy credentials are retired. Sequence:
  1. A reviewed release ships the client. After pilot approval, widen eligibility and prepare rollout instructions for Drew to share. Direct keys still take precedence, and the bundle still works during the migration window.
  2. Advance pins in Atomic repositories to the session-aware version (`rzm update --set-version` per repository; the repository owners do this). Use the `client_version` field in the gateway logs to see who has moved.
  3. After at least 2 stable weeks, an explicit inventory of remaining direct-key/pinned users, and a successful rollback drill: the authorized provider operator rotates previously distributed keys and retires the shared credential entries. Stop bundling keys, remove `pkg/teamkeys` and obsolete prompt/build paths in that same retirement change, and release it through the normal gate. Keep current releases supported during migration without introducing a new compatibility layer.
  4. Update [secrets.md](../../engineering/secrets.md), [RELEASING.md](../../RELEASING.md), and credential/build guidance in the retirement change. This migration does not remove S3 release distribution merely because credential bundling is removed. Keep the release-location policy independent.
- **Exit.** A25, plus the provider dashboards show no traffic on the retired keys.

### Batch 8 (after the MVP, gated): team embedding cache (5–8 days if approved)

- **Gate.** Build it only if the pilot shows a meaningful `dup_inputs` rate *across machines* and a latency or rate-limit benefit. First compare against the existing local `RHIZOME_EMBEDDING_CACHE`, which [already caches vectors in a local SQLite file](../../../pkg/embeddings/provider_helpers.go). Pointed at one user-global path, it may already capture most repeats, because worktrees of the same repository are the common duplicate source.
- **Namespace.** Derived on the server from the authenticated person. Per-person by default, so nothing crosses people. A cross-person scope must be explicitly registered on the server with a member list; a client-supplied namespace is never trusted.
- **Key.** `SHA-256(cache_schema_version ‖ namespace ‖ provider ‖ model ‖ operator-set model epoch ‖ input_type ‖ output_dimension ‖ output_dtype ‖ encoding_format ‖ truncation ‖ exact input)`. Bump the epoch when the provider changes model behavior behind an alias.
- **Behavior.** Batch lookup; only misses go upstream; the caller's order is restored; writes come only from upstream responses on the server, so clients can never upload vectors; in-flight misses are deduplicated with a per-key singleflight in the single task; there is no hash-lookup endpoint.
- **Economics and confidentiality.** A 1,024-dimension float32 vector alone is 4,096 bytes; metadata increases the billed item size, and DynamoDB rounds writes in 1 KB increments. Measure actual item sizes, repeat rate, storage, requests, and transfer before claiming a saving ([DynamoDB pricing](https://aws.amazon.com/dynamodb/pricing/)). Vectors remain derived client data. Choose retention with the data owner; define deletion and revocation for personal and shared scopes rather than assuming a shared partition belongs to one departing person.

### Critical path

Batch 0 setup/approval → Batch 1 foundation review → Batches 2 and 3 → integrated Batch 5 → Batch 6 pilot → Batch 7. Batch 4 follows the client foundation in Batch 3 and must finish before Batch 6. Infrastructure preparation in Batch 5 may overlap local work, but deployed integration waits for a tested server and client. Initial hands-on estimate: **about 25–45 engineer-days**, plus external lead time and 1–2 weeks of pilot. This is a planning range, not a measured delivery forecast; re-estimate after Batch 1, especially native stores, request accounting, and deployment proof. The smallest useful checkpoint is Batch 1, not completion of the whole range.

## Security and adversarial tests (Batches 2, 3, and 6)

- Token attacks: a forged token, a truncated token, another session's ID with a valid secret, an expired access token, refresh after revocation, refresh reuse after the grace window, and an access token presented to the refresh endpoint and vice versa.
- Login attacks: a mismatched `state`, a second request to the loopback listener, a missing PKCE verifier or wrong verifier, a code replayed twice, and an ID token whose `aud` belongs to another client (from the fake Google).
- Verify signatures and nonce as well as claims. A late browser callback, login cancellation, keychain save failure, or concurrent logout cannot create a usable stale session. If local publication fails after server session creation, revoke the new session or report incomplete cleanup explicitly.
- Eligibility: a non-org account, an unaccepted `hd`, an unverified email, a disabled person, and allowlist mode.
- Redirection: a repository endpoint pointing at an attacker, dotenv `HTTPS_PROXY`/certificate/path overrides, forged metadata, origin changes, a 30x upstream, and proxy settings in the server environment. Use synthetic files and a controlled proxy; never read actual credential files.
- Abuse: oversized bodies, 1,001 inputs, a disallowed model, unknown routes and methods, path traversal in provider paths, and slow-loris uploads (server read timeouts).
- Limits: concurrency floods from one person and from many, and budget races in which 100 concurrent requests reserve near the limit.
- Billing failures: crash before/after reservation and dispatch; repeated receipt/settlement; lost successful response; unknown upstream result; day rollover; invalid actual usage; unavailable state store. Prove that retries cannot duplicate paid dispatch or create budget credit.
- Leakage: canary text and canary keys in bodies and in upstream error echoes, followed by a scan of the logs, the error envelopes, and the client error strings.
- Operations: kill switch during load, scale to zero during load, key rotation during load, and image rollback.

Synthetic versus live: most adversarial checks run in CI/local fixtures. The matrix explicitly identifies remaining live/native/deployed checks, including A1, A4–A6, A11–A15, A18, A22, A24–A25, and A30. They use synthetic content, run under explicit live/deployment authorization, and never execute automatically from an untrusted pull request.

## Rollout and rollback

| Stage | Rollback |
| --- | --- |
| Batch 5 synthetic stack | Delete or ignore it; no users are involved. |
| Pilot (allowlist) | Use the tested stop-and-replace procedure to return to the previous digest. Explicitly choose direct mode or clear a session if using still-supported direct credentials during migration; the service never silently changes billing. |
| Team release (`all`) | Close new admissions and roll back the image through the same single-serving-task procedure. If direct access is needed during the migration window, users explicitly select it. A broken client release is handled by supported pins/release procedures, preserving the session store. |
| After retirement | Old keys stay dead, and are not redistributed. Image rollback and fix-forward cover service faults. During an extended outage, semantic indexing and Jev degrade: lexical search keeps working, and individuals may use their own direct keys for client work. This is why retirement waits for 2 stable weeks and a rollback drill. |
| Provider key compromise | Rotate the provider key, update Secrets Manager, force a new deployment. No client change. |
| Session compromise | `session revoke` or `revoke-all <email>`; the budget bounds the damage in the meantime. |

Pinned and old binaries: binaries older than the session release work only through the bundle or direct keys, until Batch 7 stage 3. After that they fail with a provider 401, and the remedy is advancing the repository pin. The desktop app bundles its own `rzm`, so its updater delivers the session support. Mixed versions on one machine share the store format, which is versioned with additive changes only.

## External prerequisites

| ID | Prerequisite | Who | Blocks |
| --- | --- | --- | --- |
| X1 | Refresh AWS CLI access. Choose a dedicated account or the release account. Verify that effective permissions (SCPs, explicit denies) and Fargate, ALB, and DynamoDB quotas allow the stack. | Drew (with Atomic IT if accounts are vended) | 5 |
| X2 | A Google Cloud project under Atomic's organization, Internal audience, a Desktop OAuth client, and controlled test accounts. | Google Workspace or Cloud admin (Atomic IT), with Drew | 1, 6 |
| X3 | The accepted Workspace domain list for `hd`. | Atomic IT | 1 |
| X4 | A DNS name and its zone owner; the ACM validation record. | Atomic IT | 5 |
| X5 | New dedicated gateway keys for Voyage and TypeSafe, with spend caps, plus confirmation that the provider terms allow one org key to serve all Atomic staff through a proxy. | Provider account owner (Drew) | 6 |
| X6 | The private GitHub repository, the `production` environment, and its approvers. | Drew or a GitHub org admin | 2, 5 |
| X7 | Provider pricing, maximum billable-request rules, supported models/options, hard-cap availability, and gateway-key rate limits. Finalize conservative admission/charge bounds. | Drew, with the providers | 6 |
| X8 | Sign-off on quotas, 30-day absolute/14-day idle session limits, the support owner, and a tested gateway-disablement step in offboarding. | Drew and Atomic IT | 6 |
| X9 | The list of Atomic repositories and owners whose pins must advance. | Drew and the repository owners | 7 |

An Apple Developer ID is **not** required. It is the upgrade path for a tighter keychain ACL.

## Open decisions for review

1. **Direct Google OIDC instead of Cognito.** This reverses the research note. If Atomic has an organizational reason to prefer Cognito (for example, other apps sharing the pool), Batch 1 must add a hosted handoff design and its security review.
2. **The keychain item is readable by any process running as that user** (through `security`). This is equivalent to the threat model the research note already accepts ("A malicious process already running as the OS user remains able to attempt legitimate API use"). Per-person revocation and budgets bound the damage.
3. **The server goes in a new private repository**, while the client and contract stay in this public repository.
4. **Offboarding ownership.** No Google refresh tokens are retained. The baseline requires an owned gateway-disablement action; automatic Workspace reconciliation is a separate requirement if Atomic needs it.
5. **Single serving task.** The service accepts measured stop-and-replace downtime to keep concurrency enforcement local. It does not accept doubled limits during deployment. Add distributed admission before introducing rolling overlap or multiple serving tasks.
6. **Separate pre-existing finding:** direct provider keys are sent to repository-configured endpoints today. Track it as a separate hardening task; it is outside this plan's scope.
