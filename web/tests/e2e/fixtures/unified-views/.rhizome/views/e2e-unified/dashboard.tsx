import { openNode, useViewContext, useViewRows } from "@rhizome/kit";

export default function Dashboard({ configuration }) {
  const context = useViewContext();
  const efforts = useViewRows("e2e.views.efforts", { variant: "table" });
  const tasks = useViewRows("e2e.views.tasks", { variant: "table" });

  return (
    <main>
      <h1>Demo dashboard</h1>
      <output aria-label="View context">{JSON.stringify(context)}</output>
      <p>{configuration?.greeting}</p>
      {efforts.error && <p role="alert">{efforts.error.message}</p>}
      <ul aria-label="Native effort rows">
        {efforts.data?.rows.map((row) => (
          <li key={row.ref.notePath}>
            <button onClick={() => openNode(row.ref, { view: "e2e.views.node" })}>
              Open {row.title}
            </button>
            <button onClick={() => openNode(row.ref, { view: "e2e.views.node", beside: true })}>
              Beside {row.title}
            </button>
            <span>{JSON.stringify(row.fields.status)}</span>
          </li>
        ))}
      </ul>
      <ul aria-label="Native task rows">
        {tasks.data?.rows.map((row) => (
          <li key={`${row.ref.notePath}#${row.ref.fragment}`}>
            <button onClick={() => openNode(row.ref, { view: "e2e.views.task-node" })}>
              Open {row.title}
            </button>
          </li>
        ))}
      </ul>
      <output aria-label="Native capabilities">{JSON.stringify(efforts.data?.capabilities)}</output>
    </main>
  );
}
