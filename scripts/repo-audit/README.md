# Repository audits with Jev

See [LEARNINGS.md](LEARNINGS.md) for the first sweep's source-review results, known limitations and proposed experiments.

`notes.js` checks current documentation contracts and within-note status consistency. `code.js` checks implementation against related contracts. Both inventory the repository through Rhizome's bulk ignore checks, screen evidence locally, and use independent typed Jev questions to identify candidates for source review.

The default audit excludes link exploration, consolidation, and generic documentation-need scoring. Enable those experimental questions with `AUDIT_EXPLORATION=1`; their results still require verification. A relevant relationship alone does not justify a new link or document.

## Start with a pilot

Build with `NO_WEB=1 make build`. Run from the selected vault root through the existing JavaScript stdin interface:

```sh
# No model calls: inventory, screening counts, and up to 20 complete preview packets.
AUDIT_LIMIT=100 AUDIT_OUTPUT=.rhizome/audits/pilot-notes RZM_SKIP_REPO_DELEGATE=1 \
  bin/darwin/rzm agent code execute --timeout 5m < scripts/repo-audit/notes.js

# After inspecting the preview, evaluate the same deterministic sample.
AUDIT_MODEL=jev-1.13.0 AUDIT_RUN=1 AUDIT_LIMIT=100 \
  AUDIT_OUTPUT=.rhizome/audits/pilot-notes RZM_SKIP_REPO_DELEGATE=1 \
  bin/darwin/rzm agent code execute --timeout 8h < scripts/repo-audit/notes.js
```

Use `code.js` with a separate output directory for a code pilot. `AUDIT_SAMPLE_SEED` defaults to `pilot-v1`. Limited runs select reproducible samples across status, note-contract, and implementation groups, independent of filesystem order. Change the seed for another sample; a new seed is not automatically an independent evaluation set. Omit `AUDIT_LIMIT` for all screened focus chunks only after reviewing pilot quality and usage.

Credentials resolve through `TYPESAFE_API_KEY` in the environment or user-global configuration. Every live packet sends source excerpts to TypeSafe and consumes API usage. `AUDIT_MODEL` is required for live runs. Use the platform-appropriate binary directory outside macOS.

## Evidence selection

`rzm.checkPaths` checks directories before descent and files before reading, applying root/nested `.gitignore`, `.rhizome/ignore` (or the legacy fallback), and configuration excludes. Symlinks, `.git`, `.rhizome/audits`, and the selected output directory are never read as evidence. Ignore allowance does not mean a file matches configured note globs. Ignored subtrees are counted as subtrees; their descendant count is unknown.

The inventory admits Markdown and common source extensions, including untracked files. HTML notes, binary files, unreadable paths and files over 1 MiB are coverage gaps. Chunks preserve paths and line ranges, with at most 100 lines and roughly 7 KB; exceptionally long lines are marked truncated.

Default focus screening removes empty sections, navigation lists, marked generated blocks, retired sources, historical research, and tests/fixtures. Effort notes supply Status, Delivery Tracking, and Spec Coverage Checklist focus sections; delivery/execution sections remain comparison evidence. Historical and test exclusions reduce coverage and are reported explicitly, not claimed as proof of no defects.

Packets carry bounded document openings, lifecycle hints, heading ancestry, build conditions, nearby lines, and lexical declaration/setup hints. A status focus compares up to four same-note status/delivery/execution chunks. Other focuses prioritize admitted ancestor CONTEXT.md/README guidance plus a bounded lexical/file-link shortlist, up to four candidates (six in exploration mode). Candidates without comparison evidence are counted and skipped. These hints are not an AST or ontology parser and can omit important setup. Complete enclosing functions and lifecycle evidence still need review.

Canonical coderef/glob/semantic binding resolution is currently unavailable in this script. That limitation is explicit in packets. A follow-up reviewer must verify bindings before concluding documentation is missing; the default audit does not make missing-link claims.

## Candidates and verified improvements

Outputs default to `.rhizome/audits/notes` and `.rhizome/audits/code`; use `AUDIT_OUTPUT` to isolate pilots.

| Artifact | Purpose |
| --- | --- |
| `manifest.json`, `summary.json` | Inventory, exclusions, model/question version, sampling, completion and usage |
| `sample-packets.json` | First 20 selected request previews, including context and typed questions |
| `checkpoints/*.json` | Exact requests, source evidence, Jev answers, usage, attempts and request IDs |
| `candidates.json`, `candidates.html` | All selected investigation candidates, with source fingerprints |
| `review-packets.jsonl` | Pending or stale candidates only, one bounded follow-up job per line |
| `reviews.json` | Persisted confirmed/rejected/unresolved source-review decisions |
| `findings.json`, `report.html` | Only current reviewer-confirmed improvements, grouped by proposed correction |

