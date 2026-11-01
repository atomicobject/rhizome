import type {
  TaskRunDetail,
  TaskRunView,
  TasksLiveState,
  TasksReport,
  TaskTranscriptEvent,
} from "../../api/tasks";

// Synthetic task data shaped like the runtime routes, for tests and the
// visual check. Times are relative to `now`.

const MIN = 60_000;

const HOUR = 60 * MIN;

export const RUN_ID = "20261031T140212Z-3fa91c2e";

export const CODEX_RUN_ID = "20261030T090000Z-7c01d4aa";

function at(now: number, offset: number) {
  return new Date(now + offset).toISOString();
}

export function reportFixture(now = Date.now()): TasksReport {
  return {
    ref: "origin/main",
    commit: "1236ddc7e9a04b51c2f3",
    fetchedAt: at(now, -2 * HOUR),
    paused: false,
    stateDir: "/Users/dev/.local/state/rhizome/tasks/rhizome",
    tasks: [
      {
        id: "docs-drift",
        name: "Docs drift check",
        scope: "person",
        every: "1d",
        mode: "auto",
        modeSource: "repository",
        lastRun: { id: RUN_ID, task: "docs-drift", at: at(now, -3 * HOUR), outcome: "ready" },
        nextDue: at(now, 21 * HOUR),
        due: false,
        item: {
          id: "7",
          task: "docs-drift",
          kind: "task",
          state: "ready",
          created: at(now, -3 * HOUR),
          updated: at(now, -3 * HOUR),
          summary: "Three notes still reference the removed --legacy-index flag",
          branch: "rzm/docs-drift/2026-10-31",
          worktree: "/Users/dev/.local/state/rhizome/tasks/rhizome/worktrees/docs-drift-2026-10-31",
          run: RUN_ID,
        },
        tokensLast30Days: 212_480,
      },
      {
        id: "dependency-updates",
        name: "Dependency updates",
        scope: "team",
        every: "7d",
        mode: "notify",
        modeSource: "default",
        lastRun: {
          id: "20261024T080000Z-11aa22bb",
          task: "dependency-updates",
          at: at(now, -7 * 24 * HOUR - HOUR),
          outcome: "notified",
        },
        nextDue: at(now, -HOUR),
        due: true,
        item: {
          id: "9",
          task: "dependency-updates",
          kind: "task",
          state: "due",
          created: at(now, -HOUR),
          updated: at(now, -HOUR),
          summary: "Four Go modules have minor updates",
        },
      },
      {
        id: "flaky-tests",
        name: "Flaky test triage",
        scope: "person",
        every: "12h",
        mode: "auto",
        modeSource: "override",
        lastRun: { task: "flaky-tests", at: at(now, -5 * HOUR), outcome: "failed" },
        nextDue: at(now, 7 * HOUR),
        due: false,
        item: {
          id: "8",
          task: "flaky-tests",
          kind: "task",
          state: "needs-attention",
          created: at(now, -5 * HOUR),
          updated: at(now, -5 * HOUR),
          summary: "TestWatchHubDebounce failed 3 of the last 20 CI runs",
          reason: "check failed: go test exited 2",
        },
      },
      {
        id: "changelog",
        name: "Changelog draft",
        scope: "person",
        every: "1d",
        mode: "off",
        modeSource: "override",
        lastRun: { task: "changelog", at: at(now, -26 * HOUR), outcome: "not-needed" },
        nextDue: at(now, 9 * HOUR),
        due: false,
      },
    ],
  };
}

export function stateFixture(now = Date.now()): TasksLiveState {
  const report = reportFixture(now);

  return {
    paused: false,
    items: report.tasks.flatMap((task) => (task.item ? [task.item] : [])),
    recent: [{ id: RUN_ID, task: "docs-drift", at: at(now, -3 * HOUR), outcome: "ready" }],
  };
}

