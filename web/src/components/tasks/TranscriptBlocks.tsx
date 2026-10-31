import type { ReactNode } from "react";
import ReactMarkdown from "react-markdown";

import { decodeJson, isJsonObject, isString } from "../../api/parse";

import { StatusMark } from "../StatusMark";
import { UnifiedDiff } from "../UnifiedDiff";
import {
  ChevronIcon,
  FileIcon,
  LogIcon,
  RetryIcon,
  TerminalIcon,
  ThinkingIcon,
  ToolIcon,
} from "./TaskIcons";
import { formatDuration } from "./taskFormat";
import type { TimelineBlock } from "./transcript";

/** Which blocks are open, keyed by block key; the run view owns it across polls. */
export type Expansion = {
  isOpen: (key: string) => boolean;
  toggle: (key: string, open: boolean) => void;
};

type BlockOf<K extends TimelineBlock["kind"]> = Extract<TimelineBlock, { kind: K }>;

function Disclosure({
  id,
  expansion,
  className,
  icon,
  summary,
  meta,
  children,
}: {
  id: string;
  expansion: Expansion;
  className: string;
  icon: ReactNode;
  summary: ReactNode;
  meta?: ReactNode;
  children: ReactNode;
}) {
  const open = expansion.isOpen(id);

  return (
    <details
      className={`task-block ${className}`}
      open={open}
      onToggle={(event) => {
        if (event.currentTarget.open !== open) expansion.toggle(id, event.currentTarget.open);
      }}
    >
      <summary className="task-block__summary">
        <span className="task-block__chevron">
          <ChevronIcon />
        </span>
        <span className="task-block__icon">{icon}</span>
        <span className="task-block__title">{summary}</span>
        {meta ? <span className="task-block__meta">{meta}</span> : null}
      </summary>
      {/* Bodies render only while open, so a long transcript stays light. */}
      {open ? <div className="task-block__body">{children}</div> : null}
    </details>
  );
}

function firstLine(text: string): string {
  return text.trim().split("\n", 1)[0] ?? "";
}

function Output({ text, truncated, empty }: { text: string; truncated: boolean; empty: string }) {
  return (
    <>
      {text ? (
        <pre className="task-output">{text}</pre>
      ) : (
        <p className="task-block__empty">{empty}</p>
      )}
      {truncated ? (
        <p className="task-block__notice">Output truncated at 16 KB; the rest was not recorded.</p>
      ) : null}
    </>
  );
}

function SpanMeta({
  block,
  status,
}: {
  block: BlockOf<"command" | "tool" | "file">;
  status: ReactNode;
}) {
  const duration = formatDuration(block.startedAt, block.endedAt);

  return (
    <>
      {duration ? <span className="task-block__duration">{duration}</span> : null}
      {status}
    </>
  );
}

function phaseStatus(block: BlockOf<"command" | "tool" | "file">) {
  if (!block.endedAt) return <StatusMark tone="progress" label="Running" />;

  if (block.phase === "failed") return <StatusMark tone="risk" label="Failed" />;

  if (block.phase === "declined" || block.phase === "cancelled")
    return (
      <StatusMark tone="muted" label={block.phase === "declined" ? "Declined" : "Cancelled"} />
    );

  return null;
}

export function CheckLogBlock({ log, expansion }: { log: string; expansion: Expansion }) {
  const lines = log.trimEnd().split("\n").length;

  return (
    <Disclosure
      id="check-log"
      expansion={expansion}
      className="task-block--log"
      icon={<LogIcon />}
      summary="Check stderr"
      meta={
        <span className="task-block__duration">{lines === 1 ? "1 line" : `${lines} lines`}</span>
      }
    >
      <pre className="task-output">{log}</pre>
    </Disclosure>
  );
}

export function ThinkingBlock({
  block,
  expansion,
}: {
  block: BlockOf<"thinking">;
  expansion: Expansion;
}) {
  return (
    <Disclosure
      id={block.key}
      expansion={expansion}
      className="task-block--thinking"
      icon={<ThinkingIcon />}
      summary={
        <>
          <span className="task-block__label">Thinking</span>
          <span className="task-block__preview">
            {firstLine(block.content).replace(/\*\*/g, "")}
          </span>
        </>
      }
    >
      <div className="task-thinking">
        <ReactMarkdown>{block.content}</ReactMarkdown>
      </div>
    </Disclosure>
  );
}

function exitStatus(block: BlockOf<"command">) {
  if (block.exitCode === undefined || block.exitCode === null) return phaseStatus(block);

  return (
    <StatusMark tone={block.exitCode === 0 ? "success" : "risk"} label={`exit ${block.exitCode}`} />
  );
}

