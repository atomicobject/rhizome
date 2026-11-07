import { NodeLink } from "@rhizome/kit";
import { inlineText, paragraphs } from "./lib/prose.ts";
import type { relationships } from "./lib/knowledge.ts";

type Graph = ReturnType<typeof relationships>;

export function LinkedText({ text, graph }: { text: string; graph: Graph }) {
  return inlineText(text, graph.resolve).map((part, i) => {
    if (part.kind === "text") return part.text;
    const record = part.kind === "link" && graph.all.find((r) => r.path === part.path);
    return record ? (
      <NodeLink key={i} node={record.ref}>
        {part.label}
      </NodeLink>
    ) : (
      <span
        key={i}
        className="ak-unresolved"
        title={`Unresolved target: ${part.kind === "unresolved" ? part.target : part.path}`}
      >
        {part.label} <span className="ak-unresolved-label">(unresolved)</span>
      </span>
    );
  });
}

export function ClaimProse({ text, graph }: { text: string; graph: Graph }) {
  return (
    <div className="ak-claim">
      {paragraphs(text).map((paragraph, i) => (
        <p key={i}>
          <LinkedText text={paragraph} graph={graph} />
        </p>
      ))}
      {!text.trim() && <p>None recorded</p>}
    </div>
  );
}
