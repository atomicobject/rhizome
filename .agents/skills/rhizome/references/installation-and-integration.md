# Installation and integration

Use this route when Rhizome is missing, the project lacks a launcher, the promised agent experience may not actually be active, or setup looks wrong (code not indexed, search off, skills missing).

## Install with mise as the project owner

Use this path when the project already uses mise and should have no global `rzm`, checked-in Rhizome launcher, or second Rhizome-managed version pin. Add an exact version to `mise.toml` with mise's built-in HTTP backend. Pin the release you intend to use; releases are listed at https://github.com/atomicobject/rhizome/releases.

```toml
[tools]
"http:rhizome" = {
  version = "<X.Y.Z>",
  url = 'https://github.com/atomicobject/rhizome/releases/download/v{{version}}/rhizome-{{os(macos="darwin")}}-{{arch(x64="amd64")}}.tar.gz',
  checksum_url = 'https://github.com/atomicobject/rhizome/releases/download/v{{version}}/checksums.txt',
}
```

Generate and commit `mise.lock` so mise records Rhizome's published checksum for every supported platform. Then install and initialize through mise while recording provider-neutral external ownership in Rhizome:

```bash
mise lock
mise install --locked
mise exec -- rzm --version
mise exec -- rzm init --binary-manager external
```

`checksum_url` is resolved by `mise lock`; plain `mise install` downloads the configured archive but does not validate it against Rhizome's published checksum manifest. Verify `.rhizome/config.yml` contains `rhizome.binaryManager: external` and does not contain `rhizome.version` or authored binary path fields. `external` does not identify mise; it tells Rhizome to trust whichever external tool selected the executable. Do not expect `bin/rzm` or `.rhizome/bin`: the executable resolved by `mise exec` runs directly. To upgrade, edit the exact `mise.toml` version, rerun `mise lock` and `mise install --locked`, and verify again. `rzm update` and the project installer intentionally refuse this mode; update through the external manager.

## Install into a project

Download the current installer from the repository's `main` branch to a local file so it can be run explicitly. Optionally inspect the downloaded file before running it:

```bash
curl -fsSLo install-rzm.sh https://raw.githubusercontent.com/atomicobject/rhizome/main/scripts/install/install-rzm.sh
```

For agent-driven project setup, keep user-scope mutation off unless the user separately authorizes it:

```bash
bash install-rzm.sh \
  --project <project-root> \
  --user-binary skip \
  --yes \
  --json
```

`--project` is explicit and may differ from the current directory. `--launcher` must remain inside that project and defaults to `bin/rzm`. With no `--version`, the installer preserves an existing project pin and chooses the latest release only when no pin exists. Use `--version <vX.Y.Z>` only for an intentional pin change.

`--json` requires `--yes` so machine-readable installs cannot prompt. It emits one stdout object; treat progress on stderr separately. Read its resolved project, launcher, selected pin, `latestRelease`, `pinSelectionSource`, `pinRelationToLatest`, user-scope mutation, structural status/reason, repair object, and `next` argv array instead of reconstructing them. The structural status checks only the presence and shape of the managed instruction block and consolidated `rhizome` skill. `present` is not a runtime integration claim; it routes to the minimal agent-session verification.

`--user-binary auto|install|skip` is a separate scope decision. Agents should use `skip`; humans may choose `auto` or `install` when they want bare `rzm` outside the project launcher. User-only installation remains available through `bash install-rzm.sh --user ...`.

This installer path is for Rhizome-managed projects. If `.rhizome/config.yml` declares `rhizome.binaryManager: external`, project installation stops before launcher, config, or binary mutation. User-only installation remains independent of repository ownership.

## Initialize only with consent

The installer does not run `init`. For `init-required` or `incomplete`, `next` is intentionally empty because the installer cannot prove that the selected release can generate the consolidated skill. Explain `agentSurfaceStructureReason`; if initialization is not already authorized, ask the user before executing `agentSurfaceRepair.initCandidate` as an argv array. The installer already chose the arguments the pinned release accepts. When that release supports `init --check` (its `init --help` lists the flag), run the check first and show the user the change list, because `init` makes every change the check lists. Re-check the structure afterward.

When `pinRelationToLatest` is `different`, a preserved or explicitly selected older pin may not support the consolidated skill. Do not silently advance it. The repair object provides an exact `advertisedLatestPinChangeCandidate`; execute it only after the user intentionally accepts a pin change and you have confirmed that the advertised release supports the surface. A `same` relation is not a capability guarantee: the advertised latest release may still predate the installer guidance. If consented `init` leaves the structure incomplete, select a release known to contain the consolidated skill using an explicit `--version`, or wait for such a release. Never claim that a version supports the surface solely because it equals `latestRelease`.

