import { isString, type JsonValue } from "../../api/parse";
import type { TaskTranscriptEvent } from "../../api/tasks";

// Turns transcript lines (pkg/app/tasks transcript.go) into timeline blocks.
// Command, tool, and file records arrive as `started` plus a terminal phase
// and merge by itemId. Text and reasoning arrive whole; a later block for the
// same itemId replaces the earlier one in place.

type Span = { key: string; startedAt: string; endedAt?: string; phase?: string };

export type TimelineBlock =
  | { kind: "text"; key: string; content: string }
  | { kind: "thinking"; key: string; content: string }
  | (Span & {
      kind: "command";
      command: string;
      cwd?: string;
      exitCode?: number;
      output: string;
      truncated: boolean;
    })
  | (Span & { kind: "tool"; name: string; input: string; output: string; truncated: boolean })
  | (Span & {
      kind: "file";
      paths: string[];
      diff: string;
      input: string;
      output: string;
      truncated: boolean;
    })
  | { kind: "error"; key: string; message: string; retrying: boolean }
  | { kind: "turn-failed"; key: string; phase: string; status?: string }
  | { kind: "truncated"; key: string }
  | { kind: "other"; key: string; type: string; detail: string };

const TERMINAL = new Set(["completed", "failed", "declined", "cancelled"]);

/** Pretty-prints a JSON value; a string holding JSON is parsed first. */
export function preview(value: JsonValue | undefined): string {
  if (value === undefined || value === null) return "";

  if (isString(value)) {
    const trimmed = value.trim();

    if (trimmed.startsWith("{") || trimmed.startsWith("[")) {
      try {
        return JSON.stringify(JSON.parse(trimmed), null, 2);
      } catch {
        return value;
      }
    }

    return value;
  }

  return JSON.stringify(value, null, 2);
}

function blockKey(event: TaskTranscriptEvent): string {
  return event.id ? `e${event.id}` : `i${event.result?.itemId ?? ""}`;
}

function outputOf(event: TaskTranscriptEvent): string {
  return event.content || preview(event.result?.output);
}

function lastOfType(events: TaskTranscriptEvent[], type: string) {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    if (events[index]?.type === type) return events[index];
  }

  return undefined;
}

export function finalMessage(events: TaskTranscriptEvent[]): string | undefined {
  return lastOfType(events, "final-message")?.content;
}

export function lastUsage(events: TaskTranscriptEvent[]) {
  return lastOfType(events, "token-usage")?.result?.usage;
}

/**
 * Appends `incoming` to `current`. The writer corrects a block it already
 * wrote by appending a line with the same id, so a repeated id replaces the
 * held line in place.
 */
export function appendEvents(current: TaskTranscriptEvent[], incoming: TaskTranscriptEvent[]) {
  const merged = [...current];
  const at = new Map(merged.map((event, index) => [event.id, index]));

  for (const event of incoming) {
    const index = at.get(event.id);

    if (index === undefined) {
      at.set(event.id, merged.length);
      merged.push(event);
    } else {
      merged[index] = event;
    }
  }

  return merged;
}

export function buildTimeline(events: TaskTranscriptEvent[]): TimelineBlock[] {
  const blocks: TimelineBlock[] = [];
  const byItem = new Map<string, number>();

  const span = (event: TaskTranscriptEvent): Span => ({
    key: blockKey(event),
    startedAt: event.createdAt,
    phase: event.result?.phase,
  });

  for (const event of events) {
    const result = event.result;
    const itemKey = result?.itemId ? `${event.type}:${result.itemId}` : "";
    const index = itemKey ? byItem.get(itemKey) : undefined;
    const existing = index === undefined ? undefined : blocks[index];

    switch (event.type) {
      case "assistant-text-delta":
      case "message_delta":
      case "reasoning": {
        const content = event.content ?? "";

        if (!content.trim()) break;
        const kind = event.type === "reasoning" ? "thinking" : "text";

        if (existing && (existing.kind === "text" || existing.kind === "thinking")) {
          // A delta chunk continues the block only while it is the last one.
          const continues = result?.phase === "updated" && index === blocks.length - 1;
          blocks[index!] = {
            ...existing,
            content: continues ? existing.content + content : content,
          };
          break;
        }

        if (itemKey) byItem.set(itemKey, blocks.length);
        blocks.push({ kind, key: blockKey(event), content });
        break;
      }

      case "command-execution":
      case "tool-call":
      case "file-change": {
        if (!existing) {
          if (itemKey) byItem.set(itemKey, blocks.length);
          blocks.push(newSpanBlock(event, span(event)));
          break;
        }

        blocks[index!] = mergeSpan(existing, event);
        break;
      }

      case "error":
        blocks.push({
          kind: "error",
          key: blockKey(event),
          message:
            event.content || result?.reason || event.error || "The harness reported an error.",
          retrying: result?.status === "retrying",
        });
        break;
      case "turn-completed":
        if (result?.phase === "failed" || result?.phase === "declined") {
          blocks.push({
            kind: "turn-failed",
            key: blockKey(event),
            phase: result.phase,
            status: result.status,
          });
        }

        break;
      case "transcript-truncated":
        blocks.push({ kind: "truncated", key: blockKey(event) });
        break;
      case "final-message":
      case "token-usage":
      case "turn-started":
      case "diagnostic":
        break;
      default:
        blocks.push({
          kind: "other",
          key: blockKey(event),
          type: event.type,
          detail: event.content || preview(result?.input) || preview(result?.output),
        });
    }
  }

  return blocks;
}

function newSpanBlock(event: TaskTranscriptEvent, base: Span): TimelineBlock {
  const result = event.result;
  const terminal = TERMINAL.has(result?.phase ?? "");
  const ended = terminal ? { endedAt: event.createdAt } : {};
  const output = event.result?.phase === "started" ? "" : outputOf(event);
  const truncated = Boolean(result?.truncated);

  if (event.type === "command-execution") {
    return {
      ...base,
      ...ended,
      kind: "command",
      command: result?.command || preview(result?.input),
      cwd: result?.cwd,
      exitCode: result?.exitCode,
      output,
      truncated,
    };
  }

  if (event.type === "file-change") {
    return {
      ...base,
      ...ended,
      kind: "file",
      paths: result?.paths ?? [],
      diff: result?.diff ?? "",
      input: preview(result?.input),
      output,
      truncated,
    };
  }

  return {
    ...base,
    ...ended,
    kind: "tool",
    name: result?.name || event.toolName || "Tool",
    input: preview(result?.input),
    output,
    truncated,
  };
}

function mergeSpan(existing: TimelineBlock, event: TaskTranscriptEvent): TimelineBlock {
  const result = event.result;
  const phase = result?.phase;

  if (!TERMINAL.has(phase ?? "")) return existing;
  const output = outputOf(event);
  const truncated = Boolean(result?.truncated);

  switch (existing.kind) {
    case "command":
      return {
        ...existing,
        phase,
        endedAt: event.createdAt,
        command: existing.command || result?.command || "",
        exitCode: result?.exitCode ?? existing.exitCode,
        output: output || existing.output,
        truncated: truncated || existing.truncated,
      };
    case "file":
      return {
        ...existing,
        phase,
        endedAt: event.createdAt,
        paths: result?.paths?.length ? result.paths : existing.paths,
        diff: result?.diff || existing.diff,
        output: output || existing.output,
        truncated: truncated || existing.truncated,
      };
    case "tool":
      return {
        ...existing,
        phase,
        endedAt: event.createdAt,
        input: existing.input || preview(result?.input),
        output: output || existing.output,
        truncated: truncated || existing.truncated,
      };
    default:
      return existing;
  }
}
