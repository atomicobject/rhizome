# Team API client

`pkg/teamapi` is Rhizome's side of the Atomic team API gateway ([SPEC-0129](../../docs/specs/technical/team-api-gateway.md)). `wire` is the stdlib-only v1 contract shared with the server in `services/team-api`. `session` keeps one OS user's session per gateway origin and authorizes requests to it. `login` runs the browser sign-in and publishes its grant through `session`. None of these import `pkg/app`, `pkg/vault`, SQLite, or CGO; `pkg/diagnostics` and the standard library are the only other dependencies, and the packages build with `CGO_ENABLED=0`.

## Trusted origin

- An origin is `scheme://host[:port]`: HTTPS, or plain HTTP only on `127.0.0.1`, `localhost`, or `[::1]`. It has no path other than `/`, and no query, user information, or fragment. `CanonicalOrigin` lower-cases it and drops a default port; the canonical form keys the store, the selector, and the transport's origin check.
- Only an interactive `rzm auth` command chooses an origin. `ResolveOrigin` takes, in order: `--server`; in development builds (version `dev`) only, `RHIZOME_TEAM_API_ORIGIN`, accepted only when that origin already has a stored session; the selector file; then `DevelopmentOrigin` in development builds or the compiled-in `DefaultOrigin` (set with `-X` in internal releases, empty in public ones). Repository configuration, dotenv files, and tool input never take part.
- The selector `<home>/.config/rhizome/team-session.json` (`{"v":1,"origin":...}`) names the origin of the last sign-in. It holds no secret. `Publish` writes it under the lock after the generation check; `Logout` removes it only if it still names the origin being signed out. `CurrentOrigin` reads it for consumers such as the provider factory.
- `Account.Transport` sends only to the session origin (exact scheme, host, and port), adds the bearer token and `User-Agent`, and refuses every redirect, in the transport itself so a caller's client cannot follow one. The base transport has `Proxy: nil` because `main` loads repository dotenv into the environment before dispatch; trusted proxy configuration is a marked upgrade path.
- `rzm auth` runs on the invoked executable: `main.go` skips dotenv for it, `pkg/app/repoexec` keeps it off repository delegation and pin bootstrap, and the CLI does not open vault diagnostics for it.

## Local store format and versioning

- `Store` is `Load`, `Save`, and `Delete` of an opaque record per origin. `Keychain` (macOS) writes a login-keychain generic password through `/usr/bin/security`, with the record on standard input and a 10-second deadline per call, since a locked keychain in a GUI session blocks on an unlock dialog. `DefaultStore` returns `ErrStoreUnavailable` on other platforms. There is no plaintext fallback.
- The record is JSON with `v` (currently 1), `origin`, `generation`, `session_id`, `email`, `access_token`, `access_expires_at`, `refresh_token`, `session_expires_at`, and `limits`. A client never overwrites or deletes a record whose `v` it does not understand (`ErrNewerRecord`). A change an older client must not drop on rewrite bumps `v`.
- `generation` identifies the sign-in. Publication increments it and refresh keeps it. A login captures it before opening the browser and publishes only if it is unchanged (`ErrSuperseded`), revoking its own unpublished grant otherwise. A sign-in-ending gateway error deletes the record only at the generation that sent the rejected token. Sign-out resets the account to generation 0, so a login that began signed out can still publish after another process signs in and out again; this ABA case is accepted and marked in code.
- Store and lock locations come from the real OS account (`os/user`), never `HOME` or a repository-supplied `Env`. Tests inject a store and paths through `Options`; there is no production override.

## Refresh coordination

- Requests read the record without the lock and use the access token while it stays valid for more than 60 seconds. Otherwise the account takes `<home>/.config/rhizome/team-session.lock` (`flock`, 30-second timeout; Windows has a stub until it has a store), rereads the record, and refreshes only if no other process already did. Twenty processes refreshing at once rotate once.
- Before rotating, the account rewrites the current record to prove the store accepts writes. If it does not, it keeps using the current token until it expires and then returns `ErrStoreUnavailable`, because rotation retires the stored refresh token and the gateway revokes the session when another process reuses it after the grace period. If the save after a rotation fails twice, this process keeps the new record in memory, records `session.save_failed`, and writes it before its next rotation.
- `access_refresh_required` means the gateway rejected an older access token before admission. The transport refreshes only if the stored token is the rejected one, then replays the request once using `GetBody`; without `GetBody` it returns the error. It never clears a healthy shared session for this code.
- `Logout` holds the lock, revokes the session on the gateway, and deletes the record, so a refresh waiting for the lock finds nothing to write back. Offline, it deletes the record and reports that remote revocation is incomplete.

## No fallback billing

A gateway error is returned as `*wire.GatewayError`, with `RetryAfter` from `Retry-After`; `errors.Is(err, wire.ErrSignInRequired)` marks codes that end the session. This package never retries a paid request beyond the one pre-admission replay and never switches to another credential. Callers that choose between direct keys, the session, and the legacy bundle do so before construction, and a session failure stays a failure.

## Diagnostics

With a `diagnostics.Recorder` in the context, the account records `session.refresh` (outcome, duration, lock wait), `session.save_failed`, `gateway.replay`, and `gateway.error` (code, status, route) under subsystem `teamapi`. Events never carry tokens, records, or request bodies.
