import { Markdown, usePageNavigation } from "@rhizome/kit";
import type { LiveNode } from "./data.tsx";

export function SourceContent({ node }: { node: LiveNode }) {
  const nav = usePageNavigation();
  if (node.format === "markdown") return <Markdown skipHtml>{node.content ?? ""}</Markdown>;
  return (
    <div>
      <p className="ae-note">
        {node.format || "Unknown format"} source. This brief shows its source text safely.
      </p>
      <button
        className="ae-link"
        onClick={() => nav.openNode(node.ref, { view: "builtin:source" })}
      >
        Open original source
      </button>
      <pre className="ae-source">{node.content ?? "Source content unavailable."}</pre>
    </div>
  );
}

export function FieldEvidence({ node }: { node: LiveNode }) {
  return (
    <>
      {node.workspace?.fields.flatMap((field) =>
        (field.issues ?? []).map((issue, index) => (
          <p role="status" key={`${field.name}-${index}`}>
            {field.name}: {issue.code}. {issue.message}
          </p>
        )),
      )}
      {node.workspace?.fields
        .filter((field) => field.name === "specSetFrozen" || field.name === "storiesInScopeFrozen")
        .map((field) => (
          <p className="ae-note" key={field.name}>
            {field.name}: recorded field {field.present ? "present" : "absent"},{" "}
            {field.sectionNodes?.length ?? 0} returned sections.
          </p>
        ))}
      <details>
        <summary>Recorded fields and source issues</summary>
        {node.workspace ? (
          node.workspace.fields.map((field) => (
            <div className="ae-row" key={field.name}>
              <strong>{field.name}</strong>
              <p className="ae-note">{field.present ? "present" : "absent"}</p>
              {field.values?.map((value, index) => (
                <p key={index}>{value}</p>
              ))}
              {field.issues?.map((issue, index) => (
                <p role="status" key={index}>
                  {issue.code}: {issue.message}
                </p>
              ))}
              {field.links
                ?.filter((link) => !link.resolved)
                .map((link, index) => (
                  <p key={index}>Unresolved recorded target {link.value}</p>
                ))}
            </div>
          ))
        ) : (
          <p>Field evidence unavailable.</p>
        )}
      </details>
    </>
  );
}
