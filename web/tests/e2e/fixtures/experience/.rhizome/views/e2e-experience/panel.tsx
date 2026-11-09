import { useState } from "react";
import { useViewContext } from "@rhizome/kit";

// The panel frame's document start, so a journey can tell a kept frame from a new one.
const frameStart = String(window.performance.timeOrigin);

export default function ExperiencePanel() {
  const context = useViewContext();
  // Local state, which survives subject changes while the page stays mounted.
  const [marks, setMarks] = useState(0);

  const subject =
    context.kind === "node"
      ? (context.ref.notePath.split("/").at(-1) ?? "")
      : context.kind === "group"
        ? context.group
        : context.kind;

  return (
    <main>
      <p aria-label="Panel subject">{subject}</p>
      <p aria-label="Panel frame">{frameStart}</p>
      <button type="button" onClick={() => setMarks((count) => count + 1)}>
        {`Marked ${marks}`}
      </button>
    </main>
  );
}
