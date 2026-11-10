# Integrated review evidence

[AE integrated review fixes recheck](t3-thread://v2/b6cc208d-de4b-4b15-a36f-c28b251e92c7/thread:delegated-task:command%3Amcp%3A6e466306-8b4e-47d7-b21f-fded108c08d1%3Adelegate-task%3Aae-integrated-sol-review-v2)

VERDICT APPROVE

S1, S2, and S3 are resolved in the reviewed source. No new actionable findings in the scoped delta.

- **S1 resolved.** `lib/prompts.ts:25` requires complete selected-record coverage from one graph generation. `stage.tsx:74–90` additionally blocks Copy during pending, failed, stale, or incomplete selected-effort reads and preserves the reason in the preview.
- **S2 resolved.** `stage.tsx:47–66` retains selected record snapshots and passes every selected identity to `promptFor`. Lines 203–230 announce unavailable selections and require explicit reduction or clearing. Select-all is disabled while unavailable selections remain at line 162.
- **S3 resolved.** `evidence.tsx:27–68` preserves Markdown versus wikilink syntax and the containing source path in resolver requests and cache keys. Only returned canonical refs enable internal navigation; failures and unresolved fragments remain visible. Lines 78–95 keep Markdown components stable. The new kit exports directly reuse existing implementations.

The source resolver distinguishes authored Markdown from wikilink syntax, so preserving that distinction in requests is necessary. I also inspected the reachable spec/effort callers and the new regression assertions against SPEC-0137.

I ran no builds or tests and made no changes. Integrated gates remain pending; compounding retrieval failed authentication.

