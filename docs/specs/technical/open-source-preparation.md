---
type: TechnicalSpec
id: SPEC-0107
aliases:
  - SPEC-0107
summary: "Remove private release dependencies and bundled internal content; establish explicit checkout trust and a resumable public migration."
spec-status: active
last-updated: 2026-09-29
---

# Open Source Preparation

## Summary

Prepare Rhizome's code and developer setup for a future public repository. This contract implements the decisions approved in the September 22 migration discussion. Publication and historical content review remain separate work.

## Goals

- Builds and ordinary use require no Atomic Object secrets or private infrastructure.
- Atomic Object developers can import provider keys once using the stable 1Password CLI.
- Repository-selected executables require an explicit, locally recorded trust decision.
- Releases and installation use GitHub Releases and Homebrew.
- The project knowledge-base starter works without the internal consulting library.

## Non-Goals

History rewriting, repository renaming, changing visibility, publishing a release, altering existing downloads, rotating live keys, deleting historical effort or meeting notes, or restricting trusted HTML notes to a per-note asset list.

## Requirements

1. Public binaries and release artifacts MUST NOT bundle provider keys. The shared unlock-key mechanism and internal release tooling MAY remain temporarily, with all credentials, encrypted bundles, and private artifact locations outside Git. Environment overrides and manually entered credentials MUST continue working.
2. Setup-time import MUST use explicit 1Password item references, read all requested values before a single atomic global-config update, preserve unrelated configuration, restrict file permissions, and avoid printing credentials or provider output on failure. Repeating import MUST support rotation. Normal execution MUST NOT require 1Password.
3. A repository-selected executable, including a version probe, MUST require explicit trust for that canonical checkout. Trust MUST live outside repository-controlled configuration. Noninteractive execution MUST fail with an actionable trust command. Users MUST be able to grant and revoke trust without first running repository code.
4. The application HTTP handler MUST reject untrusted Host values before API or mutation routes. The isolated HTML viewer and configured application origins MUST retain their intended behavior. Existing message nonce checks MUST remain intact.
5. Public installation, update discovery, and release automation MUST use GitHub Releases and the existing Homebrew tap. Public artifacts MUST exclude private S3 locations, legacy URL tokens, AWS release credentials, and encrypted key bundles. Internal publication MAY continue from the private archive until a replacement is ready. Historical S3 objects remain untouched.
6. The `project-kb` starter and its internal AO reference library MUST be removed from source, binary, and available init choices. Existing installed project files MUST not be deleted implicitly. Repeat initialization of supported starters MUST remain idempotent.
7. Contributor setup MUST declare the required secret scanner and CI MUST use explicit least-privilege defaults. Community guidance MUST describe an AO-led project without support or response-time promises. Third-party attribution work and unresolved provenance MUST be recorded accurately.
Amendment (2026-09-29, superseding the September 28 removal deadline): internal releases, selected by a setting in the release 1Password Environment, may continue to bundle team keys and publish to a private S3 mirror after the source becomes public. The code may remain in the public tree, but internal publication MUST remain confined to the private archive. Public builds MUST reject inherited compiler overlays, disable persisted Go environment settings, remove private mirror settings, and check the built binaries and staged installer before upload. Internal releases MUST fail unless the target repository is verified private. Retire the bridge and unlock key when the replacement is ready. See [internal releases](../../RELEASING.md#internal-releases).

8. A durable migration guide MUST distinguish completed code preparation from remaining key rotation, private content review, licensing verification, archive/public-repository creation, and publication verification.

## Verification

Use synthetic credential fixtures, untrusted executable sentinels, browser-host request tests, mocked release responses and installer fixtures, project-starter scaffolding tests, required repository gates, and a built-binary initialization smoke test. Do not exercise live credentials or publish artifacts during preparation.