export function CommandBlock({
  block,
  expansion,
}: {
  block: BlockOf<"command">;
  expansion: Expansion;
}) {
  const line = firstLine(block.command);
  const multiline = block.command.trim() !== line;

  return (
    <Disclosure
      id={block.key}
      expansion={expansion}
      className="task-block--command"
      icon={<TerminalIcon />}
      summary={<code className="task-block__command">{line || "Command"}</code>}
      meta={<SpanMeta block={block} status={exitStatus(block)} />}
    >
      {multiline ? <pre className="task-output task-output--command">{block.command}</pre> : null}
      {block.cwd ? <p className="task-block__cwd">in {block.cwd}</p> : null}
      <Output
        text={block.output}
        truncated={block.truncated}
        empty={block.endedAt ? "No output." : "Waiting for output…"}
      />
    </Disclosure>
  );
}

/** The tool input's most telling value, such as a path or a query. */
function inputHint(input: string): string {
  if (!input.trim().startsWith("{")) return firstLine(input);
  const parsed = decodeJson(input, isJsonObject);
  const value = parsed ? Object.values(parsed).find(isString) : undefined;

  return value ? firstLine(value) : "";
}

export function ToolBlock({ block, expansion }: { block: BlockOf<"tool">; expansion: Expansion }) {
  const hint = inputHint(block.input);

  return (
    <Disclosure
      id={block.key}
      expansion={expansion}
      className="task-block--tool"
      icon={<ToolIcon />}
      summary={
        <>
          <span className="task-block__label">{block.name}</span>
          {hint ? (
            <code className="task-block__preview task-block__preview--mono">{hint}</code>
          ) : null}
        </>
      }
      meta={<SpanMeta block={block} status={phaseStatus(block)} />}
    >
      {block.input ? (
        <section className="task-block__section">
          <h4>Input</h4>
          <pre className="task-output">{block.input}</pre>
        </section>
      ) : null}
      <section className="task-block__section">
        <h4>Output</h4>
        <Output
          text={block.output}
          truncated={block.truncated}
          empty={block.endedAt ? "No output." : "Waiting for the result…"}
        />
      </section>
    </Disclosure>
  );
}

export function FileChangeBlock({
  block,
  expansion,
}: {
  block: BlockOf<"file">;
  expansion: Expansion;
}) {
  const [first, ...rest] = block.paths;

  return (
    <Disclosure
      id={block.key}
      expansion={expansion}
      className="task-block--file"
      icon={<FileIcon />}
      summary={
        <>
          <span className="task-block__label">Changed</span>
          <code className="task-block__preview task-block__preview--mono">
            {first ?? "files"}
            {rest.length ? ` +${rest.length} more` : ""}
          </code>
        </>
      }
      meta={<SpanMeta block={block} status={phaseStatus(block)} />}
    >
      {block.paths.length > 1 ? (
        <ul className="task-paths">
          {block.paths.map((path) => (
            <li key={path}>
              <code>{path}</code>
            </li>
          ))}
        </ul>
      ) : null}
      {block.diff ? (
        <UnifiedDiff diff={block.diff} className="task-diff" />
      ) : block.input ? (
        <pre className="task-output">{block.input}</pre>
      ) : null}
      {block.output ? <Output text={block.output} truncated={block.truncated} empty="" /> : null}
    </Disclosure>
  );
}

export function TimelineEntry({
  block,
  expansion,
}: {
  block: TimelineBlock;
  expansion: Expansion;
}) {
  switch (block.kind) {
    case "text":
      return (
        <article className="task-message" aria-label="Agent message">
          <span className="task-message__label">Agent</span>
          <ReactMarkdown>{block.content}</ReactMarkdown>
        </article>
      );
    case "thinking":
      return <ThinkingBlock block={block} expansion={expansion} />;
    case "command":
      return <CommandBlock block={block} expansion={expansion} />;
    case "tool":
      return <ToolBlock block={block} expansion={expansion} />;
    case "file":
      return <FileChangeBlock block={block} expansion={expansion} />;
    case "error":
      return block.retrying ? (
        <p className="task-line task-line--muted">
          <RetryIcon />
          <span>Retrying: {block.message}</span>
        </p>
      ) : (
        <div className="task-error" role="note">
          <StatusMark tone="risk" label="Error" />
          <p>{block.message}</p>
        </div>
      );
    case "turn-failed":
      return (
        <div className="task-error" role="note">
          <StatusMark
            tone="risk"
            label={block.phase === "declined" ? "Turn declined" : "Turn failed"}
          />
          {block.status ? <p>{block.status}</p> : null}
        </div>
      );
    case "truncated":
      return (
        <p className="task-line task-line--muted">
          The transcript reached its 10 MB limit, so later events were dropped. The final message
          and token usage are kept.
        </p>
      );
    case "other":
      return (
        <Disclosure
          id={block.key}
          expansion={expansion}
          className="task-block--other"
          icon={<ToolIcon />}
          summary={<span className="task-block__label">{block.type}</span>}
        >
          {block.detail ? <pre className="task-output">{block.detail}</pre> : null}
        </Disclosure>
      );
  }
}
