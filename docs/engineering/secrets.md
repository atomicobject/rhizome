# Credentials and 1Password

Secrets belong in 1Password, private user configuration, or protected GitHub Actions secrets. Never commit live credentials, unlock keys, encrypted credential bundles, private release locations, or copies in test fixtures. Never print values or include them in agent context.

## Developer setup

Ordinary contributors need no provider credentials to build, test, or use local Ollama embeddings. Optional cloud providers resolve keys from the environment first, then the user-global `~/.config/rhizome/config.yml`.

Atomic Object developers use dedicated shared Rhizome provider keys in a 1Password item. Install the stable `op` CLI, enable desktop integration if desired, and authenticate an account with access to the item. Import explicit field references once:

```sh
rzm credentials import --from 1password --account example.1password.com \
  --key 'VOYAGE_API_KEY=op://Shared/Rhizome/VOYAGE_API_KEY' \
  --key 'TYPESAFE_API_KEY=op://Shared/Rhizome/TYPESAFE_API_KEY'
```

Replace the non-secret references and account with those supplied by the team. The command validates references, reads all requested fields before saving, preserves unrelated user settings, and writes the global config atomically with mode `0600`. This is plaintext user-local storage. It does not change repository configuration. Normal execution needs no `op` process. Repeat import after rotation; environment variables still take precedence.

The shared item and provider administration belong to the team, outside the repository. Use provider spend limits and separately scoped keys where available. Do not put tenant IDs, account defaults, live values, or exported secret files in Git.

## Development and release tooling

`scripts/with-secrets` remains optional tooling for development and release work. Its Environment-based workflow requires a CLI supporting `op run --environment`, an explicit `RZM_OP_ENVIRONMENT_ID`, and `OP_ACCOUNT`. This is separate from ordinary setup-time import and is not required for Rhizome users.

The wrapper filters inherited credentials. Development commands receive only the required provider keys. Release work also uses dedicated GitHub and Homebrew tokens; `RZM_RELEASE_GITHUB_TOKEN` becomes `GITHUB_TOKEN` only in the publishing subprocess. Build subprocesses must not receive provider, 1Password, or AWS credentials. AWS access is used only by the internal mirror publish step.

Public builds and releases never bundle provider keys or private mirror configuration. The temporary internal bridge may remain in public source (approved 2026-09-29), with internal releases continuing from the private archive until a replacement is ready. They bundle rotated Voyage and TypeSafe keys, AES-GCM encrypted under the existing `ATOMIC_RHIZOME_KEY`, so current Atomic Object users keep working without setup. The tracked `pkg/teamkeys/keys_encrypted.go` stays empty; `scripts/teamkeys/release.py` supplies the bundle through a temporary compiler overlay only when `RZM_INTERNAL_S3_BUCKET` is set and only for a verified private GitHub repository. Public builds reject inherited overlays, disable persisted Go settings, remove stale mirror configuration, and scan each built binary and staged installer before upload. Everyone who has held the unlock key can decrypt the internal bundle, including former holders; keep provider spend caps on these keys. See [internal releases](../RELEASING.md#internal-releases).

## Tests and GitHub

Ordinary tests use synthetic fixtures and no live keys. The optional TypeSafe smoke test requires both `-tags live` and `TYPESAFE_LIVE_TEST=1`, so an ambient key cannot enable it. Its manual workflow uses the protected `live-tests` environment on trusted branches. Pull-request workflows must never receive live secrets.

## Commit protection and incident response

Run `mise install` to install gitleaks and `make hooks-setup` to enable commit checks. The pre-commit hook and `make credential-check` scan the Git index; stage intended changes before checking. They also block staged files that contain any value of 8 or more characters from a local `.env` (in the worktree or main checkout) or from a credential-named environment variable, naming the file and variable but never the value. CI also scans new commits after the documented historical incident boundary. Reports must redact values. Do not bypass checks or whitelist real credentials.

History through `961e3b41feb9e082985357acb710f7c6ab243b54` is retained by instruction. That boundary does not certify history as safe to publish. Revoke superseded or exposed historically bundled provider keys (Voyage, TypeSafe, OpenAI, Cerebras). The current internal keys and unlock key are intentionally retained until the bridge is replaced; rotate the provider keys and retire the unlock key then. Removing a bundle or rewriting Git history cannot revoke a credential. Update the shared item, repeat setup import, and restart processes with the new values.

The historical repository is retained as a private archive. This clean repository remains private until publication is separately approved. Never merge archive history into this repository. The [open-source preparation specification](../specs/technical/open-source-preparation.md) records the source and artifact requirements.
