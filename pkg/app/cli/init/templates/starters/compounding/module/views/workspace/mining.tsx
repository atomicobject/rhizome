import {
  Count,
  Fact,
  Facts,
  Section,
  sectionStatus,
  setViewParams,
  useActionQuery,
} from "@rhizome/kit";

type Verification = {
  examined: number;
  proposed: number;
  rejected: Record<string, number>;
  pending: number;
  complete: boolean;
  deferred: { count: number; reasons: Record<string, number> };
  contractProblems: number;
  next: { command: string; reason: string } | null;
};

export type MiningData = {
  store: { root: string; path: string; mode: "repository" | "isolated" | "task-test" };
  verification: Verification;
  lastRun: {
    run: string;
    status: string;
    spend: { usd: number } | null;
    stoppedReason: string | null;
    stoppedMessage: string | null;
    finishedAt: string | null;
    report: {
      episodes?: { read: number; kept: number };
      candidates?: { extracted: number };
      verified?: {
        planned: number;
        proposed: number;
        rejected: Record<string, number>;
        contractProblems: number;
      };
      held?: { tierC: number; tierBOverBudget: number };
      verification?: Verification;
      proposals?: number;
    } | null;
  } | null;
};

const REASON_LABEL: Record<string, string> = {
  count_limit: "verification count limit",
  budget: "spend limit",
  contract: "verifier response needs retry",
};
const reasonLabel = (reason: string) => REASON_LABEL[reason] ?? reason.replaceAll("_", " ");

export function MiningStatus({ detail = false }: { detail?: boolean }) {
  const query = useActionQuery("compounding/health", {});
  const data = query.data as MiningData | undefined;
  const run = data?.lastRun;
  const queue = data?.verification;
  const summary = run
    ? `Latest mining run: ${run.status}${queue?.pending ? `; ${queue.pending} awaiting evidence verification` : ""}.`
    : "No mining run recorded in this store.";
  if (!detail)
    return (
      <div className="mb-2 text-sm text-muted-foreground" role="status">
        {query.isPending
          ? "Loading mining status…"
          : query.isError
            ? "Mining status unavailable."
            : summary}{" "}
        <button className="text-link" onClick={() => setViewParams({ tab: "health" })}>
          View Health and store
        </button>
      </div>
    );
  const report = run?.report;
  const verified = report?.verification ?? report?.verified;
  const rejected = verified?.rejected ?? {};
  const rejectedCount = Object.values(rejected).reduce((sum, n) => sum + n, 0);
  const examined =
    report?.verification?.examined ??
    (report?.verified ? report.verified.proposed + rejectedCount : null);
  const held = (report?.held?.tierC ?? 0) + (report?.held?.tierBOverBudget ?? 0);
  return (
    <Section title="Mining" status={sectionStatus(query)} what="mining status">
      {() =>
        data ? (
          <>
            <p className="text-sm">{summary}</p>
            {run ? (
              <>
                <p className="mt-1 text-sm text-muted-foreground break-words">
                  Run {run.run}
                  {run.finishedAt ? ` · ${run.finishedAt}` : ""}
                </p>
                {run.stoppedReason ? (
                  <p className="mt-1 text-sm text-risk">
                    Stopped: {run.stoppedReason}. {run.stoppedMessage}
                  </p>
                ) : null}
                <Facts>
                  <Fact value={report?.episodes?.read ?? "—"}>episodes read</Fact>
                  <Fact value={report?.candidates?.extracted ?? "—"}>candidates extracted</Fact>
                  <Fact value={examined ?? "—"}>examined</Fact>
                  <Fact value={verified?.proposed ?? "—"}>proposed</Fact>
                  <Fact value={verified ? rejectedCount : "—"}>rejected</Fact>
                  <Fact value={verified?.contractProblems ?? "—"}>invalid responses</Fact>
                  <Fact value={run.spend ? `$${run.spend.usd.toFixed(4)}` : "—"}>run spend</Fact>
                </Facts>
                {Object.keys(rejected).length ? (
                  <ul className="text-sm text-muted-foreground">
                    {Object.entries(rejected).map(([reason, n]) => (
                      <li key={reason}>
                        {reasonLabel(reason)}: <Count>{n}</Count>
                      </li>
                    ))}
                  </ul>
                ) : null}
                {!report?.verification && held ? (
                  <p className="mt-1 text-sm">
                    This historical run held {held} clusters without evidence verification
                    {report?.held?.tierC ? ` (${report.held.tierC} below the former Jev gate)` : ""}
                    .
                  </p>
                ) : null}
              </>
            ) : null}
            <div className="mt-2 text-sm">
              <p>
                {queue?.pending
                  ? `${queue.pending} clusters still await evidence verification in this store.`
                  : "No clusters await evidence verification in this store."}
              </p>
              {queue?.pending ? (
                <ul className="text-muted-foreground">
                  {Object.entries(queue.deferred.reasons).map(([reason, n]) => (
                    <li key={reason}>
                      {reasonLabel(reason)}: <Count>{n}</Count>
                    </li>
                  ))}
                </ul>
              ) : null}
              {queue?.next ? (
                <div className="mt-1">
                  <p>
                    {queue.next.reason} Run this from the checkout below through the compounding
                    skill, choosing an approved spend limit.
                  </p>
                  <pre className="mt-1 whitespace-pre-wrap break-words rounded bg-sunk p-2 font-mono text-xs">
                    {queue.next.command}
                  </pre>
                </div>
              ) : null}
            </div>
            <dl className="mt-2 break-words text-sm text-muted-foreground">
              <dt>Checkout</dt>
              <dd>{data.store.root}</dd>
              <dt className="mt-1">Mining store ({data.store.mode})</dt>
              <dd>{data.store.path}</dd>
            </dl>
            <p className="mt-1 text-sm text-muted-foreground">
              This workspace reads this store only. Isolated replay history appears here only when
              this is its store. Recommendations contain reviewable proposals and findings.
            </p>
          </>
        ) : null
      }
    </Section>
  );
}
