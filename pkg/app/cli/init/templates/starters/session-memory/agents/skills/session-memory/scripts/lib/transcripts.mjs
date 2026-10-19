// Normalize Claude Code and Codex transcripts into one session shape.
//
// A session is { harness, id, file, parentId, kind, cwd, repoUrl, start, end,
// title, messages }. A message is { i, id, ts, role, kind, text?, tool?,
// reads?, edits?, origin?, confidence?, basis? }, where kind is "text",
// "tool_call", "tool_result", or "turn_end". Tool output is never kept.

import { readFileSync, statSync } from "node:fs";
import { basename, dirname } from "node:path";
import { codexExecCalls, isReadOnlyShell, patchPaths, relativize, shellPaths } from "./paths.mjs";

// Leading text that marks content the harness injected, not something a person
// or agent wrote for this conversation.
const HARNESS_PREFIXES = [
  /^<(system-reminder|recommended_plugins|task-notification|command-name|command-message|command-args|local-command-stdout|local-command-stderr|local-command-caveat|environment_context|user_instructions|skill|turn_aborted|user_shell_command|subagent_notification|guardian_tool_descriptions)\b/,
  /^# AGENTS\.md instructions for /,
  /^\[Request interrupted by user/,
  /^\[SYSTEM NOTIFICATION/,
  /^This session is being continued from a previous conversation/,
  /^Delegated task node:/,
  /^Update on pull request/,
  /^T3 Code stopped watching/,
  /^Note: the T3 server/,
  /^Your previous turn was/,
  /^Caveat: The messages below/,
];

// Leading text that marks a brief written by another agent.
const AGENT_PREFIXES = [
  /^Act as the (implementation|review|research|design)/i,
  /^You are (a|the) (subagent|reviewer|implementer)\b/i,
  /^Another Claude session sent a message/,
  /^\[Subagent hand-back\]/,
];

function classifyText(text) {
  const t = text.trimStart();
  if (HARNESS_PREFIXES.some((re) => re.test(t))) return "harness";
  if (AGENT_PREFIXES.some((re) => re.test(t))) return "agent";
  return null;
}

function readJsonl(file) {
  const rows = [];
  for (const line of readFileSync(file, "utf8").split("\n")) {
    if (!line) continue;
    try {
      rows.push(JSON.parse(line));
    } catch {
      // A truncated last line in a live transcript is expected.
    }
  }
  return rows;
}

function textBlocks(content) {
  if (typeof content === "string") return [{ type: "text", text: content }];
  return Array.isArray(content) ? content : [];
}

function claudeToolPaths(name, input, root) {
  const reads = [];
  const edits = [];
  const add = (list, p) => {
    const rel = relativize(p, root);
    if (rel) list.push(rel);
  };
  switch (name) {
    case "Read":
    case "NotebookRead":
      add(reads, input?.file_path ?? input?.notebook_path);
      break;
    case "Grep":
    case "Glob":
    case "LS":
      add(reads, input?.path);
      break;
    case "Edit":
    case "MultiEdit":
    case "Write":
    case "NotebookEdit":
      add(edits, input?.file_path ?? input?.notebook_path);
      break;
    case "Bash": {
      const r = shellPaths(input?.command, root);
      reads.push(...r.reads);
      edits.push(...r.edits);
      break;
    }
  }
  return { reads, edits };
}

const READ_ONLY_TOOLS = new Set(["Read", "Grep", "Glob", "LS", "NotebookRead", "WebFetch", "WebSearch", "ToolSearch"]);

// Tool calls that start another agent session. Their text lets families link a
// delegated or spawned session back to the session that started it.
const SPAWN_TOOL = /(?:^|__)(delegate_task|create_threads|t3_thread_launch|t3_thread_send|spawn_agent)$|^(Agent|Task)$/;
const CLI_SPAWN = /(?:^|[\s;&|(])(?:codex\s+(?:exec|e)\b|claude\s+(?:-p|--print)\b|\S*claude-fable\b)/;

function spawnTexts(input) {
  const texts = [];
  for (const k of ["task", "message", "prompt"]) if (typeof input?.[k] === "string") texts.push(input[k]);
  for (const t of input?.threads ?? []) if (typeof t?.message === "string") texts.push(t.message);
  return texts;
}

export function parseClaude(file, { root } = {}) {
  const rows = readJsonl(file);
  const isSubagent = file.includes("/subagents/");
  const id = isSubagent ? `${basename(dirname(dirname(file)))}/${basename(file, ".jsonl")}` : basename(file, ".jsonl");
  const parentId = isSubagent ? basename(dirname(dirname(file))) : null;
  const cwd = rows.find((r) => r.cwd)?.cwd ?? null;
  const session = {
    harness: "claude",
    id,
    file,
    parentId,
    kind: isSubagent ? "subagent" : "main",
    cwd,
    repoUrl: null,
    start: null,
    end: null,
    title: null,
    entrypoint: rows.find((r) => r.entrypoint)?.entrypoint ?? null,
    spawns: [],
    messages: [],
  };
  const base = root ?? cwd;
  let firstPrompt = true;
  for (const r of rows) {
    if (r.type === "ai-title" || r.type === "custom-title") {
      session.title = r.aiTitle ?? r.customTitle ?? r.title ?? session.title;
      continue;
    }
    if (r.timestamp) {
      session.start ??= r.timestamp;
      session.end = r.timestamp;
    }
    if (r.type === "system" && r.subtype === "compact_boundary") {
      session.messages.push({ i: session.messages.length, id: r.uuid, ts: r.timestamp, role: "system", kind: "turn_end", compact: true });
      continue;
    }
    if (r.type === "assistant") {
      for (const b of textBlocks(r.message?.content)) {
        if (b.type === "text" && b.text?.trim()) {
          session.messages.push({ i: session.messages.length, id: r.uuid, ts: r.timestamp, role: "assistant", kind: "text", text: b.text });
        } else if (b.type === "tool_use") {
          const { reads, edits } = claudeToolPaths(b.name, b.input, base);
          const call = { i: session.messages.length, id: r.uuid, ts: r.timestamp, role: "assistant", kind: "tool_call", tool: b.name, reads, edits, readOnly: READ_ONLY_TOOLS.has(b.name) || (b.name === "Bash" && edits.length === 0 && isReadOnlyShell(b.input?.command)) };
          if (SPAWN_TOOL.test(b.name)) for (const text of spawnTexts(b.input)) session.spawns.push({ ts: r.timestamp, text });
          if (b.name === "Bash" && CLI_SPAWN.test(String(b.input?.command ?? ""))) session.spawns.push({ ts: r.timestamp, cli: true });
          session.messages.push(call);
        }
      }
      continue;
    }
    if (r.type !== "user") continue;
    for (const b of textBlocks(r.message?.content)) {
      if (b.type === "tool_result") {
        session.messages.push({ i: session.messages.length, id: r.uuid, ts: r.timestamp, role: "tool", kind: "tool_result" });
        continue;
      }
      if (b.type !== "text" || !b.text?.trim()) continue;
      const msg = { i: session.messages.length, id: r.uuid, ts: r.timestamp, role: "user", kind: "text", text: b.text };
      Object.assign(msg, claudeOrigin(r, b.text, { isSubagent, firstPrompt, entrypoint: session.entrypoint }));
      if (msg.origin !== "harness") firstPrompt = false;
      session.messages.push(msg);
    }
  }
  markTurnEnds(session);
  return session;
}

function claudeOrigin(row, text, { isSubagent, firstPrompt, entrypoint }) {
  const marker = classifyText(text);
  if (row.isMeta || row.isCompactSummary || row.promptSource === "system" || marker === "harness") {
    return { origin: "harness", confidence: "confirmed", basis: ["injected"] };
  }
  if (marker === "agent") return { origin: "agent", confidence: "confirmed", basis: ["agent-brief-marker"] };
  if (isSubagent || row.isSidechain) return { origin: "agent", confidence: "confirmed", basis: ["subagent-transcript"] };
  if (row.turnOrigin === "human") return { origin: "person", confidence: "confirmed", basis: ["turnOrigin=human"] };
  if (row.turnOrigin === "task_notification" || row.turnOrigin === "peer") {
    return { origin: "harness", confidence: "confirmed", basis: [`turnOrigin=${row.turnOrigin}`] };
  }
  // The entrypoint is transport, not authorship: automation can use the CLI too.
  const ep = row.entrypoint ?? entrypoint;
  if (firstPrompt) return { origin: "unknown", confidence: "unknown", basis: [`entrypoint=${ep}`, "session-opening"] };
  return { origin: "person", confidence: "likely", basis: [`entrypoint=${ep}`, "mid-session-reply"] };
}

export function parseCodex(file, { root } = {}) {
  const rows = readJsonl(file);
  const meta = rows.find((r) => r.type === "session_meta")?.payload ?? {};
  const kind = meta.thread_source === "guardian_review" ? "guardian" : meta.thread_source === "subagent" || meta.parent_thread_id ? "subagent" : meta.originator === "codex_exec" || meta.source === "exec" ? "exec" : "main";
  const cwd = meta.cwd ?? null;
  const session = {
    harness: "codex",
    id: meta.id ?? meta.session_id ?? basename(file, ".jsonl"),
    file,
    parentId: meta.parent_thread_id ?? null,
    kind,
    cwd,
    repoUrl: meta.git?.repository_url ?? null,
    start: meta.timestamp ?? rows[0]?.timestamp ?? null,
    end: null,
    title: null,
    entrypoint: meta.originator ?? null,
    spawns: [],
    messages: [],
  };
  const base = root ?? cwd;
  let firstPrompt = true;
  for (const r of rows) {
    if (r.timestamp) session.end = r.timestamp;
    const p = r.payload ?? {};
    if (r.type === "compacted" || (r.type === "response_item" && p.type === "compaction")) {
      session.messages.push({ i: session.messages.length, ts: r.timestamp, role: "system", kind: "turn_end", compact: true });
      continue;
    }
    if (r.type === "event_msg" && p.type === "task_complete") {
      session.messages.push({ i: session.messages.length, ts: r.timestamp, role: "system", kind: "turn_end" });
      continue;
    }
    if (r.type !== "response_item") continue;
    if (p.type === "message" && (p.role === "user" || p.role === "assistant")) {
      const text = (p.content ?? []).map((c) => c?.text ?? "").filter(Boolean).join("\n");
      if (!text.trim()) continue;
      const msg = { i: session.messages.length, id: p.id, ts: r.timestamp, role: p.role, kind: "text", text };
      if (p.role === "assistant" && p.phase) msg.phase = p.phase;
      if (p.role === "user") {
        Object.assign(msg, codexOrigin(text, kind, firstPrompt));
        if (msg.origin !== "harness") firstPrompt = false;
      }
      session.messages.push(msg);
    } else if (p.type === "agent_message") {
      const text = (Array.isArray(p.content) ? p.content.map((c) => c?.text ?? "").join("\n") : String(p.content ?? "")).trim();
      if (!text) continue;
      // Messages from a parent arrive as tasks; messages to the parent are reports.
      const toParent = typeof p.recipient === "string" && typeof p.author === "string" && p.author.startsWith(p.recipient + "/");
      session.messages.push({ i: session.messages.length, id: p.id, ts: r.timestamp, role: toParent ? "assistant" : "user", kind: "text", text, origin: "agent", confidence: "confirmed", basis: ["agent_message"], interAgent: true });
    } else if (p.type === "custom_tool_call" && p.name === "exec") {
      const reads = [];
      const edits = [];
      let readOnly = true;
      const calls = codexExecCalls(p.input);
      for (const call of calls) {
        if (call.tool === "exec_command") {
          const r2 = shellPaths(call.workdir ? `cd ${JSON.stringify(call.workdir)} && ${call.cmd}` : call.cmd, base);
          reads.push(...r2.reads);
          edits.push(...r2.edits);
          if (r2.edits.length || !isReadOnlyShell(call.cmd)) readOnly = false;
          if (CLI_SPAWN.test(call.cmd)) session.spawns.push({ ts: r.timestamp, cli: true });
        } else if (call.tool === "apply_patch") {
          edits.push(...patchPaths(call.patch, base));
          readOnly = false;
        } else if (call.tool === "spawn") {
          session.spawns.push({ ts: r.timestamp, text: call.text });
          readOnly = false;
        }
      }
      if (!calls.length) readOnly = false;
      session.messages.push({ i: session.messages.length, id: p.call_id, ts: r.timestamp, role: "assistant", kind: "tool_call", tool: "exec", reads, edits, readOnly });
    } else if (p.type === "function_call") {
      let args = {};
      try {
        args = JSON.parse(p.arguments ?? "{}");
      } catch {}
      const reads = [];
      const edits = [];
      if (p.name === "read_file" || p.name === "search_contents") {
        const rel = relativize(args.path, base);
        if (rel) reads.push(rel);
      }
      if (p.name === "write_file" || p.name === "append_to_file") {
        const rel = relativize(args.path, base);
        if (rel) edits.push(rel);
      }
      const call = { i: session.messages.length, id: p.call_id, ts: r.timestamp, role: "assistant", kind: "tool_call", tool: p.name, reads, edits, readOnly: reads.length > 0 && edits.length === 0 };
      if (SPAWN_TOOL.test(p.name)) for (const text of spawnTexts(args)) session.spawns.push({ ts: r.timestamp, text });
      session.messages.push(call);
    } else if (p.type === "function_call_output" || p.type === "custom_tool_call_output") {
      session.messages.push({ i: session.messages.length, ts: r.timestamp, role: "tool", kind: "tool_result" });
    }
  }
  markTurnEnds(session);
  return session;
}

function codexOrigin(text, kind, firstPrompt) {
  const marker = classifyText(text);
  if (marker === "harness") return { origin: "harness", confidence: "confirmed", basis: ["injected"] };
  if (marker === "agent") return { origin: "agent", confidence: "confirmed", basis: ["agent-brief-marker"] };
  if (kind === "subagent" || kind === "guardian") return { origin: "agent", confidence: "confirmed", basis: [`session=${kind}`] };
  if (kind === "exec") return { origin: "agent", confidence: "likely", basis: ["session=exec"] };
  if (firstPrompt) return { origin: "unknown", confidence: "unknown", basis: ["session-opening"] };
  return { origin: "person", confidence: "likely", basis: ["mid-session-reply"] };
}

// A turn ends at the last assistant text before the next non-harness user
// message. The watermark only advances through completed turns.
function markTurnEnds(session) {
  const msgs = session.messages;
  let lastAssistantText = -1;
  for (let k = 0; k < msgs.length; k++) {
    const m = msgs[k];
    if (m.role === "assistant" && m.kind === "text") lastAssistantText = k;
    if (m.role === "user" && m.kind === "text" && m.origin !== "harness" && lastAssistantText >= 0) {
      msgs[lastAssistantText].endsTurn = true;
      lastAssistantText = -1;
    }
  }
  session.completeThrough = msgs.reduce((acc, m) => (m.endsTurn ? m.i : acc), -1);
  try {
    session.mtime = statSync(session.file).mtimeMs;
  } catch {
    session.mtime = 0;
  }
}
