# Team API gateway

`team-api` lets Atomic Object staff use Voyage embeddings and Jev evaluation without holding a provider key. A person signs in once per OS user with their Workspace Google account. Every Rhizome process on that account then sends paid calls to the gateway, which adds the provider key and enforces per-person and global limits. [SPEC-0129](../../docs/specs/technical/team-api-gateway.md) is the contract; [`pkg/teamapi/CONTEXT.md`](../../pkg/teamapi/CONTEXT.md) covers the client side.

This is a nested Go module with its own `go.mod` and no vendor directory, so its OIDC and SQLite dependencies stay out of `rzm`. It imports the shared wire contract (`pkg/teamapi/wire`) and the diagnostics recorder from the root module through a local `replace`.

## Run it locally

```sh
make team-api-dev        # gateway + development sign-in + fake providers on 127.0.0.1:8788
make team-api-dev-live   # the same with the real providers, using the development keys from 1Password
make team-api-dev-reset  # delete the local state (.team-api-dev/)
```

Then, from any repository:

```sh
rzm auth login --server http://127.0.0.1:8788
rzm auth status
```

Sign-in opens the **development sign-in page** instead of Google. The local stack runs its own OIDC issuer at `/dev/idp`, with discovery, keys, PKCE, and signed ID tokens, so the gateway checks a local sign-in with the same code it uses for Google. The page offers synthetic identities:

| Identity | Email | Result |
| --- | --- | --- |
| `member` | dev-member@atomicobject.com | Signs in |
| `admin` | dev-admin@atomicobject.com | Signs in and can use `rzm auth admin` |
| `outsider` | outsider@example.com | Rejected: no Workspace domain |
| `unverified` | dev-unverified@atomicobject.com | Rejected: email not verified |
| `wrongdomain` | someone@example.org | Rejected: domain not accepted |

The fake providers return deterministic vectors and valid Jev answers. Markers in request text inject faults: `__fault:slow:<ms>__`, `__fault:status:<code>__`, `__fault:drop__`, `__fault:retry_after:<s>__`. `GET /dev/upstream/voyage/_stats` and `/dev/upstream/typesafe/_stats` count what each fake received. Use the fake stack only with throwaway vaults: fake vectors in a real index would make its search wrong.

## Development builds use the local gateway

A development build of `rzm` is any build not made by the release tooling, such as `make build`. Release tooling sets `version.Channel=release`. A development build behaves like this:

- `rzm auth login` with no `--server` targets `http://127.0.0.1:8787` and starts the gateway there if nothing answers.
- Once signed in, that session wins over direct provider keys.
- `make build` puts `team-api` beside `rzm`.
- The auto-started gateway always uses the **real** providers, reads the development keys from the `rhizome-development` login-keychain items (run `./scripts/with-secrets sync-keychain` once), keeps its state in `~/.config/rhizome/team-api-dev`, and exits after an hour without requests.
- It refuses to use a gateway with fake providers, such as a leftover `make team-api-dev` on the same port.

Release builds keep the normal order: a direct key, then the team session, then the legacy bundle. CI and tests are unaffected because they have no stored session; `go test` binaries never read the person's session.

## Admin commands

Admins are named in the server configuration (`dev-admin@atomicobject.com` locally). Every change is audited, and the configuration caps the limits the API can set.

```sh
rzm auth admin usage [--date YYYY-MM-DD]
rzm auth admin people
rzm auth admin person <sub> --status disabled
rzm auth admin revoke-all <sub>
rzm auth admin config --paid-calls off
rzm auth admin audit
```

## Diagnostics

Local runs write diagnostics through `pkg/diagnostics` under the data directory: `<data-dir>/.rhizome/diagnostics/events/*.jsonl`. They are separate from any vault's diagnostics and carry no tokens, request bodies, or email addresses.

| Event | What it records |
| --- | --- |
| `teamapi.request` | One per HTTP request, including rejection and health: safe route pattern, identity, status, bytes, duration, and authentication time |
| `teamapi.provider_request` | One per paid call: route, model, sizes, upstream status, gateway code, and timings for validation, admission wait, reservation, upstream, settlement, and total |
| `auth.signin`, `auth.refresh`, `auth.logout` | Outcome and duration |
| Admin events | Each admin change |

The log of an auto-started gateway is `~/.config/rhizome/team-api-dev/team-api.log`. Client-side refresh, lock waits, and gateway errors are recorded in the calling vault's diagnostics.

