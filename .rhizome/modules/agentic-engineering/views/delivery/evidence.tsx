import { useMemo } from "react";
import type { ComponentProps, ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  DisclosureIcon,
  usePageNavigation,
  useViewActive,
  resolveRenderedLinkTarget,
  getNodePreview,
  noteHref,
} from "@rhizome/kit";
import { Markdown, convertWikilinks } from "@rhizome/kit/markdown";
import type { LiveNode } from "./data.tsx";
import { Notice } from "./presentation.tsx";

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
  const noteLink = href?.startsWith("rhizome://note/");
  const target =
    !noteLink && href && (href.startsWith("//") || URL.canParse(href))
      ? null
      : resolveRenderedLinkTarget(href, noteLink ? null : noIndexedLinks);
  const authoredTarget = noteLink
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
      <span title={`Link unavailable in this read: ${target}`}>
        {children}
        <span className="ae-note" aria-hidden="true">
          {" "}
          (unavailable)
        </span>
        <span className="ae-visually-hidden" role="status">
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
        src && (src.startsWith("//") || URL.canParse(src)) ? (
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
  const issues =
    node.workspace?.fields.flatMap((field) =>
      (field.issues ?? []).map((issue) => ({ name: field.name, issue })),
    ) ?? [];
  return (
    <>
      {issues.length > 0 && (
        <Notice>
          {issues.map(({ name, issue }, index) => (
            <p role="status" key={`${name}-${index}`}>
              <code className="ae-path">{name}</code>: <code className="ae-path">{issue.code}</code>
              . {issue.message}
            </p>
          ))}
        </Notice>
      )}
      {node.workspace?.fields
        .filter((field) => field.name === "specSetFrozen" || field.name === "storiesInScopeFrozen")
        .map((field) => (
          <p className="ae-note" key={field.name}>
            <code className="ae-path">{field.name}</code>: recorded field{" "}
            {field.present ? "present" : "absent"}, {field.sectionNodes?.length ?? 0} returned
            sections.
          </p>
        ))}
      <details>
        <summary>
          <DisclosureIcon open={false} />
          Recorded fields and source issues
        </summary>
        {node.workspace ? (
          node.workspace.fields.map((field) => (
            <div className="ae-row" key={field.name}>
              <code className="ae-path">{field.name}</code>
              <p className="ae-note">{field.present ? "present" : "absent"}</p>
              {field.values?.map((value, index) => (
                <p key={index}>{value}</p>
              ))}
              {field.issues?.map((issue, index) => (
                <p role="status" key={index}>
                  <code className="ae-path">{issue.code}</code>: {issue.message}
                </p>
              ))}
              {field.links
                ?.filter((link) => !link.resolved)
                .map((link, index) => (
                  <p key={index}>
                    Unresolved recorded target <code className="ae-path">{link.value}</code>
                  </p>
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
