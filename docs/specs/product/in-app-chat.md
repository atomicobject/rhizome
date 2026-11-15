---
type: ProductSpec
id: SPEC-0140
summary: An AI chat in the app's right pane, in every section, that knows what the person is looking at, reads the page on demand, and acts through the Codex or Claude harness with few permission prompts.
spec-status: active
last-updated: 2026-11-15
aliases: [SPEC-0140]
---

# In-app chat

## Summary

Rhizome's Agent page holds full-page chat sessions with a coding harness ([[coding-agent-harness|SPEC-0101]]), but asking about a note or a view means leaving it and describing it. This spec puts that chat beside whatever the person is looking at.

Chat is a tab of the app's one right pane. In Notes it follows Info, Outline, and Graph, or Record. Elsewhere the pane holds Chat alone. Each message carries a structured description of the current view, and the agent can read the view's visible text when it needs more. Claude runs in auto mode and Codex in Approve for me by default. A reviewer allows routine actions, so the person sees a quiet line per action and an approval card only when the reviewer escalates.

The pane and the Agent page show the same sessions. The Agent page is the expanded view of a pane conversation.

## Goals

- Ask about a note, view, type, or the ontology without leaving it or describing it.
- Let the agent read what is on screen when the structured description is not enough.
- Let the agent act with few interruptions, and keep risky actions behind an explicit approval.
- One right pane with one toggle, on VS Code's panel shortcuts.

## Non-Goals

- Screenshots of the view. Capture needs a native snapshot in Rhizome Desktop and a separate browser path; it is a follow-up phase.
- Staged edits the person reviews before they apply.
- A second agent runtime. The pane uses the Agent page's sessions and harnesses.

## User Stories

### US1 - Open Chat beside any view

- id:: ^SPEC-0140-US1
- summary:: A person opens Chat in the right pane from any section except the Agent page, with one toggle and VS Code's shortcuts.
- status:: ready

#### Acceptance Criteria

- The right pane is one pane for the whole app. In Notes its tabs are Info, Outline, Graph, and Chat for a note, and Record and Chat for a selected row. Where a section has nothing of its own for the pane, such as Notes Home, a view without a selected row, or another section, the pane holds Chat alone and shows only once the person chooses Chat, so those pages keep their width. The Agent page has no right pane, because it is the full chat.
- One button at the far right of the top bar toggles the pane; Rhizome Desktop has the same button at the far right of its toolbar.
- `]` toggles the pane. ⌥⌘B (Ctrl+Alt+B elsewhere) shows the pane on Chat and focuses the message box, or hides the pane when Chat is showing. ⌘B (Ctrl+B) toggles the left sidebar. In Rhizome Desktop the View menu owns ⌘B and has a Show Chat item on ⌥⌘B.
- The pane is resizable from 280 to 720 pixels and never leaves the section narrower than about 640 pixels. Below 1200 pixels it opens over the content, and Escape closes it.
- The pane's width, open state, and tab, and the current conversation and draft, carry across sections.

### US2 - Ask about what you're looking at

- id:: ^SPEC-0140-US2
- summary:: Each message carries what the person is looking at, and the agent can read the view's visible text on demand.
- status:: ready

#### Acceptance Criteria

- The composer shows a chip naming the current view by kind and title: a note by its title, a view by its name, a type, a group, the Atlas, or a section. The person can detach it for one page; moving to another page attaches the new one.
- Sending stores the context with the message and gives the agent a short block naming the view, its note path or view id, and its route, ahead of the person's text.
- A message's chip appears in the transcript only where the context changed from the previous message.
- The agent has a `page_text` tool. A call asks the window showing the chat for the visible text of the current section, without the chat itself, including same-origin custom view frames. The reply is bounded, and the tool fails fast when no window shows the chat.
- The transcript shows a page read as one line.

### US3 - Let the agent act with few prompts

- id:: ^SPEC-0140-US3
- summary:: The agent runs in Claude auto mode or Codex Approve for me by default, routine actions show as quiet lines, and only escalations ask.
- status:: ready

#### Acceptance Criteria

- A harness permission mode `auto` maps to Claude's `--permission-mode auto` and to Codex's automatic approval reviewer. It is the default for chat sessions; a stored explicit mode is kept.
- Commands, file edits, and tool calls show as one line each. In auto mode their detail says auto mode allowed them.
- An escalated action shows a card with Allow and Deny, and Allow for this chat where the harness supports it. Once decided, the card collapses to one line.
- The composer offers Stop while a turn runs.

### US4 - Continue on the Agent page

- id:: ^SPEC-0140-US4
- summary:: The pane's conversation is an Agent page session, so the person can open it there or start a new one.
- status:: ready

