# Credentials and 1Password

Secrets belong in 1Password, private user configuration, or protected GitHub Actions secrets. Never commit live credentials, unlock keys, encrypted credential bundles, private release locations, or copies in test fixtures. Never print values or include them in agent context.

## Developer setup

Ordinary contributors need no provider credentials to build, test, or use local Ollama embeddings. Optional cloud providers resolve keys from the environment first, then the user-global `~/.config/rhizome/config.yml`.

Atomic Object developers use dedicated shared Rhizome provider keys in a 1Password Environment. Install stable `op` 2.41 or newer, sign in to an account with read access, and enable **Settings > Developer > Integrate with 1Password CLI** in the 1Password app. In Rhizome setup or Settings, use **Import provider keys from 1Password**. Select Voyage and TypeSafe, enter the account and shared Environment ID supplied by the team, then approve the 1Password prompt. The CLI supports the same import:

```sh
rzm credentials import --from 1password --account example.1password.com \
  --environment <environment-id> --key VOYAGE_API_KEY --key TYPESAFE_API_KEY
```

The Environment ID is available in **Developer > View Environments > View environment > Manage environment > Copy environment ID**. Missing requested keys fail the import; existing shell values cannot substitute for missing Environment values. The desktop can find `op` in the process PATH, `~/.local/bin`, `/opt/homebrew/bin`, or `/usr/local/bin`.

Existing shared items can still be imported using explicit field references:

```sh
rzm credentials import --from 1password --account example.1password.com \
  --key 'VOYAGE_API_KEY=op://Shared/Rhizome/VOYAGE_API_KEY' \
  --key 'TYPESAFE_API_KEY=op://Shared/Rhizome/TYPESAFE_API_KEY'
```

Replace the non-secret references and account with those supplied by the team. Imports validate the source, read all requested keys before saving, preserve unrelated settings, and write the global config atomically with mode `0600`. This is plaintext user-local storage. Environment source references are remembered on this computer for re-import; they never enter repository configuration. Normal execution needs no `op` process. Repeat import after rotation; environment variables still take precedence.

An import times out after 90 seconds. Cancel stops its credential process tree and discards a pending result. Cancellation cannot undo an atomic save that already completed; the desktop refreshes provider status after cancellation or failure. An abrupt app crash can leave an import running until its timeout and may allow it to finish before then. An older Rhizome version may discard the saved source references when it rewrites user configuration; enter them again to re-import.

The shared Environment or item and provider administration belong to the team, outside the repository. Use provider spend limits and separately scoped keys where available. Removing 1Password access does not revoke an already imported provider key; rotate the provider keys when access must end. Do not put tenant IDs, account defaults, live values, or exported secret files in Git.

## Development and release tooling

`scripts/with-secrets` remains optional tooling for development and release work. Its Environment-based workflow requires 1Password CLI 2.41.0 or newer on PATH (or `RZM_OP_BIN`), an explicit `RZM_OP_ENVIRONMENT_ID`, and `OP_ACCOUNT`. Keep release credentials (GitHub publishing tokens and the desktop updater key) in a separate Environment named by `RZM_OP_RELEASE_ENVIRONMENT_ID`, so development runs, including coding agents, never unlock them; without it, `release` falls back to `RZM_OP_ENVIRONMENT_ID`. To stop approving every development run, run `./scripts/with-secrets sync-keychain` once (and again after rotating a key). After one 1Password approval it copies `VOYAGE_API_KEY` and `TYPESAFE_API_KEY` into the login keychain (service `rhizome-development`), passing them to `security` on standard input so no value appears in arguments or output; an agent can run it without seeing the keys. `with-secrets development` then reads them from the keychain without a prompt and falls back to 1Password when either is missing. Any process running as the user can read those two items while the keychain is unlocked, which is acceptable only for revocable API keys. Release credentials are never copied; `with-secrets release` asks 1Password every time. This is separate from ordinary setup-time import and is not required for Rhizome users.

The wrapper filters inherited credentials. Development commands receive only the required provider keys. Release work also uses dedicated GitHub and Homebrew tokens; `RZM_RELEASE_GITHUB_TOKEN` becomes `GITHUB_TOKEN` only in the publishing subprocess. Build subprocesses must not receive provider, 1Password, or AWS credentials. AWS access is used only by the internal mirror publish step.

Public builds and releases never bundle provider keys or private mirror configuration. The temporary internal bridge may remain in public source (approved 2026-09-29), with internal releases continuing from the private archive until a replacement is ready. They bundle rotated Voyage and TypeSafe keys, AES-GCM encrypted under the existing `ATOMIC_RHIZOME_KEY`, so current Atomic Object users keep working without setup. The tracked `pkg/teamkeys/keys_encrypted.go` stays empty; `scripts/teamkeys/release.py` supplies the bundle through a temporary compiler overlay only when `RZM_INTERNAL_S3_BUCKET` is set and only for a verified private GitHub repository. Public builds reject inherited overlays, disable persisted Go settings, remove stale mirror configuration, and scan each built binary and staged installer before upload. Everyone who has held the unlock key can decrypt the internal bundle, including former holders; keep provider spend caps on these keys. See [internal releases](../RELEASING.md#internal-releases).

## Tests and GitHub

Ordinary tests use synthetic fixtures and no live keys. The optional TypeSafe smoke test requires both `-tags live` and `TYPESAFE_LIVE_TEST=1`, so an ambient key cannot enable it. Its manual workflow uses the protected `live-tests` environment on trusted branches. Pull-request workflows must never receive live secrets.

## Commit protection and incident response

Run `mise install` to install gitleaks and `make hooks-setup` to enable commit checks. The pre-commit hook and `make credential-check` scan the Git index; stage intended changes before checking. They also block staged files that contain any value of 8 or more characters from a local `.env` (in the worktree or main checkout) or from a credential-named environment variable, naming the file and variable but never the value. CI also scans new commits after the documented historical incident boundary. Reports must redact values. Do not bypass checks or whitelist real credentials.

History through `961e3b41feb9e082985357acb710f7c6ab243b54` is retained by instruction. That boundary does not certify history as safe to publish. Revoke superseded or exposed historically bundled provider keys (Voyage, TypeSafe, OpenAI, Cerebras). The current internal keys and unlock key are intentionally retained until the bridge is replaced; rotate the provider keys and retire the unlock key then. Removing a bundle or rewriting Git history cannot revoke a credential. Update the shared item, repeat setup import, and restart processes with the new values.

The historical repository is retained as a private archive. This clean repository's source is public. Never merge archive history into this repository. Source publication does not authorize a release or prove that public installation works. The [open-source preparation specification](../specs/technical/open-source-preparation.md) records the source and artifact requirements.
