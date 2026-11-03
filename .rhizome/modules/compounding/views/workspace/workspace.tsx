// The compounding workspace (SPEC-0124 US9): review recommendations as a
// checklist and copy one agent prompt for the selected ones, read the
// friction report, and see the memory the team has approved.
import { useEffect, useState, type Dispatch, type SetStateAction } from "react";
import {
  Count,
  Dash,
  Fact,
  Facts,
  MoreToggle,
  NoteLink,
  Section,
  openView,
  sectionStatus,
  setViewParams,
  useActionCommand,
  useActionQuery,
  useViewParams,
  useViewRows,
} from "@rhizome/kit";
import { Button, Checkbox, Tabs, TabsContent, TabsList, TabsTrigger } from "@rhizome/ui";
import {
  ItemDetail,
  Person,
  RESPONSES,
  RESPONSE_LABEL,
  RemoveControl,
  errorText,
  type Recommendation,
} from "./parts.tsx";

const TABS = ["recommendations", "health", "memory"];

export default function Workspace() {
  const params = useViewParams() as { tab?: string; item?: string };
  const tab = TABS.includes(params.tab ?? "") ? params.tab! : "recommendations";
  // Held here, not in Recommendations, which unmounts on another tab: an
  // item the person unchecked must stay unchecked when they come back.
  const unchecked = useState<Set<string>>(new Set());
  // The revision of each proposal the person has seen; a poll that brings a
  // redraft does not replace it until they open the proposal again.
  const reviewed = useState<Record<string, string>>({});
  return (
    <main className="px-5 py-3 text-base">
      <h1 className="mb-2 text-xl font-bold">Compounding</h1>
      <Tabs value={tab} onValueChange={(next) => setViewParams({ tab: next })}>
        <TabsList>
          <TabsTrigger value="recommendations">Recommendations</TabsTrigger>
          <TabsTrigger value="health">Health</TabsTrigger>
          <TabsTrigger value="memory">Memory</TabsTrigger>
        </TabsList>
        <TabsContent value="recommendations">
          <Recommendations initial={params.item} unchecked={unchecked} reviewed={reviewed} />
        </TabsContent>
        <TabsContent value="health">
          <Health />
        </TabsContent>
        <TabsContent value="memory">
          <Memory />
        </TabsContent>
      </Tabs>
    </main>
  );
}

type Copy =
  | { status: "copied" }
  | { status: "manual"; prompt: string }
  | { status: "error"; message: string };

