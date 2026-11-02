// Pieces the workspace and item views share: one recommendation's detail and
// its quick actions. Every call goes through the module's actions.
import { useState } from "react";
import {
  Dash,
  NoteLink,
  SectionBody,
  sectionStatus,
  useActionCommand,
  useActionQuery,
} from "@rhizome/kit";
import { Button, Input } from "@rhizome/ui";

export type Recommendation = {
  ref: string;
  kind: "proposal" | "finding";
  response: string | null;
  title: string;
  summary: string;
  cluster?: string;
  tier?: string;
  families?: number;
  status?: string;
  revision?: string;
  path?: string;
  assignee?: string | null;
  actionItem?: string | null;
};

export const RESPONSES = ["remember", "document", "fix", "guard", "ask"];
export const RESPONSE_LABEL: Record<string, string> = {
  remember: "Remember",
  document: "Document",
  fix: "Fix",
  guard: "Guard",
  ask: "Ask",
  none: "No response yet",
};

export const errorText = (error: unknown) =>
  (error as { reason?: string })?.reason ??
  (error instanceof Error ? error.message : String(error));

// A person link as findings write it, `[[docs/people/sam]]`, shown by name.
export function Person({ link }: { link?: string | null }) {
  const path = link?.match(/^\[\[(.+?)(\|.*)?\]\]$/)?.[1];
  if (!path) return link ? <span>{link}</span> : <Dash />;
  return <NoteLink path={`${path}.md`}>{path.split("/").at(-1)}</NoteLink>;
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[7rem_1fr] gap-2 py-0.5">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 whitespace-pre-wrap break-words">{children}</dd>
    </div>
  );
}

const Mono = ({ children }: { children: React.ReactNode }) => (
  <pre className="max-h-48 overflow-auto whitespace-pre-wrap rounded bg-sunk p-2 font-mono text-xs">
    {children}
  </pre>
);

function DraftedArtifact({ p }: { p: Record<string, any> }) {
  switch (p.response) {
    case "document":
      return p.edit ? (
        <Field label="Edit">
          <NoteLink path={p.edit.document}>{p.edit.document}</NoteLink>
          <Mono>{`- ${p.edit.old}\n+ ${p.edit.new}`}</Mono>
        </Field>
      ) : null;
    case "fix":
    case "guard":
      return (
        <>
          {p.change ? <Field label="Change">{p.change}</Field> : null}
          {p.guardRule ? (
            <Field label="Guard rule">
              <Mono>{JSON.stringify(p.guardRule, null, 2)}</Mono>
            </Field>
          ) : null}
        </>
      );
    case "ask":
      return p.question ? (
        <Field label="Question">
          {p.question}
          {p.owner ? <span className="text-muted-foreground"> · owner {p.owner}</span> : null}
        </Field>
      ) : null;
    default:
      return p.appliesTo?.length ? (
        <Field label="Applies to">{p.appliesTo.join(", ")}</Field>
      ) : null;
  }
}

// One recommendation in full, loaded when it is shown.
export function ItemDetail({ itemRef }: { itemRef: string }) {
  const query = useActionQuery("compounding/item", { ref: itemRef });
  const d = query.data as Record<string, any> | undefined;
  return (
    <SectionBody status={sectionStatus(query)} what="the recommendation">
      {() =>
        d?.kind === "finding" ? (
          <dl className="text-sm">
            <Field label="Note">
              <NoteLink path={d.path}>{d.path}</NoteLink>
            </Field>
            <Field label="Action item">{d.actionItem ?? <Dash />}</Field>
            <Field label="Assignee">
              <Person link={d.assignee} />
            </Field>
            <Mono>{d.note}</Mono>
          </dl>
        ) : d ? (
          <dl className="text-sm">
            <Field label="Statement">{d.proposal.statement}</Field>
            {d.proposal.why ? <Field label="Why">{d.proposal.why}</Field> : null}
            {d.proposal.responseReason ? (
              <Field label="Reason">{d.proposal.responseReason}</Field>
            ) : null}
            <DraftedArtifact p={d.proposal} />
            {d.proposal.workaround ? (
              <Field label="Workaround">Teaches agents to work around a defect.</Field>
            ) : null}
            {d.proposal.rejected ? (
              <Field label="Rejected">
                {d.proposal.rejected.reason}
                {d.proposal.rejected.notes ? `: ${d.proposal.rejected.notes}` : ""}
              </Field>
            ) : null}
            <Field label="Evidence">
              {d.proposal.families} families, tier {d.proposal.tier}
            </Field>
            {d.sources.map((s: Record<string, any>, k: number) => (
              <details key={k} className="py-0.5">
                <summary className="cursor-pointer text-muted-foreground">
                  Source {k + 1}: {s.harness} session {s.session}, {String(s.ts ?? "").slice(0, 10)}
                </summary>
                <Mono>{s.text}</Mono>
              </details>
            ))}
          </dl>
        ) : null
      }
    </SectionBody>
  );
}

const REASONS = ["Not relevant", "Wrong", "Already fixed"];

// Dismisses a proposal locally with a quick reason, or hides a finding here.
// Neither touches a tracked file.
export function RemoveControl({
  item,
  onRemoved,
}: {
  item: Recommendation;
  onRemoved?: () => void;
}) {
  const dismiss = useActionCommand("compounding/dismiss");
  const hide = useActionCommand("compounding/hide");
  const [open, setOpen] = useState(false);
  const [typed, setTyped] = useState("");
  const [error, setError] = useState<string>();
  const run = async (call: () => Promise<unknown>) => {
    setError(undefined);
    try {
      await call();
      setOpen(false);
      onRemoved?.();
    } catch (err) {
      setError(errorText(err));
    }
  };
  const busy = dismiss.isPending || hide.isPending;
  const dismissWith = (reason: string) =>
    run(() => dismiss.mutateAsync({ cluster: item.cluster, reason }));

  if (item.kind === "finding")
    return (
      <span className="inline-flex items-center gap-2">
        {error ? <span className="text-xs text-risk">{error}</span> : null}
        <Button
          size="sm"
          variant="ghost"
          disabled={busy}
          title="Hide it from this workspace. The shared action item stays open."
          onClick={() => run(() => hide.mutateAsync({ path: item.path }))}
        >
          Hide
        </Button>
      </span>
    );

  return (
    <span className="inline-flex flex-wrap items-center justify-end gap-1">
      {error ? <span className="text-xs text-risk">{error}</span> : null}
      {open ? (
        <>
          {REASONS.map((r) => (
            <Button
              key={r}
              size="sm"
              variant="ghost"
              disabled={busy}
              onClick={() => dismissWith(r.toLowerCase())}
            >
              {r}
            </Button>
          ))}
          <form
            className="inline-flex gap-1"
            onSubmit={(e) => {
              e.preventDefault();
              if (typed.trim()) void dismissWith(typed.trim());
            }}
          >
            <Input
              className="h-[26px] w-40"
              aria-label="Another reason"
              placeholder="Another reason"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
            />
          </form>
          <Button size="sm" variant="ghost" onClick={() => setOpen(false)}>
            Cancel
          </Button>
        </>
      ) : (
        <Button
          size="sm"
          variant="ghost"
          title="Dismiss it for you; teammates still see it."
          onClick={() => setOpen(true)}
        >
          Remove
        </Button>
      )}
    </span>
  );
}
