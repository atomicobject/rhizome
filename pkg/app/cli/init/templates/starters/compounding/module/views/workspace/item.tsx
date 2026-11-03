// One compounding recommendation, named by the identity param `item`, with
// its quick actions. Health links here.
import { useRef, useState } from "react";
import { openView, useActionCommand, useActionQuery, useViewParams } from "@rhizome/kit";
import { Button } from "@rhizome/ui";
import {
  ItemDetail,
  RESPONSE_LABEL,
  RemoveControl,
  errorText,
  type Recommendation,
} from "./parts.tsx";

export default function CompoundingItem() {
  const { item } = useViewParams() as { item?: string };
  if (!item)
    return <p className="p-4 text-faint">Open a recommendation from the compounding workspace.</p>;
  return <Item itemRef={item} />;
}

function Item({ itemRef }: { itemRef: string }) {
  const query = useActionQuery("compounding/item", { ref: itemRef });
  const promptCommand = useActionCommand("compounding/prompt");
  const [note, setNote] = useState<string>();
  const [removed, setRemoved] = useState(false);
  const x = query.data as (Recommendation & { current?: boolean }) | undefined;
  // The revision first shown; a poll that brings a redraft is refused at copy and remove time.
  const shown = useRef<string>();
  if (!shown.current && x?.revision) shown.current = x.revision;
  const toWorkspace = () =>
    openView(
      "compounding-workspace",
      { kind: "standalone" },
      { tab: "recommendations", item: itemRef },
    );

  const copyPrompt = async () => {
    let prompt: string;
    try {
      ({ prompt } = (await promptCommand.mutateAsync({
        items: [shown.current ? `${itemRef}@${shown.current}` : itemRef],
      })) as { prompt: string });
    } catch (err) {
      setNote(`Could not build the prompt: ${errorText(err)}`);
      return;
    }
    try {
      await navigator.clipboard.writeText(prompt);
      setNote("Copied the prompt. Paste it into your agent.");
    } catch {
      // No clipboard access, as in an insecure context: copy by hand.
      setNote(prompt);
    }
  };

  return (
    <main className="px-5 py-3 text-base">
      <div className="mb-2 flex items-baseline gap-2">
        <h1 className="min-w-0 flex-1 truncate text-xl font-bold">{x?.title ?? itemRef}</h1>
        {x ? (
          <span className="shrink-0 text-sm text-muted-foreground">
            {RESPONSE_LABEL[x.response ?? "none"]} {x.kind}
          </span>
        ) : null}
      </div>
      {removed ? (
        <p className="text-sm text-muted-foreground" role="status">
          Removed from your review.{" "}
          <button className="text-link" onClick={toWorkspace}>
            Back to the workspace
          </button>
        </p>
      ) : (
        <>
          <div className="mb-2 flex flex-wrap items-center gap-1">
            <Button
              variant="primary"
              size="sm"
              disabled={!x?.current || promptCommand.isPending}
              onClick={() => void copyPrompt()}
            >
              Copy prompt
            </Button>
            <Button size="sm" variant="ghost" onClick={toWorkspace}>
              Open in the workspace
            </Button>
            {x?.current ? (
              <RemoveControl item={x} revision={shown.current} onRemoved={() => setRemoved(true)} />
            ) : null}
          </div>
          {note ? (
            <pre
              className="mb-2 whitespace-pre-wrap rounded bg-sunk p-2 font-mono text-xs"
              role="status"
            >
              {note}
            </pre>
          ) : null}
          <ItemDetail itemRef={itemRef} />
        </>
      )}
    </main>
  );
}
