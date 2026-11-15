---
type: EffortNote
id: EFF-2026-11-15-15-45
name: Chat agent and model picker
created-at: 2026-11-15T20:45:00Z
status: complete
summary: Choose the chat agent, model, reasoning effort, and permission mode from the composer, show sign-in help in the conversation, and turn agents on or off in Settings.
aliases: [EFF-2026-11-15-15-45]
governing-specs:
  - "[[in-app-chat]]"
---

# Chat agent and model picker

## Scope

Deliver [[in-app-chat#^SPEC-0140-US5]] and [[in-app-chat#^SPEC-0140-US6]] on top of PR #149, which made Rhizome Desktop find Claude Code and Codex. The work adds a mid-session model and effort change to the harness `Session` contract, which is a subsystem invariant, so it runs as an effort.

## Spec Set (Frozen)

- [[in-app-chat|SPEC-0140]], revision dated 2026-11-15, US5 and US6 and their requirements.

## Stories In Scope (Frozen)

- [[in-app-chat#^SPEC-0140-US5]]
- [[in-app-chat#^SPEC-0140-US6]]

## Spec Coverage Checklist

- [x] One picker in the composer of the right pane and the Agent page: search, models grouped by agent, a Reasoning column, ⌘1–⌘9, ⇧⌘M, ⇧⌘E, and a permission mode menu on ⇧⌘A.
- [x] The last choice is the default for new chats; there is no separate default control.
- [x] An open chat on the same agent switches model and effort from its next turn; a permission mode change resumes the conversation in a new process; a different agent starts a new chat on the next Send, with a one-line hint.
- [x] A signed-out or missing agent shows a notice in the conversation with a copyable command, Check again, and Use the previous model instead, and holds Send while keeping the draft.
- [x] Settings → Rhizome on this Mac → Chat shows each agent's status with an on/off switch; a turned-off agent never resolves and its models leave the picker.
- [x] The setup card, the Agent page rail settings, and the old composer availability text are removed.
- [x] Independent review findings fixed.

## Plan

Approved by Drew Colthorp in chat on 2026-11-15 from the third mockup ("That looks great. I like that a lot. Let's do it."), after he chose one list for both agents, sign-in help in the conversation rather than the picker, and status-only Settings with switches. The coordinator decided two behaviors the mockup did not show: a different agent starts a new chat on the next Send, and a model or effort change applies to an open chat when the CLI supports it.

1. Implementation (Claude Opus 5.5, xhigh) on `t3/chat-agent-model-picker`: harness `Session.SetModel`, settings and the disabled flag, the shared picker and notice, the desktop bridge and Settings section, docs.
2. Independent review (GPT-6.1 Sol, high) and Greptile; fix accepted findings.
3. Close this effort and open the pull request stacked on #149.

## Original Intended Delivery

From the chat composer, a person picks the agent, model, reasoning effort, and permission mode in one quiet popover. If the agent they pick cannot run, the conversation says why and how to fix it. Settings shows which agents work and lets them turn one off.

## Actual Delivered

Built on `t3/chat-agent-model-picker`.

- Both CLIs support a mid-session change, so no "applies to the next chat" hint was needed. Claude Code 2.1.325 takes `set_model` and `apply_flag_settings` with `effortLevel` (a control-only probe of the installed CLI, and golden replies captured from it); Codex 0.169.0 takes `model` and `effort` on `turn/start`, applying to that and later turns (the generated app-server schema). Claude refuses Full access mid-session (`bypass_not_launched`), so a permission mode change resumes the conversation in a new process, as T3 Code does.
- `web/src/components/agent/chatAgent.ts` holds the picker's rules as one pure module; `ChatAgentControls`, `ChatAgentPanels`, and `ChatAgentNotice` serve the pane and the Agent page.
- The desktop `chat-settings` bridge operation reports status and saves the per-agent switch; the `agent-settings` page command keeps open pages in step.

## Deviations

- Claude Code signed in with an API key or a cloud provider reads as signed in, so the notice does not block it. A Codex login that uses only `OPENAI_API_KEY` is not checked.
- A failed status probe shows no notice and leaves Send enabled, since the server still runs the agent.
- Settings has no "Checked N minutes ago" line; nothing records a check time.
- The Agent page header no longer names the model, since the picker does.

## Execution Notes

- The visual comparison against the mockup found the Agent page's button style drawing borders on the picker's buttons; fixed.
- The web E2E agent journey found the picker opening while invisible, so ⌘3 never reached it; fixed.
- The Claude driver crossed the file-size limit; control requests moved to `pkg/harness/claude/control.go`.
- Review fixes (Greptile and GPT-6.1 Sol): a send waits for the choice it shows to save; a turn for an agent turned off in Settings is refused before its message is stored; the composer and the Settings switch each write only their own fields inside one locked rewrite of user configuration; a Claude model change and a turn never overlap; and a model change Claude never answers stops the session after five seconds, so the next turn resumes in a new process.
- Greptile's second pass (4/5) found a failed choice save blocking every later send and the picker overflowing short windows; both fixed in `2610b42c2`, with a test shown to fail before the fix.
- Greptile's third pass (4/5) found a message waiting on one save missing a choice made meanwhile, and the picker's 160px height floor still allowing it past the window's top; both fixed in `da1acd571`, with a test shown to fail before the fix.
- Greptile's fourth pass found Stop not stopping a message still waiting on its model change, and a fresh setup with no agent installed and nothing saved hiding the picker; Stop now cancels the message before its harness turn starts and ends it as interrupted, and the picker (with ⇧⌘M) stays available, labelled "Choose a model", whenever an agent is on. Each fix has a test shown to fail before it.
- Greptile's fifth pass (4/5) found a waiting message going out after a choice made meanwhile that needed sign-in; the wait now rechecks the saved choice and keeps the draft, with a test shown to fail before the fix.
- Greptile's sixth pass (4/5) found the Agent page erasing text typed while a message waited, and a slow login shell spending the agent status check's time limit; the draft now clears only if unchanged (test shown to fail before the fix), and the status check finds the login PATH before its limit starts.
- Greptile's seventh pass found the chat pane's Stop leaving a message that waited for its choice to save to go out afterwards, and another window's open Settings keeping a chat agent's old switch state; Stop now cancels a send that has not reached the server, keeping the draft, and a saved chat setting is broadcast to every window's shell, where Settings checks again. Each fix has a test shown to fail before it.
- Greptile's eighth pass (4/5) found a stopped send's late chat creation replacing a newer chat, and Stop during a Claude model change ending as a connection failure because the driver stopped the process on any cancellation; the pane now drops a stopped send's new chat, and the driver stops the process only when Claude never answers, resending both values after a canceled change. Each has a test shown to fail before the fix.
- Greptile's ninth pass (4/5) found a message sent while the saved chat was still loading going to that chat's agent instead of the one chosen; the pane now reads the open chat's agent before deciding, with a test shown to fail before the fix.
- Greptile's tenth pass (3/5) found a turn stopped during its model change completing without a turn id, so the browser gathered every earlier reply into it, and a failed read of the open chat still sending to its unknown agent; stopped turns now carry their own id, and an unreadable chat starts a new one. Each has a test shown to fail before the fix.
- Greptile's eleventh pass (4/5) found a choice made while the open chat loaded being skipped; the pane now reads the chat first, then waits for the latest choice, and waits again before sending to a chat it just opened, with a test shown to fail before the fix.

## Compounding Follow-ups

- Check ⇧⌘A and ⇧⌘M in Chrome, which may reserve them; Rhizome Desktop's menus do not.
- A live end-to-end turn on each real CLI after a mid-session model change.

## Closure Checklist

- [x] Approved scope delivered or deviations recorded.
- [x] Independent review completed and accepted findings fixed.
- [x] Delivery branch and commit recorded.

## Status

Complete on `t3/chat-agent-model-picker` for [PR #151](https://github.com/atomicobject/rhizome/pull/151), stacked on [PR #149](https://github.com/atomicobject/rhizome/pull/149) and left open and unmerged for Drew. Human-only checks remain as follow-ups: a live turn on each real CLI after a model change, and a click-through in the installed desktop app.
