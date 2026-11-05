---
type: EffortNote
id: EFF-2026-11-05-17-40
name: Desktop Homebrew cask
created-at: 2026-11-05T22:44:28Z
status: complete
summary: Publish a desktop Homebrew cask with the app's bundled CLI while preserving the existing CLI-only formula.
aliases:
  - EFF-2026-11-05-17-40
governing-specs:
  - "[[desktop-distribution-and-updates]]"
  - "[[evidence-grounded-release-orchestration]]"
---

# Desktop Homebrew cask

## Scope

Add the desktop cask the user accepted after PR #94's release-flow review. The user also asked whether to retire the old CLI recipe. Preserve that existing CLI-only path for Linux, Intel Macs and headless use; prefer the Desktop cask on supported Macs and link the app's bundled CLI. This is a separate change based on PR #94, without reopening its closed effort. Do not publish a release, write the live tap, retire the formula or request signing access.

## Spec Set (Frozen)

- [[desktop-distribution-and-updates|SPEC-0122]], as extended on 2026-11-05 with Homebrew distribution requirements.
- [[evidence-grounded-release-orchestration|SPEC-0078]], as extended on 2026-11-05 with two-recipe Homebrew recovery.

## Stories In Scope (Frozen)

- [[desktop-distribution-and-updates#Homebrew distribution|SPEC-0122 Homebrew distribution requirements]]: cask, bundled CLI link, platform floor and retained formula.
- [[evidence-grounded-release-orchestration#Requirements|SPEC-0078 publication/recovery requirements]]: validated recipe hashes, safe creation, idempotence and gateway-gated partial recovery.

## Spec Coverage Checklist

- [x] Cask describes the published notarized DMG, Apple Silicon/macOS minimum and bundled CLI link without another build.
- [x] Publisher creates or updates both recipes using verified release hashes, with existing credential boundaries.
- [x] Missing-file detection distinguishes proven absence from API/auth failures; partial updates and completed-release recovery remain safe.
- [x] Desktop is preferred on supported Macs; CLI-only users retain their formula and migration guidance prevents accidental executable replacement.
- [x] Generated cask is parsed by Homebrew and checked against the existing accepted native artifacts; focused/CI tests and review cover publication changes.

## Plan

The user's acceptance of adding a cask authorizes the bounded publisher/documentation work. Retiring the CLI formula was exploratory, so it is retained as the compatible option; no deletion is authorized. Current-user metadata remains unconfigured and no approver identity is fabricated.

1. Extend the existing distribution/release contracts and record this new effort. Keep PR #94 unchanged; branch from its reviewed head and target that branch until it merges.
2. Render Casks/rhizome-desktop.rb using the verified release version/DMG hash, arm64 and actual minimum macOS, auto-updates, app installation and Homebrew's binary link to the bundled rzm. Update both tap recipes with SHA checks, proving absence before first creation. Preserve gateway-before-recovery-write and token scope.
3. Test initial creation, both-current no-op, API failures, partial formula/cask failure and retry. Parse the generated cask with local Homebrew and verify its app/binary targets against PR #94's accepted notarized artifact. No live tap write or real release is an acceptance test.
4. Reconcile installation/release docs, run relevant gates, inspect the diff, obtain independent/Greptile review, close this effort and open a separate PR. Native build output is unchanged, so reuse the accepted candidate instead of spending another Mac runner.

## Original Intended Delivery

Desktop installation through Homebrew installs both the app and its matching CLI, and each approved release updates that cask together with the existing CLI-only formula.

## Actual Delivered

Implemented in [PR #97](https://github.com/atomicobject/rhizome/pull/97), stacked on PR #94. The public publisher now maintains both the CLI formula and Casks/rhizome-desktop.rb from verified release hashes. The cask installs the notarized app and links its bundled rzm through Homebrew, with Apple Silicon/macOS12 requirements and built-in updates. The original CLI formula remains supported for Linux, Intel Macs and headless use.

Both-recipe no-op checks, complete-tree absence detection, conditional SHA writes and gateway-gated partial recovery are implemented. README and maintainer guidance prefer the cask on supported Macs and explain migration from the CLI-only formula. No extra Mac build, signing access, live tap write or product publication was performed.

All 400 release tests, the 20 focused publisher tests, repository content tests, documentation/frozen-scope validation and file-size/whitespace checks passed. Independent review found no actionable issues. Homebrew7's real content loader accepted both a synthetic cask and a cask generated from the accepted notarized DMG checksum; its declared app/CLI target and macOS minimum match that artifact. An actual brew install from a public release URL remains a first-release check because no such release was authorized here. Greptile and ordinary PR CI remain merge gates.

## Execution Notes

- 2026-11-05T22:51:16Z [verification] Full release suite passed 400 tests; focused publisher suite passed 20. Repository content tests, file-size/whitespace checks and default/frozen-scope validation passed. Independent review reported no actionable findings.
- 2026-11-05T22:51:16Z [verification] Homebrew7's public info command rejects standalone cask paths because it requires a tap. Loaded the generated source through the installed Homebrew CaskLoader instead, without installing an app or changing any tap. The loader resolved version, checksum, URL, auto-updates, arm64/macOS12 requirements, Rhizome.app and the bundled-rzm binary artifact correctly.
- 2026-11-05T22:51:16Z [verification] Generated a second cask using candidate26b9fa78's real DMG checksum beab1a36408256e8bd0f308986af74fd235e3affb9e50f1900488878f6599faf. Homebrew loaded it; the accepted app contains Contents/MacOS/rzm and declares LSMinimumSystemVersion12.0. The build output is unchanged, so no Mac runner or signing-secret access was repeated.
- 2026-11-05T22:51:16Z [closure] Closure drift pack resolved this EffortNote, its plan and frozen requirement links with no query errors. Scope is implemented and independently verified. The separate PR will carry this closure; CI/Greptile and merge approval retain their normal gates.
- 2026-11-05T22:44:28Z [decision] Actual accepted candidate Info.plist declares macOS 12.0 and contains Rhizome.app/Contents/MacOS/rzm. The cask reuses those existing notarized bytes.
- 2026-11-05T22:44:28Z [decision] The live tap contains only rhizome.rb. Homebrew's documented binary artifact can link a CLI inside the installed app. The CLI-only formula stays available; cask installation must not overwrite an existing CLI link.
- 2026-11-05T22:44:28Z [scope] Frozen spec indexes were read. Any active/planned frozen drift must be reconciled by validation; closed efforts remain historical.

## Deviations

None. Keeping the existing CLI-only formula preserves platform and headless support while making Desktop the default supported-Mac path.

## Compounding Follow-ups

No delivery-blocking follow-ups. The maintainer guide records safe recipe creation, partial recovery and the distinction between cask-managed and direct-DMG CLI links.

## Closure Checklist

- [x] Publisher and focused failure/recovery tests complete.
- [x] Homebrew parsing and candidate artifact target checks passed.
- [x] Docs/specs and default/frozen validation reconciled.
- [x] Independent review, applicable checks and delivery PR recorded.

## Status

Complete implementation and local acceptance on the separate cask branch. PR review/CI and merge approval remain required; no public release or live tap write is implied.