## Tests

```sh
make team-api-check          # go vet and race-enabled tests; SQLite by default
make team-api-dynamodb-check # starts disposable DynamoDB Local, then runs all tests
# Or use an already running Local instance:
TEAMAPI_DYNAMODB_ENDPOINT=http://127.0.0.1:8000 make team-api-dynamodb-check
scripts/team-api/e2e.sh      # real rzm and team-api: sign in, index, search, Jev, admin, diagnostics
TEAM_API_E2E_UPSTREAMS=live scripts/team-api/e2e.sh   # the same against the real providers
```

`cmd/team-api/stack_test.go` runs the whole stack in-process with the real client packages (`pkg/teamapi/login` and `session`). The e2e script needs `NO_WEB=1 make build` first. It writes a synthetic session for its own origin to the login keychain and removes it on exit.

## Deploying

`team-api serve --config <file>` is the deployed form:
- A production configuration accepts only Google's issuer and the fixed provider URLs, and refuses to start otherwise.
- `VOYAGE_API_KEY` and `TYPESAFE_API_KEY` come from the environment (Secrets Manager in ECS).
- `RHIZOME_TEAM_API_CLIENT_SECRET` optionally holds the Google Desktop client secret.

The ARM64 container, CloudFormation template, protected OIDC workflow, and operations instructions are in [RUNBOOK.md](RUNBOOK.md). The infrastructure uses ECS Express Mode on its stable AWS hostname, one steady-state task, DynamoDB, and a 30-day CloudWatch log group. All account configuration stays in AWS or the protected GitHub `production` environment.

The image builds with Go 1.27.3 and runs the pinned `govulncheck` 1.9.0 against the compiled binary before packaging it. A finding or unavailable vulnerability database fails the build. CI builds the image without cached layers to exercise this gate. ECR does not support scanning the final `scratch` image, and the Go scan does not cover statically linked C dependencies such as SQLite or musl.

`team-api serve` also accepts the full JSON configuration in `RHIZOME_TEAM_API_CONFIG`, injected from SSM by ECS. An explicit `--config` takes precedence. Environment configuration defaults diagnostics to JSON lines on stdout; set `diagnostics_output: "stdout"` explicitly in production. Provider keys and the Google client secret are separate ECS secret injections from Secrets Manager. `trusted_proxy_cidrs` identifies the ALB subnet CIDRs; only the final ALB-appended address is trusted, and task ingress must allow only the ALB security group. `/healthz` becomes ready after store initialization and recovery, checks no providers, and reports unavailable during shutdown.

`session_lookup_limits` bounds the session-store reads that unrecognized tokens can cause. Each authentication cache miss, logout, and refresh takes a token from a bucket for the client address and then from a process-wide bucket; an empty bucket returns `rate_limited` with `Retry-After` before the store is read. Cache hits are free, so a session's steady traffic costs at most one lookup per 30 seconds plus one after each refresh. The defaults below suit a few hundred people behind one office address; raise the per-address values if a larger office shares one address.

```json
"session_lookup_limits": {"per_ip_burst": 300, "per_ip_per_second": 20, "global_burst": 1000, "global_per_second": 100}
```

The TypeSafe model list is cached for 60 seconds, concurrent misses share one upstream request, and each person may make 10 requests at once, refilling one every 6 seconds. Errors are never cached.

Select DynamoDB for `serve` by adding this to the production configuration. The table must already exist with string keys `pk` (partition) and `sk` (sort). The gateway never provisions AWS resources.

```json
"state": {
  "backend": "dynamodb",
  "table": "team-api-state",
  "region": "us-east-1"
}
```

Credentials come from the AWS SDK v2 default chain, normally the ECS task role. Omit `state` to keep SQLite. `team-api dev` always uses SQLite and its existing data directory. DynamoDB uses `data_dir` for diagnostics when file output is selected. Production configuration has no endpoint override.

## State and accounting

Identity uses `store.Store`; paid accounting uses `ledger.Ledger`. Each exposes atomic business operations, with explicit state and ownership preconditions. Identity and traffic accept those interfaces; `cmd/team-api` selects and owns the backends. SQLite writes use `BEGIN IMMEDIATE`, including migration, so two ledger instances on one database serialize through SQLite itself.

