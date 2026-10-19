// Turn normalized sessions into episodes: the unit that is screened,
// extracted, and cited. Every text field is redacted before it is shortened.

import { redact, shorten } from "./redact.mjs";

const LIMITS = { text: 4000, notes: 1500, context: 1200, person: 600, task: 600, title: 120 };
const MIN_DISCOVERY_CHARS = 200;
const RESEARCH_MIN_CALLS = 8;

function clean(text, max) {
  const r = redact(text ?? "");
  return { text: shorten(r.text, max), redactions: r.count };
}

// The family is the root session: subagents, spawned workers, and delegated
// sessions join the family that started them, so recurrence counts
// independent investigations rather than transcript files.
export function familyOf(session, byId) {
  let cur = session;
  const seen = new Set();
  while (cur?.parentId && !seen.has(cur.id)) {
    seen.add(cur.id);
    const parent = byId.get(cur.parentId);
    if (!parent) return cur.parentId;
    cur = parent;
  }
  return cur.id;
}

// The last message of the last complete turn: a person message through the
// assistant's final reply before the next person message. A trailing turn is
// complete once it ends in assistant text with no tool call after it; one
// still waiting on a reply or mid-investigation is re-read by a later run.
export function completeThrough(session) {
  const msgs = session.messages;
  let lastPerson = -1;
  let lastText = -1;
  let lastCall = -1;
  let turnEnd = -1;
  for (const m of msgs) {
    if (m.role === "user" && m.kind === "text" && m.origin !== "harness") {
      if (lastText > lastPerson) turnEnd = lastText;
      lastPerson = m.i;
    }
    if (m.role === "assistant" && m.kind === "text") lastText = m.i;
    if (m.kind === "tool_call") lastCall = m.i;
  }
  return lastText > lastPerson && lastCall < lastText ? msgs.length - 1 : turnEnd;
}

export function openingTask(session) {
  const m = session.messages.find((x) => x.role === "user" && x.kind === "text" && x.origin !== "harness");
  return m?.text ?? "";
}

export function episodesFor(session, family, { from = 0, through = session.messages.length - 1 } = {}) {
  const msgs = session.messages;
  const episodes = [];
  let redactions = 0;
  const title = clean(session.title ?? "", LIMITS.title);
  const opening = openingTask(session);
  const task = clean(opening, LIMITS.task);
  redactions += title.redactions + task.redactions;
  const base = { harness: session.harness, session: session.id, sessionKind: session.kind, family };

  let lastAssistantText = null;
  let lastPersonText = null;
  // `span` covers tool calls since the last episode; `turn` covers the whole
  // turn, so interim prose does not split an investigation. `prevTurn` keeps
  // the files of the turn a person is replying to.
  const freshSpan = () => ({ calls: 0, readOnly: 0, reads: new Set(), edits: new Set() });
  let span = freshSpan();
  let turn = freshSpan();
  let prevTurn = freshSpan();
  // Codex progress commentary is folded into the turn's final answer.
  let notes = [];
  let lastCommentary = null;
  const finalText = lastTextIndex(msgs);

  const emitAssistant = (m, text, isReport) => {
    const t = clean(text, LIMITS.text);
    const prev = clean(lastPersonText?.text ?? "", LIMITS.person);
    redactions += t.redactions + prev.redactions;
    const research = isReport || (turn.calls >= RESEARCH_MIN_CALLS && turn.readOnly / turn.calls >= 0.75);
    episodes.push({
      ...base,
      id: `${session.harness}:${session.id}:${m.i}`,
      lens: research ? "research" : "discovery",
      ts: m.ts,
      origin: "agent",
      confidence: "confirmed",
      basis: ["assistant"],
      text: t.text,
      context: { title: title.text, task: task.text, previous: prev.text },
      files: { reads: [...span.reads].slice(0, 40), edits: [...span.edits].slice(0, 40) },
      toolCalls: turn.calls,
    });
    span = freshSpan();
  };

  // A turn that ended in commentary alone still yields its notes.
  const flushNotes = () => {
    if (!notes.length) return;
    const joined = notes.join("\n\n");
    notes = [];
    if (joined.length >= MIN_DISCOVERY_CHARS && lastCommentary && lastCommentary.i >= from) emitAssistant(lastCommentary, joined, false);
    lastCommentary = null;
  };

  for (let k = 0; k <= through && k < msgs.length; k++) {
    const m = msgs[k];
    if (m.kind === "tool_call") {
      for (const s of [span, turn]) {
        s.calls++;
        if (m.readOnly) s.readOnly++;
        m.reads?.forEach((p) => s.reads.add(p));
        m.edits?.forEach((p) => s.edits.add(p));
      }
      continue;
    }
    if (m.kind !== "text") continue;

    if (m.role === "user") {
      flushNotes();
      if (k >= from && (m.origin === "person" || m.origin === "unknown")) {
        const t = clean(m.text, LIMITS.text);
        const prev = clean(lastAssistantText?.text ?? "", LIMITS.context);
        redactions += t.redactions + prev.redactions;
        const files = turn.calls ? turn : prevTurn;
        episodes.push({
          ...base,
          id: `${session.harness}:${session.id}:${m.i}`,
          lens: "correction",
          ts: m.ts,
          origin: m.origin,
          confidence: m.confidence,
          basis: m.basis,
          opening: !lastAssistantText,
          text: t.text,
          context: { title: title.text, task: m.text === opening ? "" : task.text, previous: prev.text },
          files: { reads: [...files.reads].slice(0, 40), edits: [...files.edits].slice(0, 40) },
          toolCalls: 0,
        });
      }
      if (m.origin === "person" || m.origin === "unknown") lastPersonText = m;
      if (m.origin !== "harness") {
        if (turn.calls) prevTurn = turn;
        span = freshSpan();
        turn = freshSpan();
      }
      continue;
    }

    if (m.role === "assistant") {
      lastAssistantText = m;
      if (m.phase === "commentary") {
        // Redact each note whole: shortening the joined notes can split a secret.
        const r = redact(m.text);
        redactions += r.count;
        notes.push(r.text);
        lastCommentary = m;
        continue;
      }
      const isReport = session.kind === "subagent" && (m.interAgent || k === finalText);
      let text = m.text;
      if (notes.length) {
        text = `${m.text}\n\nProgress notes from this turn:\n${shorten(notes.join("\n\n"), LIMITS.notes)}`;
        notes = [];
        lastCommentary = null;
      }
      if (k >= from && (text.length >= MIN_DISCOVERY_CHARS || isReport)) emitAssistant(m, text, isReport);
    }
  }
  flushNotes();
  return { episodes, redactions };
}

function lastTextIndex(msgs) {
  for (let k = msgs.length - 1; k >= 0; k--) if (msgs[k].role === "assistant" && msgs[k].kind === "text") return k;
  return -1;
}
