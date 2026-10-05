---
name: vault-core-subsystem
description: Use when implementing, modifying, or reviewing code under pkg/vault/obsidian, pkg/paths, pkg/vault/config, pkg/vault/frontmatter, or pkg/vault/ignore. Loads vault-core path/config/link constraints and review checklist.
---

# Vault core subsystem

## Goal

Keep changes to vault primitives (paths, config, links, tags, frontmatter, ignore) aligned with the subsystem's invariants so absolute or malformed paths never reach indexes and config/link behavior stays consistent across OSes.

## First move

Read `docs/reference/subsystems/vault-core.md` first. Its design constraints and review checklist are normative for this subsystem — treat them as operational contracts, not background.

## Load-bearing rules

1. All path normalization goes through `pkg/paths` (`VaultPaths`, `Normalize`/`NormalizeNote`/`NormalizeCode`). No ad hoc `filepath.Abs`/`filepath.Rel` or OS-specific path logic elsewhere.
2. Indexes store only vault-root-relative, forward-slash, cleaned paths. `AbsPath` is for filesystem I/O only — never persisted, never a key.
3. Use `VaultPaths.RelStrict`/`RelNoteStrict`/`RelCodeStrict` for anything stored or compared as an index key; use `paths.AbsFromInputWithVaultPaths` / `ResolveNoteInputWithVaultPaths` / `ResolveCodeInputWithVaultPaths` for user input. Pass `PathRef` across subsystem boundaries.
4. Legacy helpers (`obsidian.NormalizePath`, `AddMdSuffix`) are compat-only — forbidden in new code. Current config flows through `LoadLocalConfig`/`FindLocalConfig` + `LocalConfigToDefinition`; legacy `agent:` config is migrated on read, never written.
5. Relative config paths resolve against the config file's directory, never cwd.
6. New `LocalConfig` fields require defaults/normalizers plus `pkg/app/cli/init` detection/migration so `rzm init` round-trips them.
7. Link parsing/resolution changes go through `ScanWikilinks`/`ScanAllLinks` + `NotePathCache`, with table-driven tests for code fences, embeds, aliases, and heading/block fragments.
8. All file exclusion goes through `ignore.LoadUnifiedMatcher`; do not re-implement glob filtering.

## Pre-handoff checklist

- [ ] No new `filepath.Abs`/`Rel` or deprecated helper calls in touched code (grep the diff).
- [ ] Stored/compared paths use a `Rel*Strict` variant; no `AbsPath` in storage-bound values.
- [ ] Config field changes mirrored in `pkg/app/cli/init` migration and defaults.
- [ ] Edge-case tests added beside the change (link syntax, path inputs, config round-trip).
- [ ] `go test ./pkg/paths/... ./pkg/vault/...` passes; run `go test -race -tags=integration ./...` for link-parsing or discovery changes.
- [ ] Touched package `CONTEXT.md` and `docs/reference/subsystems/vault-core.md` updated if an invariant moved.
