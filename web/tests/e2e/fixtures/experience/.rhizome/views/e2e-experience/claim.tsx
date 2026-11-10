import { useEffect } from "react";
import {
  Fact,
  Facts,
  RecordPage,
  setViewParams,
  useMarkReady,
  useViewContext,
  useViewParams,
} from "@rhizome/kit";

// The frame's document start: the same for every page one experience frame
// renders, so a journey can tell a reused frame from a new one.
const frameStart = String(window.performance.timeOrigin);

export default function ClaimPage() {
  const context = useViewContext();
  const { section } = useViewParams();
  const path = context.kind === "node" ? context.ref.notePath : "";
  const markReady = useMarkReady();
  // The page reads nothing asynchronously, so its data is ready once it renders.
  useEffect(() => markReady(), [markReady]);

  return (
    <main>
      <RecordPage
        title={`Claim page ${path.split("/").at(-1)}`}
        facts={
          <Facts note="A claim of the experience">
            <Fact value={2}>sources</Fact>
          </Facts>
        }
      >
        <p aria-label="Frame">{frameStart}</p>
        <p aria-label="Section">{`section=${section}`}</p>
        <button type="button" onClick={() => setViewParams({ section: "evidence" })}>
          Show evidence
        </button>
        <Facts className="e2e-body-facts">
          <Fact value={1}>note</Fact>
        </Facts>
      </RecordPage>
    </main>
  );
}