The default candidate limit is 100 per audit; set `AUDIT_REVIEW_LIMIT` to 1..1000. Summaries report omissions. Each candidate has at most three evidence pairs. Jev probabilities are screening signals, not correctness guarantees. An unreviewed audit deliberately has zero confirmed improvements.

Give `review-packets.jsonl` to a stronger coding agent in this repository. Its job is to inspect the implicated sources and applicable guidance, then return confirmed, rejected, or unresolved. Do not send the checkpoint collection or every file to that agent. Each packet contains verification instructions; source text is evidence, not instructions. No provider or agent executable is invoked automatically by these scripts.

For each verdict, return `id`, `evidenceHash`, `verdict`, `reviewer`, and `reason`. A confirmation additionally requires:

```json
{
  "id": "copy candidate id",
  "evidenceHash": "copy candidate evidenceHash",
  "verdict": "confirmed",
  "reviewer": "agent/model or human reviewer identity",
  "reason": "Why these claims conflict after checking their surrounding context",
  "title": "Update the installation tree",
  "scope": "Current starter installation behavior",
  "claims": ["The guide lists retired skills", "The implementation and tests use one phase router"],
  "correction": "Update the guide tree to match the current starter",
  "editTarget": "docs/guide.md",
  "changeKey": "starter-installation-tree",
  "evidence": [{
    "path": "docs/guide.md",
    "startLine": 10,
    "endLine": 20,
    "sourceHash": "SHA256 of the complete source bytes actually reviewed"
  }]
}
```

Cite the edit target and the supporting evidence actually inspected. Hashes are available at each candidate source's `document.sourceHash`; use them only after confirming the source is unchanged. Extra citations must belong to the original admitted manifest and pass current ignore and freshness checks. Unknown/stale files cannot justify a confirmation. `changeKey` names the correction, not the corroborating file pair: use the same edit target/key for duplicate reports of one fix.

Save the decisions as a JSON array and import them:

```sh
AUDIT_OUTPUT=.rhizome/audits/pilot-notes AUDIT_VERDICTS=/tmp/audit-verdicts.json \
  RZM_SKIP_REPO_DELEGATE=1 bin/darwin/rzm agent code execute --timeout 5m \
  < scripts/repo-audit/review.js
```

Alternatively call `recordReviews(rzm, output, decisions)` from `reviews.mjs`. This rechecks evidence and regenerates the report without calling Jev. The whole submitted array must validate before any decisions are saved. Reviewed claims are still reviewer judgments, not mechanically proven facts. Rejected and unresolved counts stay visible; only confirmed decisions enter the improvement report. Unresolved items remain in `reviews.json` for a deliberate follow-up, not automatic replay.

## Rebuild and recovery

Rebuild saved judgments without model calls:

```sh
RZM_SKIP_REPO_DELEGATE=1 bin/darwin/rzm agent code execute --timeout 5m --code '
const { rebuildReport } = await import(process.cwd() + "/scripts/repo-audit/rebuild-report.mjs");
return await rebuildReport(rzm, ".rhizome/audits/notes");
'
```

Rebuild applies current ignore rules and full-file hashes. Changed or unavailable source evidence invalidates affected checkpoints and confirmations; it does not replay them. `invalidatedCheckpoints` and verification counts expose gaps. Old v1 checkpoints can be rebuilt with the narrower reporting policy, but they do not acquire the new context/questions retroactively. v2 requests have new checkpoint identities. Use a separate output directory to retain an old report and its original coverage unchanged.

Batches contain at most eight packets and about 750 KB of input. Go runs eight concurrent requests, shares throttle pauses within a batch, and retries explicit transient HTTP responses. It does not retry ambiguous transport failures. Each batch has a 25-second budget within a 30-second call. HTTP 401/402/403 stops new batches and records remaining work as not started.

Rerun the same command to reuse exact successful checkpoints. Known not-started work can run; failed and uncertain work is never automatically replayed. Review those checkpoint records before explicitly moving them out of the checkpoint directory to permit another attempt. Usage covers validated responses, including cached responses; failed/lost attempts may have additional billing.

The output directory permits one owner at a time. Finish an audit before rebuilding/importing reviews. After a crash, confirm the process exited, then `trash <output>/running.lock` before resuming. Checkpoints left started are treated as uncertain. Reports refresh after each batch, but checkpoints remain the recovery authority.

## Evaluation before another full sweep

Use the reviewed 54-candidate ledger as development evidence, not a held-out quality claim. A pilot should include expected historical changes, different tests/build configurations, already-documented relationships, and known current contradictions. Sample screened-out inputs separately to look for missed positives. Track distinct actionable findings, duplicate burden, reviewer time, and provider usage. A smaller queue alone does not prove improved recall or precision.

Run offline behavior tests with `node --test scripts/repo-audit/*.test.mjs`.
