---
type: ReferenceDoc
summary: "How the in-app chat launches Claude Code or Codex, what view context, instructions, skills, tools, and permissions the agent gets, and what that means for writing a usage skill it can follow."
reference-kind: analysis
status: draft
last-verified: 2026-11-15
---

# In-app chat agent environment

The in-app chat (SPEC-0140, `docs/specs/product/in-app-chat.md`; effort `docs/efforts/2026-11-14-16-40-in-app-chat.md`; merged as PR #148, commit 395747786) is the most constrained place a usage skill will run. This note records what an agent in that chat receives and can do, verified against the code, and what the skill must do differently there than in a standalone repository session. Inference is labeled.

## How a turn reaches the agent

**Verified.**
- The pane's first message creates an Agent-page session, and later messages continue it, so the pane and the Agent page share one implementation (`web/src/components/agent/useAgentThread.ts`, `web/src/components/chat/chatSession.ts`). Sessions are stored per vault (`pkg/app/agentchat/service.go`, `NewService` calls `OpenStore(ctx, vaultPath)`).
- On the first turn the runtime starts the harness with `harness.SessionOptions{Cwd: vaultPath, Model, Effort, PermissionMode, Resume, Instructions: chatInstructions, MCPServers: [rhizome_page]}` (`pkg/app/agentchat/service.go`, `beginTurn`). The working directory is the vault path the runtime serves, which for a repository is its root. Later reloads resume the same harness session ID.
- The harness choice is a request value, else the user's `agent.harness` setting, else the only harness that is installed and signed in (`pkg/app/agentchat/resolve.go`). Model, effort, and permission mode come from the user's own Rhizome config, not from the repository (`pkg/app/agentchat/settings.go`, `LoadSettings` through `repoconfig.LoadCliConfig`). The permission mode defaults to `auto`.
- Each user message is prefixed with a view block (`pkg/app/agentchat/view_context.go`, `turnText`):

```text
[Rhizome view] Note "Title" · docs/path/to/note.md
Route: /notes?note=docs/path/to/note.md
Detail: Section heading

<the person's text>
```

  The kinds are `note`, `view`, `type`, `group`, `ontology`, `notes` (All notes, Problems, Changes), and `section` (any non-Notes section, titled only by its name, such as "Explorer", "Tasks", or "Graphql"; `web/src/components/chat/viewContext.ts`). The fields are bounded: title 200 runes, path and view ID 1,024 bytes, route 2,048 bytes, detail 2,000 runes. The person can detach the chip for one page. Routes use `/notes?note=<path>` and `/notes?view=<id>` (`web/src/components/notesRoute.ts`).

## What the agent is told

**Verified.** The whole Rhizome-specific instruction is one constant, `chatInstructions` in `pkg/app/agentchat/page_relay.go`:

> You are the chat assistant inside Rhizome, the knowledge base app the user has open. A user message may begin with a [Rhizome view] block naming what the user is looking at; treat it as the subject of words like "this", "here", and "these". When you need what is on screen beyond that block, call the page_text tool from the rhizome_page server; it returns the visible text of the user's current view. Prefer Rhizome's own tools and the repository's notes for facts. Keep answers short and concrete.

Claude receives it through `--append-system-prompt` (`pkg/harness/claude/options.go`, `sessionArgs`). Codex receives it as `developerInstructions` on thread start (`pkg/harness/codex/driver.go`, `threadParams`). Nothing in it names a skill, `rzm`, starters, or how to explain Rhizome.

## Which skills and project instructions the agent sees

**Verified for the launch flags; the discovery behavior is inference about the vendors' CLIs.**
- **Claude Code** runs as `claude -p --input-format stream-json --output-format stream-json --verbose --include-partial-messages --strict-mcp-config --permission-prompt-tool stdio --permission-mode <mode> … --add-dir <cwd>` from the vault root (`pkg/harness/claude/options.go`). No `--setting-sources` or `--bare` flag is passed, and no environment is filtered (`pkg/harness/claude/driver.go`). Inference: Claude Code therefore loads `CLAUDE.md` (which includes `AGENTS.md`), `.claude/skills/`, `.claude/settings.json` hooks and sandbox settings, and the person's user-level settings and plugins, as in a terminal session.
- **Codex** runs `codex app-server` with `cwd` set to the vault root (`pkg/harness/codex/options.go`, `pkg/harness/codex/driver.go`). Inference: Codex loads `AGENTS.md` and repository skills from `.agents/skills/` (`docs/reference/guides/harness-capabilities.md` says Codex reads the shared surface).
- So a usage skill installed in a repository's `.agents/skills/` and `.claude/skills/` mirror would be discoverable in chat the same way it is in a terminal. Nothing loads it automatically. The chat instructions never mention it, so either the skill's description must match chat-shaped questions ("what is this", "what can I do here", "how do I…") or `chatInstructions` must route to it. That routing decision belongs to the council.

## Tools the agent has

**Verified.**
- **`rhizome_page.page_text`** is the only tool Rhizome adds (`pkg/app/agentchat/pagemcp/server.go`). It takes no arguments and is pre-approved in both harnesses (`AutoApproveTools: ["page_text"]`). A call goes over a per-session, token-authenticated relay to whichever window shows the chat, which replies with the visible text of `.app-shell__main`, excluding the right pane, hidden tabs and panels, and invisible frames. The reply is capped at 60,000 characters in the page and 60,000 bytes on the server, and the call times out after 20 seconds (`pkg/app/agentchat/page_relay.go`, `web/src/components/agent/pageText.ts`). It fails fast with "The chat isn't open in a Rhizome window, so the page can't be read." when no window subscribes. A server bound to a non-loopback address offers no `page_text` tool at all (effort Execution Notes).
- Same-origin custom-view frames are read (`web/src/components/CustomViewFrame.tsx` adds no sandbox). HTML notes render in a `sandbox="allow-scripts"` frame (`web/src/components/HTMLNoteViewer.tsx`). Inference: their content is cross-origin to the page and drops out of `page_text`, so for an HTML note the agent should read the file at the path in the view block.
- **No Rhizome MCP server in Claude.** `--strict-mcp-config` with only `rhizome_page` means the repository's `.mcp.json` and the person's own MCP servers do not load. Codex adds `rhizome_page` through `-c mcp_servers.…` overrides without a strict flag, so the person's configured Codex MCP servers still load (inference from the flags). The two harnesses therefore see different tool sets.
- Everything else is the harness's own toolset: shell, file reads, and edits. "Rhizome's own tools" in the instructions can only mean the `rzm` CLI through skills, plus `page_text`.
- **No UI control.** The agent cannot navigate the app, open a note, press a shortcut, change Settings, or take a screenshot. Screenshots are an explicit follow-up (SPEC-0140 Non-Goals).
- **No structured questions.** The harness event kinds are turn, text, reasoning, command, file change, tool call, approval, token usage, diagnostic, and error (`pkg/harness/harness.go`). There is no question or choice event. Inference: a Claude `AskUserQuestion` call would surface, if at all, as an Allow/Deny approval card. A skill should ask in plain text and end its turn.

## Permissions and sandbox

**Verified** (`pkg/harness/claude/options.go`, `pkg/harness/codex/options.go`, `web/src/components/ChatDrawer.tsx`).

| Mode (composer label) | Claude | Codex |
| --- | --- | --- |
| `auto` (Auto, the chat default) | `--permission-mode auto` | approval `on-request`, sandbox `workspace-write`, `approvalsReviewer: auto_review` |
| `approval-required` (Ask first) | `default` | `untrusted`, `read-only` |
| `auto-accept-edits` (Auto edits) | `acceptEdits` | `on-request`, `workspace-write` |
| `full-access` (Full access) | `bypassPermissions` plus `--dangerously-skip-permissions` | `never`, `danger-full-access` |

Routine actions appear as one quiet line ("Allowed by auto mode"). A reviewer escalation appears as an Allow / Deny (and, where supported, Allow for this chat) card that collapses once decided (`web/src/components/chat/ChatTimeline.tsx`).

**Inference, unverified in chat.** Codex's turn-level `sandboxPolicy` is `{"type": "workspaceWrite"}` with no network setting, and no permission profile is selected (`pkg/harness/codex/driver.go`). If that overrides the repository's loopback-only `rhizome` profile, `rzm agent …` commands that dial the runtime on `127.0.0.1` would be blocked and escalated to the auto reviewer. Outside chat, approval reviewers have misread that escalation as credential access and data leaving the machine (commit bb746610c, PR #109), which is why `rzm init` writes loopback grants. The effort's live check confirms page reads and answers about a note, a view, and the Atlas, but not `rzm agent` calls from a Codex chat. This needs a live test before the skill relies on code mode in Codex chat.

## Reaching the harness and `rzm` from Desktop

**Verified.** On main, the harness looks up `claude` and `codex` with `exec.LookPath` on the runtime's process PATH (`pkg/harness/internal/command/runner.go`). Desktop starts the runtime with launchd's bare PATH. Open PR #149 ("find Claude Code and Codex from Rhizome Desktop…", unmerged on 2026-11-15) states that chat in Desktop therefore reported both agents as not installed. It adds login-shell PATH discovery, and gives children "the process PATH followed by the login entries it lacks".

**Inference.** Until #149 merges, a Desktop chat may not start at all. After it merges, the agent's shell should find the person's `rzm` and `node` on the same terms as a login shell. A PATH `rzm` delegates to the repository's pinned version unless `RZM_SKIP_REPO_DELEGATE` is set (`AGENTS.md`, Dogfood testing). `rhizome_page` itself runs the runtime executable with `RZM_SKIP_REPO_DELEGATE=1` (`pkg/app/agentchat/page_relay.go`).

## How answers render

**Verified.** Assistant text renders with `react-markdown` and no plugins (`web/src/components/chat/ChatTimeline.tsx`), so only CommonMark renders. GFM tables, task lists, strikethrough, and bare-URL autolinks do not. The pane is 280 to 720 pixels wide and opens as an overlay below 1,200 pixels (SPEC-0140 US1). Links render as plain anchors with no app-aware handler. Inference: a relative link such as `/notes?note=docs/specs/x.md` would navigate the window with a full page load, and a filesystem path would not open anything.

## What the agent can and cannot do, for the skill

Can, verified: read and edit repository files (Claude `--add-dir` the root; Codex workspace-write); run shell commands within the active permission mode; read the shown view with `page_text`; know the shown note path, view ID, type, group, or section from the view block; use any skill the harness discovers in the repository; and, when `rzm` is reachable, run non-interactive commands such as `rzm init --check --json` and `rzm init --json --add-addon <id>`.

Cannot, verified: drive the UI, see pixels, use a Rhizome MCP server under Claude, present choice prompts, or render tables. Unknown until tested: whether runtime-backed `rzm agent` calls succeed in a Codex chat.

## Implications for writing the usage skill

1. **Detect the environment from the view block.** A message that starts with `[Rhizome view]` means in-app chat. There the skill should answer about the named subject first, call `page_text` only when the block is not enough, and give UI directions as named controls and shortcuts the person presses ("open Settings › *repository* › Add-ons, check AO KB, Apply changes"), because the agent cannot press them.
2. **Write for a narrow pane.** Use short paragraphs and lists, no tables, and few links. The standalone path can use richer output.
3. **Separate explaining from changing.** Explaining Rhizome and the current view needs only the repository's files and `page_text`. Installing a starter or changing schema writes committed files. In auto mode those writes may not prompt, so the skill should show the `--check --json` preview and get an explicit yes in plain text before applying, then list the files to commit.
4. **Do not assume `rzm` or the Rhizome MCP server.** Check for `rzm` once and fall back to file reading with a stated limitation, following the `rhizome` skill's integration-failure rule. Treat Codex's sandbox and runtime access as unverified until tested.
5. **Route to the skill.** `chatInstructions` is the one place every chat agent reads. A one-sentence pointer there, or a skill description written in a newcomer's words, decides whether the skill is ever loaded in chat. Today nothing points to any skill.
6. **Mention the app's own sessions.** The conversation continues on the Agent page (Open in Agent), and **New chat** starts over. A long setup or customization task may fit better there than in the pane.
