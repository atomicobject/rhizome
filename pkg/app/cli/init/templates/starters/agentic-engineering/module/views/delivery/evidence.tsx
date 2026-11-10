import { useMemo } from "react";
import type { ComponentProps, ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Markdown,
  usePageNavigation,
  useViewActive,
  convertWikilinks,
  resolveRenderedLinkTarget,
  getNodePreview,
  noteHref,
} from "@rhizome/kit";
import type { LiveNode } from "./data.tsx";

const preserveLinkURL = (url: string) => url;
const noIndexedLinks = { links: [] };

function SourceAnchor({
  href,
  children,
  from,
}: {
  href?: string;
  children?: ReactNode;
  from: string;
}) {
  const target = resolveRenderedLinkTarget(
    href,
    href?.startsWith("rhizome://note/") ? null : noIndexedLinks,
  );
  const authoredTarget = href?.startsWith("rhizome://note/")
    ? `[[${target}]]`
    : `[target](${href?.replaceAll("(", "%28").replaceAll(")", "%29")})`;
  const nav = usePageNavigation();
  const active = useViewActive();
  const query = useQuery({
    queryKey: ["agentic-engineering", "source-link", from, authoredTarget],
    queryFn: ({ signal }) => getNodePreview(authoredTarget, from, { signal }),
    enabled: active && target !== null,
    subscribed: active,
    retry: false,
  });
  if (!target)
    return (
      <a href={href} target="_blank" rel="noreferrer">
        {children}
      </a>
    );
  if (query.isPending)
    return (
      <span>
        {children} <span className="ae-note">(resolving link)</span>
      </span>
    );
  if (query.error || !query.data || !query.data.fragmentResolved)
    return (
      <span>
        {children}{" "}
        <span className="ae-note" role="status">
          (recorded link unavailable in this read: {target})
        </span>
      </span>
    );
  const destination = query.data.ref;
  return (
    <a
      href={noteHref(destination)}
      onClick={(event) => {
        if (event.button !== 0 || event.altKey || event.shiftKey) return;
        event.preventDefault();
        nav.openNote(destination, { beside: event.metaKey || event.ctrlKey });
      }}
    >
      {children}
    </a>
  );
}

export function SourceContent({ node }: { node: LiveNode }) {
  const nav = usePageNavigation();
  const components = useMemo<NonNullable<ComponentProps<typeof Markdown>["components"]>>(
    () => ({
      a: ({ href, children }) => (
        <SourceAnchor href={href} from={node.ref.notePath}>
          {children}
        </SourceAnchor>
      ),
      img: ({ src, alt, title }) =>
        src && (src.startsWith("/") || URL.canParse(src)) ? (
          <img
            src={src}
            alt={alt ?? ""}
            title={title}
            style={{ maxWidth: "100%", height: "auto" }}
          />
        ) : (
          <span className="ae-note">
            <span role="status">Image preview unavailable here: {alt || "Recorded image"}.</span>{" "}
            <code>{src || "Source unavailable"}</code>{" "}
            <button
              className="ae-link"
              aria-label={`Open source for ${alt || "recorded image"}`}
              onClick={() => nav.openNode(node.ref, { view: "builtin:source" })}
            >
              Open source
            </button>
          </span>
        ),
    }),
    [node.ref, nav],
  );
  if (node.format === "markdown" || node.sectionType)
    return (
      <div className="ae-prose">
        <Markdown skipHtml components={components} urlTransform={preserveLinkURL}>
          {convertWikilinks(node.content ?? "")}
        </Markdown>
      </div>
    );
  return (
    <div>
      <p className="ae-note">
        {node.format || "Unknown format"} source. This brief shows its source text safely.
      </p>
      <button
        className="ae-link"
        onClick={() =>
          nav.openNode(node.ref, {
            view: node.format === "html" ? "builtin:read" : "builtin:source",
          })
        }
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
