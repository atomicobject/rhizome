---
type: ProductSpec
id: SPEC-0140
summary: An AI chat in the app's right pane, in every section, that knows what the person is looking at, reads the page on demand, and acts through the Codex or Claude harness with few permission prompts.
spec-status: active
last-updated: 2026-11-14
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

- The right pane is one pane for the whole app. In Notes its tabs are Info, Outline, Graph, and Chat for a note, Record and Chat for a selected row, and Chat alone where Notes has nothing else to show. Other sections show Chat alone. The Agent page has no right pane, because it is the full chat.
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
- The composer names the harness, the model, and the permission mode, and offers Stop while a turn runs.
- Without an installed harness the pane says how to set one up instead of offering a message box.

### US4 - Continue on the Agent page

- id:: ^SPEC-0140-US4
- summary:: The pane's conversation is an Agent page session, so the person can open it there or start a new one.
- status:: ready

#### Acceptance Criteria

- The pane's first message creates an Agent session; later messages continue it, across reloads.
- Open in Agent shows that session on the Agent page. New chat starts over in the pane and leaves the old session on the Agent page.
- A session deleted on the Agent page starts the pane over.

## Requirements

- The pane and the Agent page share one implementation of a session's thread, stream, and sending.
- The page-text relay authenticates the tool with a random per-session token held in memory, and the relay itself keeps no page text. The tool's result is part of the session transcript, bounded like any tool output, as a file the agent reads is.
- Context fields are bounded and validated at the HTTP boundary.
