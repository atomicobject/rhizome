import type { GraphViewNode } from "./graphViewShared";
import { noteLabelLines } from "./sigmaLabelText";

const measure = (text: string) => {
  const context =
    typeof document === "undefined" ? null : document.createElement("canvas").getContext("2d");

  if (!context?.measureText) return text.length * 7;
  context.font = "500 14px Inter, -apple-system, system-ui, sans-serif";

  return context.measureText(text).width;
};

/** Remove repeated context before sacrificing the part that distinguishes a file. */
export function fileFocusLabels(nodes: GraphViewNode[], width = measure): GraphViewNode[] {
  const names = nodes.map((node) =>
    (node.kind === "code" && node.path
      ? node.path.split("/").pop() || node.label
      : node.label
    ).replaceAll("...", "…"),
  );

  const codes = names.filter((_, i) => nodes[i].kind === "code" && !nodes[i].aggregate);
  let prefix = codes[0] || "";

  for (const name of codes) while (!name.startsWith(prefix)) prefix = prefix.slice(0, -1);
  prefix = prefix.slice(0, Math.max(prefix.lastIndexOf("_"), prefix.lastIndexOf("-")) + 1);
  let suffix = codes[0] || "";

  for (const name of codes) while (!name.endsWith(suffix)) suffix = suffix.slice(1);
  suffix = suffix.includes("_test.")
    ? suffix.slice(suffix.indexOf("_test."))
    : suffix.startsWith(".")
      ? suffix
      : "";

  const labels = names.map((name, i) => {
    if (nodes[i].kind !== "code" || width(name) <= 240) return name;
    let label = name;

    if (codes.length > 1 && prefix) label = `…${label.slice(prefix.length)}`;

    if (codes.length > 1 && suffix) label = `${label.slice(0, -suffix.length)}…`;

    while (width(label) > 240 && label.length > 2)
      label = `${label.slice(0, Math.ceil(label.length / 2) - 1)}…${label.slice(-Math.floor(label.length / 2) + 1)}`;

    return label;
  });

  return nodes.map((node, i) => {
    const duplicates = nodes.filter((_, j) => labels[j] === labels[i]);
    const path = node.notePath || node.path || "";
    const parents = path.split("/").slice(0, -1);
    let depth = 1;

    while (
      depth < parents.length &&
      duplicates.some(
        (other) =>
          other.id !== node.id &&
          (other.notePath || other.path || "").split("/").slice(0, -1).slice(-depth).join("/") ===
            parents.slice(-depth).join("/"),
      )
    )
      depth++;

    return {
      ...node,
      label: labels[i],
      labelDetail:
        duplicates.length > 1 ? `${parents.slice(-depth).join("/") || "."}/` : node.labelDetail,
    };
  });
}

export function fileFocusRowHeight(node: GraphViewNode, width = measure) {
  return (
    (node.kind === "note" || node.kind === "embedded"
      ? noteLabelLines(node.label, width).length
      : 1) *
      42 +
    50
  );
}
