# Integrated review evidence

[AE complete integrated implementation review](t3-thread://v2/b6cc208d-de4b-4b15-a36f-c28b251e92c7/thread:delegated-task:command%3Amcp%3A6e466306-8b4e-47d7-b21f-fded108c08d1%3Adelegate-task%3Aae-integrated-sol-review-v1)

VERDICT CHANGES

The Delivery experience has three reachable issues affecting exact scope and navigation.

1. **S1 · should · [stage.tsx:81](<pkg/app/cli/init/templates/starters/agentic-engineering/module/views/delivery/stage.tsx:81>)**  
   Copy remains enabled when selected efforts span different graph generations. `selectedEfforts()` detects this through combined coverage, but the stage only displays that failure. `promptFor()` validates each effort independently. A write between sequential effort reads can therefore produce a copyable prompt from inconsistent evidence.  
   **Minimal fix:** Require complete combined selected-effort coverage before enabling Copy. Preserve the corresponding reason in the preview.  
   **Independent proof:** A narrow Node assertion confirmed that a group containing generations 1 and 2 reports incomplete coverage while its composed prompt reports `copyable: true`.

2. **S2 · should · [stage.tsx:65](<pkg/app/cli/init/templates/starters/agentic-engineering/module/views/delivery/stage.tsx:65>)**  
   Refresh silently changes selected scope. Select ready stories A and B, then refresh after B becomes satisfied. The render filters B out before calling `promptFor()`, and the effect permanently removes it. The prompt becomes copyable for A without announcing the removal. This also defeats `promptFor()`’s existing changed-selection check. US2 explicitly requires reporting removed or changed eligibility before copying.  
   **Minimal fix:** Retain and announce removed selections, and require acknowledgement or reselection before copying the reduced scope.  
   **Evidence:** Traced the selection filtering, cleanup effect, and prompt caller. No browser reproduction run.

3. **S3 · should · [evidence.tsx:9](<pkg/app/cli/init/templates/starters/agentic-engineering/module/views/delivery/evidence.tsx:9>)**  
   Inline source content bypasses Rhizome’s Markdown link handling. Wikilinks remain plain text, and ordinary relative Markdown links resolve against the view iframe URL rather than the source note. This affects criteria, execution notes, and other narratives rendered through `SourceContent`.  
   **Minimal fix:** Reuse the existing source-aware link conversion and canonical navigation behavior used by the native narrative reader.  
   **Independent proof:** The installed Markdown dependency rendered `[plan](../plan.html)` as an unchanged relative anchor. URL resolution placed it under `/views/...`, rather than the effort’s source directory.

Coverage included the supplied spec and foundation contract, domain rules, adapters, page and panel callers, exports, registrations, recipe/schema changes, public ref handling, fixtures, and lifecycle test sources, including the untracked source/test files.

Review ran in-thread on `codex/gpt-6.1-sol`, high. No files changed or subagents started.

I did not rerun builds, broad gates, installer tests, or browser acceptance, and did not independently inspect screenshots. Pending gates are not findings. Compounding retrieval remained unavailable because authentication failed.