export function runsFixture(now = Date.now()): TaskRunView[] {
  const day = 24 * HOUR;

  return [
    {
      id: RUN_ID,
      task: "docs-drift",
      startedAt: at(now, -3 * HOUR - 4 * MIN - 28_000),
      at: at(now, -3 * HOUR),
      outcome: "ready",
      tokens: 48_210,
      item: "7",
      branch: "rzm/docs-drift/2026-10-31",
      commit: "1236ddc7e9a04b51c2f3",
      worktree: "/Users/dev/.local/state/rhizome/tasks/rhizome/worktrees/docs-drift-2026-10-31",
      changed: [
        "docs/reference/indexing.md",
        "docs/reference/guides/getting-started.md",
        "README.md",
      ],
      hasTranscript: true,
      hasCheckLog: true,
    },
    ...[1, 2].map((days) => ({
      id: `2026103${1 - days}T140000Z-0000000${days}`,
      task: "docs-drift",
      startedAt: at(now, -days * day - 3 * HOUR - 4_000),
      at: at(now, -days * day - 3 * HOUR),
      outcome: "not-needed",
      hasTranscript: false,
      hasCheckLog: true,
    })),
    {
      id: "20261028T140004Z-9b2d7e10",
      task: "docs-drift",
      startedAt: at(now, -3 * day - 3 * HOUR - 2 * MIN),
      at: at(now, -3 * day - 3 * HOUR),
      outcome: "no-changes",
      tokens: 21_904,
      hasTranscript: true,
      hasCheckLog: true,
    },
    {
      id: "20261027T140003Z-5e5e1f00",
      task: "docs-drift",
      startedAt: at(now, -4 * day - 3 * HOUR - MIN),
      at: at(now, -4 * day - 3 * HOUR),
      outcome: "failed",
      tokens: 3_118,
      message: "agent run failed: Claude Code exited 1 (rate limited)",
      hasTranscript: true,
      hasCheckLog: true,
    },
    {
      id: "20261026T140001Z-a0a0b1b1",
      task: "docs-drift",
      startedAt: at(now, -5 * day - 3 * HOUR - 6 * MIN),
      at: at(now, -5 * day - 3 * HOUR),
      outcome: "interrupted",
      hasTranscript: true,
      hasCheckLog: true,
    },
  ];
}

function event(
  id: number,
  start: number,
  seconds: number,
  fields: Omit<TaskTranscriptEvent, "id" | "sessionId" | "createdAt">,
  session = RUN_ID,
): TaskTranscriptEvent {
  return { id, sessionId: session, createdAt: at(start, seconds * 1000), ...fields };
}

const DIFF = `@@ -41,7 +41,7 @@ Rebuild the index after an upgrade:

 \`\`\`sh
-rzm index --legacy-index
+rzm index --rebuild
 \`\`\`

 The rebuild keeps embeddings that are still current.`;

/** Claude Code: thinking, Bash with Text output, Read, an Edit, a retry notice. */
export function claudeTranscript(start: number): TaskTranscriptEvent[] {
  return [
    event(1, start, 2, {
      type: "reasoning",
      content:
        "**Scoping the drift**\n\nThe check listed three notes that mention `--legacy-index`. I should confirm the flag is really gone from the CLI before editing anything, then update each note to the current command.",
      result: { itemId: "think-1", phase: "completed" },
    }),
    event(2, start, 4, {
      type: "assistant-text-delta",
      role: "assistant",
      content: "I'll confirm the flag was removed, then update the three notes the check found.",
      result: { itemId: "msg-1", phase: "completed" },
    }),
    event(3, start, 5, {
      type: "command-execution",
      toolName: "Bash",
      result: {
        itemId: "toolu_01",
        name: "Bash",
        command: "rg -n -- '--legacy-index' cmd pkg docs README.md",
        phase: "started",
        input: { command: "rg -n -- '--legacy-index' cmd pkg docs README.md" },
      },
    }),
    event(4, start, 6.4, {
      type: "command-execution",
      content:
        "docs/reference/indexing.md:44:rzm index --legacy-index\ndocs/reference/guides/getting-started.md:112:  rzm index --legacy-index\nREADME.md:87:`rzm index --legacy-index` rebuilds from scratch.",
      result: { itemId: "toolu_01", phase: "completed", status: "completed", exitCode: 0 },
    }),
    event(5, start, 8, {
      type: "tool-call",
      toolName: "Read",
      result: {
        itemId: "toolu_02",
        name: "Read",
        phase: "started",
        input: { file_path: "docs/reference/indexing.md", offset: 30, limit: 40 },
      },
    }),
    event(6, start, 8.3, {
      type: "tool-call",
      content: "30→## Rebuilding\n31→\n32→Rebuild the index after an upgrade:\n…",
      result: { itemId: "toolu_02", phase: "completed", status: "completed" },
    }),
    event(7, start, 11, {
      type: "file-change",
      toolName: "Edit",
      result: {
        itemId: "toolu_03",
        name: "Edit",
        phase: "started",
        paths: ["docs/reference/indexing.md"],
        input: {
          file_path: "docs/reference/indexing.md",
          old_string: "rzm index --legacy-index",
          new_string: "rzm index --rebuild",
        },
        diff: DIFF,
      },
    }),
    event(8, start, 11.2, {
      type: "file-change",
      content: "The file docs/reference/indexing.md has been updated.",
      result: { itemId: "toolu_03", phase: "completed", status: "completed" },
    }),
    event(9, start, 14, {
      type: "error",
      content: "Overloaded (529); retrying in 4s",
      result: { status: "retrying" },
    }),
    event(10, start, 20, {
      type: "command-execution",
      toolName: "Bash",
      result: {
        itemId: "toolu_04",
        name: "Bash",
        command: "rzm validate docs/reference",
        phase: "started",
      },
    }),
    event(11, start, 23.8, {
      type: "command-execution",
      content:
        "docs/reference/indexing.md: ok\n1 issue: docs/reference/old-flags.md links a removed heading",
      result: { itemId: "toolu_04", phase: "failed", status: "failed", exitCode: 1 },
    }),
    event(12, start, 26, {
      type: "assistant-text-delta",
      role: "assistant",
      content:
        "The remaining validation issue predates this change, so I left `old-flags.md` alone and noted it in the summary.",
      result: { itemId: "msg-2", phase: "completed" },
    }),
    event(13, start, 268, {
      type: "token-usage",
      result: { usage: { inputTokens: 41_020, outputTokens: 7_190, totalTokens: 48_210 } },
    }),
    event(14, start, 268, {
      type: "final-message",
      content:
        "Replaced the removed `--legacy-index` flag with `--rebuild` in three notes.\n\n- `docs/reference/indexing.md`\n- `docs/reference/guides/getting-started.md`\n- `README.md`\n\n`rzm validate docs/reference` still reports one older issue in `old-flags.md`, which this change does not touch.",
    }),
  ];
}

