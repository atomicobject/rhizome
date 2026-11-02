import { setViewParams, useViewParams } from "@rhizome/kit";
import { useState } from "react";

// A mount counter shows whether a param change remounted the view.
let mounts = 0;

export default function ParamsDemo() {
  const params = useViewParams();
  const [mount] = useState(() => (mounts += 1));

  return (
    <main>
      <h1>Params demo</h1>
      <p aria-label="Params">{`tab=${params.tab} item=${params.item}`}</p>
      <p aria-label="Mount">{`started ${String(window.performance.timeOrigin)} mount ${mount}`}</p>
      <button type="button" onClick={() => setViewParams({ tab: "second" })}>
        Second tab
      </button>
      <button type="button" onClick={() => setViewParams({ item: "two" })}>
        Item two
      </button>
    </main>
  );
}