DynamoDB uses one table, no secondary indexes, and no TTL. Each item has a JSON `body` and an opaque `rev`. Strong reads plus revision conditions in `TransactWriteItems` protect every read-modify-write. Transactions check the complete state used for admission, including the owner lease, the receipt, and both counters. Conflicts retry with bounded jitter and a 20-second deadline; ambiguous transport errors escape conservatively. A transaction token handles SDK retries; durable receipts provide long-lived request idempotency.

| Partition key `pk` | Sort key `sk` | State |
| --- | --- | --- |
| `people` | Google subject | Person and session revocation generation |
| `sessions` | Session ID | Token hashes, generation, expiries, and revocation generation at creation |
| `revocations#<subject>` | Generation number | Immutable bulk revocation time and reason |
| `config` | `runtime` | Runtime switches and limits |
| `audit-sequence` | `current` | Commit order for the audit log |
| `audit` | Zero-padded sequence number | Audit entry, committed with its mutation |
| `owners` | Random process instance ID | Lease expiry and permanent recovery fence |
| `receipts` | `<base64url(subject)>:<idempotency key>` | Admission day, reservation, owner, and outcome |
| `day#YYYY-MM-DD` | `global` or `person#<subject>` | Daily usage counters |

The shared table keeps provisioning and transactions simple for the pilot. Collection queries walk every page. Listing sessions filters the session partition by person. Recovery reads the owners and receipts partitions, then rechecks unfinished receipts before changing them. These reads grow with retained state; measure them before expanding traffic or retention. The global counter intentionally serializes admission. Admin usage reads are observations during traffic, not a transaction snapshot across all people.

Bulk session revocation advances the person's generation with an immutable revocation record and its audit entry in one transaction. Every session write also changes the person's revision, so bulk revocation counts a stable set of sessions and cannot miss a concurrent creation. A session from an older generation uses its first subsequent revocation record. This preserves the first reason and time, avoids the 100-item transaction limit, and keeps sessions created after revocation active. Rotation checks the session generation and revocation state and preserves the supplied predecessor deadline. The auth layer still enforces its fixed 120-second grace.

### Canary recovery

Each process creates a fresh random instance ID and a 60-second lease. The gateway heartbeats every 20 seconds, keeps heartbeating during shutdown draining, and independently runs recovery at startup and every 20 seconds. Heartbeat failure stops admission and drains HTTP work. An expired owner cannot renew its lease; a fresh process must use a new ID. Task clocks must track UTC.

Every receipt records its owner. Admission and all receipt transitions require a live lease. Dispatch and settlement also require that owner to match. Recovery permanently fences an expired owner before converting its reserved or dispatched receipts to unknown, atomically adding the unknown count to both admission-day counters. A heartbeat racing that fence compares the same revision, so only one wins. Another task's unexpired lease protects its requests throughout canary overlap.

A late settlement after expiry or recovery returns `ledger.ErrPrecondition` and keeps the reservation. Concurrent recovery cannot count the same receipt twice. A crash during recovery is safe to retry; a crash after an uncertain commit also keeps the reservation. Even receipts that had not yet dispatched become unknown after their owner expires. Ownerless receipts in an older SQLite database migrate to this conservative recovery path. Receipts and owner fences are retained indefinitely. A quick restart can leave the old receipt in progress until its lease expires and a recovery pass finishes.

The pilot still uses process-local concurrency slots, so old and new tasks may briefly double the slot limits during canary deployments. Shared daily counters and reservations stay atomic. The deployment package covers CloudFormation, IAM, the protected workflow, and the runbook. Store-backed slots before rollout and live AWS verification remain in Batch 5 and later work.

### DynamoDB Local tests

`TEAMAPI_DYNAMODB_ENDPOINT` enables the DynamoDB contract and auth integration tests. Without it, those cases skip and `make team-api-check` needs no Docker. The test helper accepts only loopback HTTP endpoints, constructs a synthetic credential provider directly, and never loads the default AWS credential chain. Each case creates and deletes a uniquely named Local table. The helper tests bulk revocation beyond 100 sessions and across query pages, concurrent rotations and partial updates, audit rollback, two-instance budget races, idempotency, canary overlap, lease expiry, and late settlement.

`make team-api-dynamodb-check` starts an in-memory `amazon/dynamodb-local:3.1.0` container on a random loopback port when no endpoint is provided, waits for it, runs vet and all race tests with verbose output, and stops the container on exit. Start Docker Desktop first if needed. The CI `Team API Gateway` job runs the default checks and then the Local checks against its service container.

DynamoDB transaction limits and retry semantics follow the [AWS transaction documentation](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/transaction-apis.html).