#### Acceptance Criteria

- The pane's first message creates an Agent session; later messages continue it, across reloads.
- Open in Agent shows that session on the Agent page. New chat starts over in the pane and leaves the old session on the Agent page.
- A session deleted on the Agent page starts the pane over.

### US5 - Choose the agent and model in the chat

- id:: ^SPEC-0140-US5
- summary:: One picker beside Send chooses the coding agent, its model, and its reasoning effort, a menu beside it chooses the permission mode, and a notice in the conversation says what to do when the chosen agent cannot run.
- status:: ready

#### Acceptance Criteria

- The pane and the Agent page share one composer control. Beside Send, a ghost button reads the model and the effort ("Opus 5.5 · High"), and its tooltip names the agent too. Clicking it or pressing ⇧⌘M (Ctrl+Shift+M elsewhere) opens a popover above the composer, about 430 pixels wide, that floats over the page when the pane is narrower. It holds only a search field, one list of models grouped under "Claude Code" and then "Codex", and a Reasoning column for the chosen model's efforts.
- The list shows every model of each agent that is on, from the agent's status, with a faint "⌘1" to "⌘9" beside the first nine. An agent that is signed out or not installed still lists its models, or itself when it reports none, and shows only a faint "· signed out" or "· not installed" after its heading. Search filters by model or agent name; ↑ and ↓ move, Enter or ⌘1 to ⌘9 chooses, → and ← move between the models and Reasoning, and Escape closes.
- Reasoning lists the chosen model's efforts and checks the current one. When the agent names a default effort (Codex), that row is marked "Default"; when it does not (Claude Code), a first "Default" row leaves the effort to the agent. In the composer, ⇧⌘E opens the picker with focus in Reasoning; elsewhere ⇧⌘E stays the Notes shortcut that starts editing.
- A menu beside the picker ("Auto ⌄"), also on ⇧⌘A, offers Ask first, Auto, Accept edits, and Full access, each with a one-line description, as far as the agent's capabilities list them.
- A choice saves `agent.harness` and that agent's model, effort, and permission mode, which every new chat uses; there is no other default control. It changes nothing else, so a page holding older settings cannot undo a newer choice or turn an agent back on. A message sent while the choice is saving waits for it, so it runs the choice the composer shows. Choosing a model keeps the current effort when the new model offers it.
- An open conversation with the same agent runs the new model and effort from its next turn: Claude Code switches through `set_model` and `apply_flag_settings`, and Codex through the next `turn/start`. A new permission mode resumes the conversation in a new process before its next turn. When the chosen agent differs from the open conversation's, a muted line above the composer reads "Sends in a new chat with Codex", and the next message starts that chat.
- When the chosen agent is signed out or not installed, a notice sits in the conversation above the composer: "Codex is signed out. Run `codex login` in Terminal." with the command in a chip with a copy button, or "Codex isn't installed." with a How to install link. With no agent installed it names both with their install links. It offers Check again, which probes the agents afresh, and "Use Opus 5.5 instead" when a working choice preceded it. Send waits while the notice shows, and the draft stays. A saved choice whose agent cannot run shows the notice as soon as chat opens.
- With nothing chosen, chat uses the first agent that is ready, without asking.

### US6 - Turn agents on or off on this Mac

- id:: ^SPEC-0140-US6
- summary:: Rhizome Desktop's Settings → Rhizome on this Mac → Chat shows each agent's status and a switch that hides it from the picker.
- status:: ready

#### Acceptance Criteria

- Each agent has one row: its name, a status mark, and one plain line (Ready; Signed out with the command to run; or Not installed with How to install), and an on/off switch. Detected agents are on until turned off; a missing agent's switch is off and disabled. Check again probes the agents afresh.
- Turning an agent off stores `agent.harnesses.<kind>.disabled: true` and changes nothing else, hides its models from the picker, and keeps chat from ever resolving it; chat falls back to the first enabled agent that is ready. A chat already open on that agent refuses its next message with "Codex is turned off in Settings." Open pages read the change at once.

## Requirements

- The pane and the Agent page share one implementation of a session's thread, stream, and sending.
- The page-text relay authenticates the tool with a random per-session token held in memory, and the relay itself keeps no page text. The tool's result is part of the session transcript, bounded like any tool output, as a file the agent reads is.
- Context fields are bounded and validated at the HTTP boundary.
- A turn runs on the model and effort saved for its session's agent. A saved model the agent no longer offers falls back to its default, and an effort the model lacks falls back to the model's default.
- A turn never waits on a fresh status probe: an open process reuses the cached status, and only a new process probes.
