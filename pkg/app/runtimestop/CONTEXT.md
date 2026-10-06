## Stopping vault runtimes

Owns stopping a vault's runtime (SPEC-0104). `cmd/stop.go` is a thin adapter for `rzm stop [--all]`, and the desktop companion's Stop operation (`pkg/app/desktop/stop.go`) calls `StopVault` after checking that the runtime belongs to its folder. The package depends only on `pkg/app/runtime`, so the companion does not link the server.

- **Entry points**: `Stop(ctx, StopOptions)` is `rzm stop [--all]` and reports each vault's outcome to its writer; `StopVault(ctx, vaultPath, grace)` stops one vault and returns why it did not stop.
- **Stop discovery**: the global registry lists elected owners and pending detached children. Stop cancels the observed token, so recovery blocked on another writer can exit before publication; it does not cancel a successor's different token. It waits within its grace period for a live owner or spawn lease to resolve and reports incomplete shutdown if ownership remains. It never guesses the mode or kills an unpublished owner. A published but unresponsive headless owner gets the full grace period before force termination; an attached owner is never force-killed.
