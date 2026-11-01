import { useCallback, useMemo, useState } from "react";
import ReactMarkdown from "react-markdown";

import type { TaskRunView as TaskRun } from "../../api/tasks";
import { StatusMark } from "../StatusMark";
import { BackIcon, CopyIcon } from "./TaskIcons";
import {
  formatDuration,
  formatFullDateTime,
  formatTokens,
  outcomePresentation,
  shortCommit,
} from "./taskFormat";
import { CheckLogBlock, type Expansion, TimelineEntry } from "./TranscriptBlocks";
import { buildTimeline, finalMessage, lastUsage } from "./transcript";
import { useRunTranscript } from "./useTasksData";

function CopyPath({ path }: { path: string }) {
  const [copied, setCopied] = useState(false);

  return (
    <span className="task-copy">
      <code title={path}>{path}</code>
      <button
        type="button"
        className="task-icon-button"
        aria-label={copied ? "Copied worktree path" : "Copy worktree path"}
        title="Copy path"
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(path);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1500);
          } catch {
            setCopied(false);
          }
        }}
      >
        <CopyIcon />
        <span className="task-copy__done" aria-hidden="true">
          {copied ? "Copied" : ""}
        </span>
      </button>
    </span>
  );
}

function useExpansion(): Expansion & { setAll: (keys: string[], open: boolean) => void } {
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set());

  const toggle = useCallback((key: string, next: boolean) => {
    setOpen((current) => {
      const copy = new Set(current);

      if (next) copy.add(key);
      else copy.delete(key);

      return copy;
    });
  }, []);

  const setAll = useCallback((keys: string[], next: boolean) => {
    setOpen(next ? new Set(keys) : new Set());
  }, []);

  return { isOpen: (key) => open.has(key), toggle, setAll };
}

