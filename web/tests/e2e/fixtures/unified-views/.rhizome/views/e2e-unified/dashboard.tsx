import { openNode, openView, useGraphQL, useViewContext, useViewRows } from "@rhizome/kit";

export default function Dashboard({ configuration }) {
  const context = useViewContext();
  const efforts = useViewRows("e2e.views.efforts", { variant: "table" });
  const tasks = useViewRows("e2e.views.tasks", { variant: "table" });

  const queried = useGraphQL(`query NavigationRefs {
    viewDemoEffort {
      title
      ref { notePath kind fragment nodeId typeName structuralFingerprint: structural }
    }
  }`);

  return (
    <main>
      <h1>Demo dashboard</h1>
      <output aria-label="View context">{JSON.stringify(context)}</output>
      <p>{configuration?.greeting}</p>
      {queried.error && <p role="alert">{queried.error.message}</p>}
      <ul aria-label="GraphQL effort rows">
        {queried.data?.viewDemoEffort.map((record) => (
          <li key={record.ref.notePath}>
            <output aria-label={`GraphQL ref ${record.title}`}>{JSON.stringify(record.ref)}</output>
            <button onClick={() => openNode(record.ref, { view: "e2e.views.node" })}>
              GraphQL open {record.title}
            </button>
            <button
              onClick={() =>
                openView("e2e.views.node", {
                  kind: "node",
                  type: "ViewDemoEffort",
                  ref: record.ref,
                })
              }
            >
              GraphQL view {record.title}
            </button>
          </li>
        ))}
      </ul>
      <button
        onClick={() =>
          window.parent.postMessage(
            {
              type: "rhizome:open-node",
              ref: { notePath: "a.md", kind: "NOTE", fragment: 7 },
            },
            window.location.origin,
          )
        }
      >
        Send invalid ref
      </button>
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
