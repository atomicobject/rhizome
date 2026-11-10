import { useMemo } from "react";
import { NodeLink } from "@rhizome/kit";
import { MarkdownDocument } from "@rhizome/kit/markdown";
import type { MarkdownLinkTo } from "@rhizome/kit/markdown";
import { inlineText } from "./lib/prose.ts";
import type { relationships } from "./lib/knowledge.ts";

type Graph = ReturnType<typeof relationships>;

/** Without `graph` (still loading), a link shows as its plain label, never as unresolved. */
export function LinkedText({ text, graph }: { text: string; graph?: Graph }) {
  return inlineText(text, (t) => graph?.resolve(t)).map((part, i) => {
    if (part.kind === "text") return part.text;
    if (!graph) return <span key={i}>{part.label}</span>;
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

/** Authored links in claim text, resolved through the knowledge graph; absent while it loads. */
function useGraphLinks(graph?: Graph) {
  return useMemo(() => {
    if (!graph) return undefined;
    return (target: string): MarkdownLinkTo | undefined => {
      const path = graph.resolve(target);
      const record = path ? graph.all.find((r) => r.path === path) : undefined;
      return record ? { node: record.ref } : undefined;
    };
  }, [graph]);
}

/** A claim's authored text as one Markdown document. */
export function ClaimProse({ text, graph }: { text: string; graph?: Graph }) {
  const resolveLink = useGraphLinks(graph);
  if (!text.trim())
    return (
      <div className="ak-claim">
        <p>None recorded</p>
      </div>
    );
  return <MarkdownDocument className="ak-claim" source={text} resolveLink={resolveLink} />;
}
