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

The gateway writes diagnostics through `pkg/diagnostics` under its data directory: `<data-dir>/.rhizome/diagnostics/events/*.jsonl`. They are separate from any vault's diagnostics and carry no tokens, request bodies, or email addresses.

| Event | What it records |
| --- | --- |
| `teamapi.provider_request` | One per paid call: route, model, sizes, upstream status, gateway code, and timings for validation, admission wait, reservation, upstream, settlement, and total |
| `auth.signin`, `auth.refresh`, `auth.logout` | Outcome and duration |
| Admin events | Each admin change |

The log of an auto-started gateway is `~/.config/rhizome/team-api-dev/team-api.log`. Client-side refresh, lock waits, and gateway errors are recorded in the calling vault's diagnostics.

## Tests

```sh
make team-api-check          # go vet and race-enabled tests for this module
scripts/team-api/e2e.sh      # real rzm and team-api: sign in, index, search, Jev, admin, diagnostics
TEAM_API_E2E_UPSTREAMS=live scripts/team-api/e2e.sh   # the same against the real providers
```

`cmd/team-api/stack_test.go` runs the whole stack in-process with the real client packages (`pkg/teamapi/login` and `session`). The e2e script needs `NO_WEB=1 make build` first. It writes a synthetic session for its own origin to the login keychain and removes it on exit.

## Deploying

`team-api serve --config <file>` is the deployed form:
- A production configuration accepts only Google's issuer and the fixed provider URLs, and refuses to start otherwise.
- `VOYAGE_API_KEY` and `TYPESAFE_API_KEY` come from the environment (Secrets Manager in ECS).
- `RHIZOME_TEAM_API_CLIENT_SECRET` optionally holds the Google Desktop client secret.

The deployed store (DynamoDB), CloudFormation, and the runbook are Batch 5 of the [build plan](../../docs/reference/analysis/rhizome-team-api-service-build-plan.md). Until then the server keeps its state in SQLite.
