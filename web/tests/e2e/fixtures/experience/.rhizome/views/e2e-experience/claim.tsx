import { setViewParams, useViewContext, useViewParams } from "@rhizome/kit";

// The frame's document start: the same for every page one experience frame
// renders, so a journey can tell a reused frame from a new one.
const frameStart = String(window.performance.timeOrigin);

export default function ClaimPage() {
  const context = useViewContext();
  const { section } = useViewParams();
  const path = context.kind === "node" ? context.ref.notePath : "";

  return (
    <main>
      <h1>{`Claim page ${path.split("/").at(-1)}`}</h1>
      <p aria-label="Frame">{frameStart}</p>
      <p aria-label="Section">{`section=${section}`}</p>
      <button type="button" onClick={() => setViewParams({ section: "evidence" })}>
        Show evidence
      </button>
    </main>
  );
}
