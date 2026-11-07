import { useGraphQL, useViewContext } from "@rhizome/kit";

export default function ModelRecord() {
  const context = useViewContext();
  const ref =
    context.kind === "node"
      ? context.ref.nodeId || `${context.ref.notePath}#${context.ref.fragment}`
      : "";
  const result = useGraphQL<{ node: { title: string } | null }>(
    "query ModelTitle($ref: String!) { node(ref: $ref) { title } }",
    { ref },
    { enabled: Boolean(ref) },
  );
  return (
    <main>
      <h1>{result.data?.node?.title || "Model record"}</h1>
      <p>Model record presentation pending</p>
    </main>
  );
}
