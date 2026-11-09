import { openNode, setViewParams, useViewParams } from "@rhizome/kit";

// The frame's document start: the same for every page one experience frame
// renders, so a journey can tell a reused frame from a new one.
const frameStart = String(window.performance.timeOrigin);

const claims = ["alpha", "beta", "gamma"];

export default function ExperienceHome() {
  const { sort } = useViewParams();

  return (
    <main>
      <h1>Experience home</h1>
      <p aria-label="Frame">{frameStart}</p>
      <p aria-label="Sort">{`sort=${sort}`}</p>
      <button type="button" onClick={() => setViewParams({ sort: "recent" })}>
        Sort by recent
      </button>
      <ul>
        {claims.map((name) => (
          <li key={name}>
            <button
              type="button"
              onClick={() =>
                openNode({ notePath: `notes/experience/claims/${name}.md`, kind: "NOTE" })
              }
            >
              {`Open ${name}`}
            </button>
          </li>
        ))}
      </ul>
    </main>
  );
}