Do not infer consent from installation alone. When the user already authorized initialization or the specific repair, continue its covered steps without another approval request. Without a terminal, `init` applies the change list, keeps every file someone edited, and lists the kept files; only an interactive run offers to update them. When you pass `--agents`, leave out agents the user turned off unless they ask to restore them.

## Verify the integrated experience

First identify the ownership mode from `.rhizome/config.yml`, then check the matching layers.

For `rhizome.binaryManager: external`:

1. The external manager's invocation executes and reports the intended version; for mise, run `mise exec -- rzm --version`.
2. `.rhizome/config.yml` resolves for the target project, contains `rhizome.binaryManager: external`, and does not contain `rhizome.version` or authored binary path fields.
3. The active agent instruction file contains the `rzm init` managed Rhizome block.
4. The active harness has the managed `rhizome` skill.
5. A minimal session succeeds through the external manager; for mise, run `mise exec -- rzm agent start --intent "verify Rhizome integration"`.

For Rhizome-managed ownership:

1. The project launcher exists, executes, and reports the pinned version.
2. `.rhizome/config.yml` resolves for the target project and contains the project pin.
3. The active agent instruction file contains the `rzm init` managed Rhizome block.
4. The active harness has the managed `rhizome` skill.
5. A minimal project-launcher `agent start --intent "verify Rhizome integration"` succeeds.

Report runtime integration as `ready` only after the relevant ownership-mode layers work together, including the minimal session. For the Rhizome-managed installer path, structural status uses `present` when the managed block and skill have the expected shape, `init-required` when both are absent and initialization still needs user consent, and `incomplete` when only part of the structural surface exists. Say whether the problem is a missing skill, missing launcher or externally selected CLI, uninitialized project, unresolved configuration, failed session, or unavailable capability. Do not imitate an integrated experience with generic commands.

## Troubleshoot setup

Diagnose with `rzm init --check`, `rzm index --status`, and `rzm index --explain`, which write nothing. Run `rzm init` only with the user's consent, after showing them the change list from `rzm init --check`, because `rzm init` makes every change that list names.

| Symptom | Route |
| --- | --- |
| Code is not indexed, or code was added after setup | `rzm index --explain <file>` names the setting that keeps it out; `rzm init --check` shows the fix, often under Suggestions (removing folder limits); with consent, run `rzm init --accept-suggestions`, then `rzm index` |
| A file is missing from search | `rzm index --explain <path>`, then fix the ignore rules or what gets indexed (`references/index-scope.md`) |
| Semantic search is off or a key is missing | `rzm index --status` (read `Enabled`); have the user set the key, or run `rzm init --search <provider>` |
| Agent guidance or skills are missing | `rzm init --check`, then `rzm init`; `--agents` changes which agents get files |
| An edited skill keeps its old version | Expected; an interactive `rzm init` offers the update |
| Search is slow, noisy, or full of third-party or generated results | `rzm init --check` for proposed skips; otherwise `references/index-scope.md` |
| `rzm init --check` lists Suggestions | They change what gets indexed or turn on a feature, so show them to the user; with consent, `rzm init --accept-suggestions` applies them along with the changes |

## When something is broken

Name the failed layer before choosing a fix. A missing skill may be guidance someone turned off on purpose. Re-enable it only with consent, by running `init --agents <agents>` through the launcher and naming every agent this repository uses (`claude`, `codex`, `cursor`). A failed session gets one minimal `agent start --intent ...` retry, not a loop on a stale id. A stale or absent index goes through `index --status` and `index --explain` before any rebuild. A degraded capability shows up in `agent surface` and response warning fields; use a narrower supported route and say what confidence that costs. Retry or repair only for a concrete cause; repeated restarts and rebuilds hide an unresolved failure instead of fixing it.

If the problem remains, report the smallest reproducing command, the launcher version and pin, the project root, the relevant config path, any capability or warning fields, and what was already ruled out. Redact tokens and provider credentials. Keep doing ordinary work that does not depend on the broken layer, and say what evidence that leaves out.

Global Rhizome requires explicit checkout trust before delegating to a repo-selected binary or probing its version. A fresh untrusted checkout fails in a noninteractive harness. Have the user review the checkout and run `rzm trust` interactively; `rzm untrust` revokes the local record. Direct execution of a checked-in launcher or binary already runs repository code.