export function TaskRunView({
  runID,
  fallback,
  live,
  interrupted,
  onBack,
}: {
  runID: string;
  /** The history row, shown until the detail arrives. */
  fallback?: TaskRun;
  live: boolean;
  /** The run lock's holder is gone, so the running run is interrupted. */
  interrupted: boolean;
  onBack: () => void;
}) {
  const { detail, error, refreshError, loading, retry } = useRunTranscript(
    runID,
    live,
    interrupted,
  );

  const expansion = useExpansion();
  const events = useMemo(() => detail?.events ?? [], [detail?.events]);
  const blocks = useMemo(() => buildTimeline(events), [events]);
  const run = detail?.run ?? fallback;
  const message = finalMessage(events);
  const usage = lastUsage(events);
  const checkLog = detail?.checkLog;

  const outcome = live ? "running" : interrupted ? "interrupted" : run?.outcome || "running";

  const { label, tone } = outcomePresentation(outcome);
  const duration = formatDuration(run?.startedAt, live ? undefined : run?.at);
  const collapsible = [...(checkLog ? ["check-log"] : []), ...blocks.map((block) => block.key)];
  const anyOpen = collapsible.some((key) => expansion.isOpen(key));
  const failedMessage = !message && run?.message && outcome === "failed" ? run.message : "";
  const shownMessage = message ?? (failedMessage ? "" : run?.message);

  return (
    <article className="task-run" aria-label={`Run ${runID}`}>
      <header className="task-run__head">
        <button type="button" className="task-back" onClick={onBack}>
          <BackIcon />
          History
        </button>
        <StatusMark tone={tone} label={label} />
        {run?.stage && live ? (
          <span className="task-run__stage">
            {run.stage === "check" ? "Running check" : "Agent working"}
          </span>
        ) : null}
        <span className="task-run__when">
          {formatFullDateTime(run?.startedAt || run?.at)}
          {duration ? ` · ${duration}` : ""}
        </span>
        <code className="task-run__id" title="Run ID">
          {runID}
        </code>
      </header>
      <div className="task-run__scroll">
        <section className="task-result" aria-label="Result">
          {failedMessage ? (
            <div className="task-error" role="note">
              <StatusMark tone="risk" label="Failed" />
              <p>{failedMessage}</p>
            </div>
          ) : null}
          {shownMessage ? (
            <div className="task-result__message">
              <ReactMarkdown>{shownMessage}</ReactMarkdown>
            </div>
          ) : live ? (
            <p className="task-result__pending">
              The agent's final message appears here when the run finishes.
            </p>
          ) : null}
          <dl className="task-facts">
            {run?.branch ? (
              <>
                <dt>Branch</dt>
                <dd>
                  <code>{run.branch}</code>
                </dd>
              </>
            ) : null}
            {run?.worktree ? (
              <>
                <dt>Worktree</dt>
                <dd>
                  <CopyPath path={run.worktree} />
                </dd>
              </>
            ) : null}
            {run?.commit ? (
              <>
                <dt>Base commit</dt>
                <dd>
                  <code title={run.commit}>{shortCommit(run.commit)}</code>
                </dd>
              </>
            ) : null}
            {run?.tokens || usage?.totalTokens ? (
              <>
                <dt>Tokens</dt>
                <dd className="task-facts__number">
                  {formatTokens(run?.tokens || usage?.totalTokens)}
                  {usage?.inputTokens || usage?.outputTokens ? (
                    <span className="task-facts__aside">
                      {formatTokens(usage.inputTokens)} in · {formatTokens(usage.outputTokens)} out
                    </span>
                  ) : null}
                </dd>
              </>
            ) : null}
            {run?.item ? (
              <>
                <dt>Inbox item</dt>
                <dd>
                  <code>{run.item}</code>
                </dd>
              </>
            ) : null}
            {run?.changed?.length ? (
              <>
                <dt>Changed</dt>
                <dd>
                  <ul className="task-paths">
                    {run.changed.map((path) => (
                      <li key={path}>
                        <code>{path}</code>
                      </li>
                    ))}
                  </ul>
                </dd>
              </>
            ) : null}
          </dl>
        </section>
        <section className="task-timeline" aria-label="Timeline">
          <div className="task-timeline__head">
            <h3>Timeline</h3>
            {collapsible.length ? (
              <button
                type="button"
                className="task-ghost-button"
                onClick={() => expansion.setAll(collapsible, !anyOpen)}
              >
                {anyOpen ? "Collapse all" : "Expand all"}
              </button>
            ) : null}
          </div>
          {loading ? (
            <div className="task-skeleton" role="status" aria-label="Loading the transcript">
              <span />
              <span />
              <span />
            </div>
          ) : error ? (
            <p className="task-inline-error" role="alert">
              Could not load this run. {error instanceof Error ? error.message : String(error)}{" "}
              <button type="button" className="task-ghost-button" onClick={retry}>
                Retry
              </button>
            </p>
          ) : (
            <div className="task-timeline__list">
              {refreshError ? (
                <p className="task-inline-error" role="alert">
                  Could not refresh this run.{" "}
                  {refreshError instanceof Error ? refreshError.message : String(refreshError)}{" "}
                  <button type="button" className="task-ghost-button" onClick={retry}>
                    Retry
                  </button>
                </p>
              ) : null}
              {checkLog ? <CheckLogBlock log={checkLog} expansion={expansion} /> : null}
              {blocks.map((block) => (
                <TimelineEntry key={block.key} block={block} expansion={expansion} />
              ))}
              {!checkLog && blocks.length === 0 ? (
                <p className="task-line task-line--muted">
                  {live
                    ? "Waiting for the first block…"
                    : run?.hasTranscript === false
                      ? "No agent work ran, so there is no transcript."
                      : "This run recorded nothing."}
                </p>
              ) : null}
              {live && blocks.length ? (
                <p className="task-line task-line--muted task-line--live" role="status">
                  Blocks appear as they complete.
                </p>
              ) : null}
            </div>
          )}
        </section>
      </div>
    </article>
  );
}