/** Codex: reasoning items, aggregated command output, a patch, an MCP call with string input. */
export function codexTranscript(start: number): TaskTranscriptEvent[] {
  const e = (
    id: number,
    seconds: number,
    fields: Omit<TaskTranscriptEvent, "id" | "sessionId" | "createdAt">,
  ) => event(id, start, seconds, fields, CODEX_RUN_ID);

  return [
    e(1, 1, { type: "turn-started", result: { turnId: "turn-1", phase: "started" } }),
    e(2, 3, {
      type: "reasoning",
      content: "Looking for the debounce window in the watch hub tests.",
      result: { itemId: "rs_1", turnId: "turn-1", phase: "completed" },
    }),
    e(3, 4, {
      type: "command-execution",
      result: {
        itemId: "call_1",
        turnId: "turn-1",
        command: "/bin/zsh -lc 'go test ./pkg/vault/watchhub -run Debounce -count=20'",
        cwd: "/tmp/wt/flaky-tests",
        phase: "started",
        status: "inProgress",
      },
    }),
    e(4, 41.5, {
      type: "command-execution",
      result: {
        itemId: "call_1",
        turnId: "turn-1",
        phase: "failed",
        status: "failed",
        exitCode: 1,
        output:
          "--- FAIL: TestWatchHubDebounce (0.21s)\n    hub_test.go:88: got 2 events, want 1\nFAIL",
        truncated: true,
      },
    }),
    e(5, 44, {
      type: "file-change",
      result: {
        itemId: "patch_1",
        turnId: "turn-1",
        phase: "started",
        paths: ["pkg/vault/watchhub/hub_test.go"],
      },
    }),
    e(6, 44.2, {
      type: "file-change",
      result: {
        itemId: "patch_1",
        turnId: "turn-1",
        phase: "completed",
        paths: ["pkg/vault/watchhub/hub_test.go"],
        diff: "@@ -85,3 +85,3 @@\n-\twait := 50 * time.Millisecond\n+\twait := hub.Debounce() + 50*time.Millisecond",
      },
    }),
    e(7, 46, {
      type: "tool-call",
      result: {
        itemId: "mcp_1",
        turnId: "turn-1",
        name: "rhizome/search",
        phase: "started",
        input: '{"query":"watch hub debounce","limit":5}',
      },
    }),
    e(8, 46.9, {
      type: "tool-call",
      result: {
        itemId: "mcp_1",
        turnId: "turn-1",
        phase: "completed",
        output: { hits: [{ path: "docs/reference/subsystems/indexing.md" }] },
      },
    }),
    e(9, 50, {
      type: "turn-completed",
      result: { turnId: "turn-1", phase: "failed", status: "failed" },
    }),
    e(10, 50, { type: "transcript-truncated" }),
    e(11, 50, {
      type: "token-usage",
      result: { usage: { inputTokens: 9_000, outputTokens: 1_200, totalTokens: 10_200 } },
    }),
    e(12, 50, {
      type: "final-message",
      content: "Could not stabilise the test; see the failure above.",
    }),
  ];
}

export function runDetailFixture(now = Date.now()): TaskRunDetail {
  const run = runsFixture(now)[0]!;
  const start = new Date(run.startedAt!).getTime();

  return {
    run,
    events: claudeTranscript(start),
    next: 18_342,
    checkLog:
      "scanning 412 notes for removed CLI flags\nfound --legacy-index in 3 notes\nwrote context: 3 paths",
  };
}
