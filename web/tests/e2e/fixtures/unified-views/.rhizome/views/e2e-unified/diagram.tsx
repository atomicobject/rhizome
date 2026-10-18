import { Diagram, mermaid, useGraphQL } from "@rhizome/kit";

type Data = {
  viewDemoEffort: Array<{ title: string; ref: { notePath: string; kind: string } }>;
};

const QUERY = `query DiagramEfforts {
  viewDemoEffort {
    title
    ref { notePath kind }
  }
}`;

export default function DiagramDemo() {
  const { data, error } = useGraphQL<Data>(QUERY);

  const source = data
    ? `flowchart LR\n${data.viewDemoEffort
        .map((record) => `${mermaid.id(record.ref.notePath)}["${mermaid.text(record.title)}"]`)
        .join("\n")}`
    : "";

  const links = Object.fromEntries(
    data?.viewDemoEffort.map((record) => [mermaid.id(record.ref.notePath), record.ref]) ?? [],
  );

  return (
    <main>
      <h1>Diagram demo</h1>
      {error && <p role="alert">{error.message}</p>}
      <Diagram label="Effort diagram" source={source} links={links} />
    </main>
  );
}
