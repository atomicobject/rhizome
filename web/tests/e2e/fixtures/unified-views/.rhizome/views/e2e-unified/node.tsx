import {
  openNode,
  useSetField,
  useViewContext,
  useViewPreference,
  useViewRows,
} from "@rhizome/kit";

export default function NodeWorkspace({ configuration }) {
  const context = useViewContext();

  const details = useViewPreference("demo.details", {
    slot: "details",
    defaultValue: configuration?.detailsOpen ?? true,
    validate: (value) => value === true || value === false,
  });

  const rows = useViewRows(
    context.kind === "node" && context.type === "ViewDemoTask"
      ? "e2e.views.tasks"
      : "e2e.views.efforts",
    { variant: "table" },
  );

  const writer = useSetField();

  const tasks = useViewRows("e2e.views.tasks", { variant: "table" });

  const relatedTask =
    context.kind === "node" && context.type === "ViewDemoEffort"
      ? tasks.data?.rows.find((item) => item.ref.notePath === context.ref.notePath)
      : undefined;

  const row =
    context.kind === "node"
      ? rows.data?.rows.find(
          (item) =>
            item.ref.notePath === context.ref.notePath &&
            (item.ref.fragment ?? "") === (context.ref.fragment ?? ""),
        )
      : undefined;

  return (
    <main>
      <h1>Demo node workspace</h1>
      <output aria-label="View context">{JSON.stringify(context)}</output>
      <p>{configuration?.greeting}</p>
      <button
        aria-expanded={details.value}
        disabled={details.loading}
        onClick={() => details.set(!details.value)}
      >
        Node details
      </button>
      <output aria-label="Preference status">
        {details.loading ? "Loading" : details.pending ? "Saving" : "Saved"}
      </output>
      <button disabled={details.loading} onClick={() => details.reset()}>
        Reset node details
      </button>
      {details.error && <p role="alert">{details.error.message}</p>}
      {rows.error && <p role="alert">{rows.error.message}</p>}
      {relatedTask && (
        <button
          onClick={() => openNode(relatedTask.ref, { view: "e2e.views.task-node", beside: true })}
        >
          Open embedded task beside
        </button>
      )}
      {row && (
        <>
          <h2>{row.title}</h2>
          <output aria-label="Node status">{JSON.stringify(row.fields.status)}</output>
          <button
            onClick={() => writer.mutate({ target: row.ref, field: "status", value: "active" })}
          >
            Stage active
          </button>
          {writer.error && <p role="alert">{writer.error.message}</p>}
        </>
      )}
    </main>
  );
}