function Recommendations({
  initial,
  unchecked: [unchecked, setUnchecked],
  reviewed: [reviewed, setReviewed],
}: {
  initial?: string;
  unchecked: [Set<string>, Dispatch<SetStateAction<Set<string>>>];
  reviewed: [Record<string, string>, Dispatch<SetStateAction<Record<string, string>>>];
}) {
  const query = useActionQuery("compounding/recommendations", {});
  const promptCommand = useActionCommand("compounding/prompt");
  const items = ((query.data as { items?: Recommendation[] })?.items ?? []) as Recommendation[];
  const [open, setOpen] = useState<Set<string>>(new Set(initial ? [initial] : []));
  const [copy, setCopy] = useState<Copy>();
  useEffect(() => {
    const unseen = items.filter((x) => x.revision && !(x.ref in reviewed));
    if (unseen.length)
      setReviewed((r) => ({
        ...r,
        ...Object.fromEntries(unseen.map((x) => [x.ref, x.revision!])),
      }));
  }, [items, reviewed, setReviewed]);
  const redrafted = (x: Recommendation) =>
    Boolean(x.revision && reviewed[x.ref] && reviewed[x.ref] !== x.revision);
  // A proposal goes with the revision the person reviewed, so a redraft since is refused.
  const selected = items
    .filter((x) => !unchecked.has(x.ref))
    .map((x) => {
      const revision = reviewed[x.ref] ?? x.revision;
      return revision ? `${x.ref}@${revision}` : x.ref;
    });
  const toggle = (set: Set<string>, ref: string) => {
    const next = new Set(set);
    next.has(ref) ? next.delete(ref) : next.add(ref);
    return next;
  };
  const groups = [...RESPONSES, "none"]
    .map((r) => ({ response: r, items: items.filter((x) => (x.response ?? "none") === r) }))
    .filter((g) => g.items.length);

  const copyPrompt = async () => {
    setCopy(undefined);
    let prompt: string;
    try {
      ({ prompt } = (await promptCommand.mutateAsync({ items: selected })) as { prompt: string });
    } catch (err) {
      setCopy({ status: "error", message: errorText(err) });
      return;
    }
    try {
      await navigator.clipboard.writeText(prompt);
      setCopy({ status: "copied" });
    } catch {
      // No clipboard access, as in an insecure context: copy by hand.
      setCopy({ status: "manual", prompt });
    }
  };

  return (
    <Section
      title="Recommendations"
      count={items.length}
      caption="Checked items go into one prompt for your agent. Removing one changes only your own review."
      status={sectionStatus(query)}
      what="recommendations"
      empty={
        items.length ? undefined : "Nothing to review. The weekly mining task adds proposals here."
      }
      controls={
        <Button
          variant="primary"
          size="sm"
          disabled={!selected.length || promptCommand.isPending}
          onClick={() => void copyPrompt()}
        >
          Copy prompt ({selected.length})
        </Button>
      }
    >
      {() => (
        <>
          {copy?.status === "copied" ? (
            <p className="py-1 text-sm text-muted-foreground" role="status">
              Copied the prompt. Paste it into your agent.
            </p>
          ) : null}
          {copy?.status === "error" ? (
            <p className="py-1 text-sm text-risk" role="alert">
              Could not build the prompt: {copy.message}
            </p>
          ) : null}
          {copy?.status === "manual" ? (
            <div className="py-1">
              <p className="text-sm text-muted-foreground">
                The clipboard is unavailable; copy the prompt from here.
              </p>
              <textarea
                className="mt-1 w-full rounded border bg-sunk p-2 font-mono text-xs"
                aria-label="Agent prompt"
                readOnly
                rows={8}
                value={copy.prompt}
                onFocus={(e) => e.currentTarget.select()}
              />
            </div>
          ) : null}
          {groups.map((g) => (
            <div key={g.response} className="pt-2">
              <h3 className="flex items-baseline gap-2 text-sm font-semibold">
                {RESPONSE_LABEL[g.response]} <Count>{g.items.length}</Count>
              </h3>
              <ul className="divide-y divide-[var(--border-faint)]">
                {g.items.map((x) => (
                  <li key={x.ref} className="py-1">
                    <div className="flex items-center gap-2">
                      <Checkbox
                        aria-label={`Include ${x.title}`}
                        checked={!unchecked.has(x.ref)}
                        onCheckedChange={() => setUnchecked((s) => toggle(s, x.ref))}
                      />
                      <button
                        className="min-w-0 flex-1 truncate text-left"
                        aria-expanded={open.has(x.ref)}
                        onClick={() => {
                          // Opening a proposal shows its current draft, which the person then has reviewed.
                          if (!open.has(x.ref) && x.revision)
                            setReviewed((r) => ({ ...r, [x.ref]: x.revision! }));
                          setOpen((s) => toggle(s, x.ref));
                        }}
                      >
                        {x.title}
                      </button>
                      {redrafted(x) ? (
                        <span className="shrink-0 text-xs text-[var(--warning-ink)]">
                          redrafted; open it to review
                        </span>
                      ) : null}
                      <span className="shrink-0 text-xs text-muted-foreground">
                        {x.kind === "finding" ? (
                          <>
                            finding · <Person link={x.assignee} />
                          </>
                        ) : x.status === "missing" ? (
                          "approved before, file missing"
                        ) : (
                          `proposal · tier ${x.tier} · ${x.families} families`
                        )}
                      </span>
                      <RemoveControl item={x} />
                    </div>
                    {open.has(x.ref) ? (
                      <div className="py-1 pl-7">
                        <ItemDetail itemRef={x.ref} />
                      </div>
                    ) : null}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </>
      )}
    </Section>
  );
}

type HealthRow = {
  ref: string;
  response: string | null;
  statement: string;
  families: number;
  familiesApproximate?: boolean;
  sessions?: number;
  toolCalls?: number;
  lastSeen?: string | null;
  status?: string;
  completedOn?: string | null;
};
type HealthData = {
  open: HealthRow[];
  completed: HealthRow[];
  lastRun: {
    run: string;
    status: string;
    spend: { usd: number } | null;
    stoppedReason: string | null;
    stoppedMessage: string | null;
    finishedAt: string | null;
  } | null;
};

const openItem = (ref: string) =>
  openView("compounding-item", { kind: "standalone" }, { item: ref });

function Health() {
  const query = useActionQuery("compounding/health", {});
  const h = query.data as HealthData | undefined;
  const [more, setMore] = useState(false);
  const recurring = h?.completed.filter((c) => c.status === "recurring") ?? [];
  const open = h?.open ?? [];
  const shown = more ? open : open.slice(0, 10);
  const run = h?.lastRun;
  return (
    <>
      {h ? (
        <Facts
          note={
            run
              ? `Last mine run ${run.run}: ${run.status}${run.stoppedReason ? ` (${run.stoppedReason}: ${run.stoppedMessage})` : ""}.`
              : "No mine run yet."
          }
        >
          <Fact value={open.length}>open</Fact>
          <Fact tone={recurring.length ? "risk" : undefined} value={recurring.length}>
            recurring
          </Fact>
          <Fact value={run?.spend ? `$${run.spend.usd.toFixed(2)}` : "—"}>last run spend</Fact>
        </Facts>
      ) : null}
      <Section
        title="Where agents lose effort"
        count={open.length}
        caption="Open findings and pending proposals, ranked by families, then sessions, then tool calls."
        status={sectionStatus(query)}
        what="the friction report"
        empty={open.length ? undefined : "No open friction."}
      >
        {() => (
          <>
            <HealthRows rows={shown} />
            {open.length > 10 ? (
              <MoreToggle hidden={open.length - 10} open={more} onToggle={() => setMore(!more)} />
            ) : null}
          </>
        )}
      </Section>
      <Section
        title="Completed"
        count={h?.completed.length}
        status={sectionStatus(query)}
        what="completed findings"
        empty={h?.completed.length ? undefined : "No completed findings yet."}
      >
        {() => <HealthRows rows={h!.completed} />}
      </Section>
    </>
  );
}

function HealthRows({ rows }: { rows: HealthRow[] }) {
  return (
    <ul className="divide-y divide-[var(--border-faint)]">
      {rows.map((r) => (
        <li key={r.ref} className="flex items-baseline gap-2 py-1">
          <button
            className="min-w-0 flex-1 truncate text-left text-link"
            onClick={() => openItem(r.ref)}
          >
            {r.statement}
          </button>
          <span className="shrink-0 text-xs text-muted-foreground">
            {RESPONSE_LABEL[r.response ?? "none"]}
            {r.status ? (
              <span className={r.status === "recurring" ? "text-risk" : undefined}>
                {" "}
                · {r.status}
              </span>
            ) : (
              ` · ${r.familiesApproximate ? "~" : ""}${r.families} families · ${r.sessions} sessions · ${r.toolCalls} tool calls`
            )}
          </span>
        </li>
      ))}
    </ul>
  );
}

type Row = {
  ref: { notePath: string };
  path?: string;
  title?: string;
  fields?: Record<string, unknown>;
};

const PAGE = 100;

function Memory() {
  // Each list grows a page at a time with Show more.
  const [shardPages, setShardPages] = useState(1);
  const [findingPages, setFindingPages] = useState(1);
  const shards = useViewRows("context-shards", {
    variant: "table",
    page: { first: PAGE * shardPages },
  });
  const findings = useViewRows("agent-findings", {
    variant: "table",
    page: { first: PAGE * findingPages },
  });
  const check = useActionQuery("compounding/shards-check", {});
  const attention =
    (check.data as { path: string; statement: string; reasons: string[] }[] | undefined) ?? [];
  const shardRows = (shards.data?.rows ?? []) as Row[];
  const findingRows = (findings.data?.rows ?? []) as Row[];
  return (
    <>
      <Section
        title="Needs attention"
        count={attention.length}
        caption="Shards and guard rules to re-verify. Ask your agent to maintain them with the compounding skill."
        status={sectionStatus(check)}
        what="shards to re-verify"
        empty={attention.length ? undefined : "Every shard is current."}
      >
        {() => (
          <ul className="divide-y divide-[var(--border-faint)]">
            {attention.map((a) => (
              <li key={a.path} className="py-1">
                <NoteLink path={a.path}>{a.statement}</NoteLink>
                <span className="text-xs text-risk"> · {a.reasons.join("; ")}</span>
              </li>
            ))}
          </ul>
        )}
      </Section>
      <MemoryRows
        title="Context shards"
        query={shards}
        rows={shardRows}
        total={shards.data?.pageInfo?.total}
        more={shards.data?.pageInfo?.hasMore ? () => setShardPages((n) => n + 1) : undefined}
        field="kind"
        what="context shards"
        empty="No approved shards yet."
      />
      <MemoryRows
        title="Agent findings"
        query={findings}
        rows={findingRows}
        total={findings.data?.pageInfo?.total}
        more={findings.data?.pageInfo?.hasMore ? () => setFindingPages((n) => n + 1) : undefined}
        field="response"
        what="agent findings"
        empty="No findings yet."
      />
    </>
  );
}

function MemoryRows({
  title,
  query,
  rows,
  total,
  more,
  field,
  what,
  empty,
}: {
  title: string;
  query: Parameters<typeof sectionStatus>[0];
  rows: Row[];
  total?: number;
  more?: () => void;
  field: string;
  what: string;
  empty: string;
}) {
  return (
    <Section
      title={title}
      count={total ?? rows.length}
      status={sectionStatus(query)}
      what={what}
      empty={rows.length ? undefined : empty}
    >
      {() => (
        <>
          <ul className="divide-y divide-[var(--border-faint)]">
            {rows.map((r) => (
              <li key={r.ref.notePath} className="flex items-baseline gap-2 py-1">
                <NoteLink path={r.ref.notePath} className="min-w-0 flex-1 truncate">
                  {String(r.fields?.summary ?? r.title ?? r.ref.notePath)}
                </NoteLink>
                <span className="shrink-0 text-xs text-muted-foreground">
                  {r.fields?.[field] ? String(r.fields[field]) : <Dash />}
                </span>
              </li>
            ))}
          </ul>
          {more ? (
            <Button variant="ghost" size="sm" onClick={more}>
              Show more ({rows.length} of {total})
            </Button>
          ) : null}
        </>
      )}
    </Section>
  );
}
