// Markdown for custom views (SPEC-0105 US3): the host's renderer behind a closed
// component. One remark plugin recognizes authored note links, and the same
// plugin answers `markdownLinks`, so a view that decides something about a
// document without rendering it agrees with the page that renders it.
import {
  Children,
  cloneElement,
  createElement,
  isValidElement,
  useMemo,
  type ReactNode,
  type Ref,
} from "react";
import type { Components, ExtraProps } from "react-markdown";
import type { Element, ElementContent } from "hast";
import type { Data, Definition, Link, LinkReference, Root } from "mdast";
import remarkGfm from "remark-gfm";
import remarkParse from "remark-parse";
import { unified, type Plugin, type PluggableList } from "unified";
import { SKIP, visit } from "unist-util-visit";

import { isString } from "../src/api/parse";
import { Markdown as HostMarkdown } from "../src/components/markdown/Markdown";
import type { ViewNodeRef } from "../src/views/context";
import { NodeLink } from "./navigation";
import { remarkWikilinks } from "./wikilinks";

/**
 * An internal link as authored: a wikilink target without its alias, or a
 * relative Markdown destination (inline or reference-style), percent-decoded.
 * `offset` is the UTF-16 index of the link's first character in the source.
 */
export type MarkdownLink = { target: string; offset: number };

/** Where an internal link opens. */
export type MarkdownLinkTo = { node: ViewNodeRef; view?: string };

/** A highlight on the innermost rendered block holding the link at `at`. */
export type MarkdownMark = {
  /** A link's `offset` from `markdownLinks`. */
  at: number;
  /** Shown at the top of the highlighted block. */
  label: string;
  /** Receives the highlighted element, for scrolling. */
  ref?: Ref<HTMLElement>;
};

export type MarkdownProps = {
  source: string;
  className?: string;
  /**
   * Absent: internal links render as plain labels while their data loads.
   * Present: a target it returns nothing for renders as explicitly unresolved.
   * Pass a stable function, since the document re-renders when it changes.
   */
  resolveLink?: (target: string) => MarkdownLinkTo | undefined;
  mark?: MarkdownMark;
  /** Hide a leading H1 unless it holds the selected mark. Offsets are unchanged. */
  skipTitle?: boolean;
};

declare module "mdast" {
  interface RootData {
    /** What `rhizomeLinks` found, for `markdownLinks`. */
    rzLinks?: MarkdownLink[];
  }
}

const TARGET = "data-rz-target";

const TITLE = "data-rz-title";

const SCHEME = /^[a-z][a-z0-9+.-]*:/i;

// A relative destination names a note; a scheme or a fragment does not.
function internalTarget(url: string): string | undefined {
  if (!url || url.startsWith("#") || SCHEME.test(url)) return undefined;

  try {
    return decodeURIComponent(url);
  } catch {
    return url;
  }
}

function tag(node: { data?: Data }, property: string, value: string) {
  node.data = { ...node.data, hProperties: { ...node.data?.hProperties, [property]: value } };
}

// Note links in a parsed document: wikilinks split out of text, and relative
// inline or reference links tagged with their target. A leading H1 is tagged
// as the title. The links found are left on the root for `markdownLinks`.
const rhizomeLinks: Plugin<[], Root> = () => (tree) => {
  const definitions = new Map<string, Definition>();

  visit(tree, "definition", (node) => {
    const id = node.identifier.toUpperCase();

    if (!definitions.has(id)) definitions.set(id, node);
  });

  const links: MarkdownLink[] = [];

  const record = (node: Link | LinkReference, target: string) => {
    tag(node, TARGET, target);

    if (node.position?.start.offset !== undefined)
      links.push({ target, offset: node.position.start.offset });
  };

  visit(tree, (node) => {
    if (node.type === "link") {
      const target = internalTarget(node.url);

      if (target !== undefined) record(node, target);

      return SKIP;
    }

    if (node.type === "linkReference") {
      const definition = definitions.get(node.identifier.toUpperCase());
      const target = definition && internalTarget(definition.url);

      if (target !== undefined) record(node, target);

      return SKIP;
    }

    if (node.type === "rzWikilink") {
      node.children = [{ type: "text", value: node.label }];
      node.data = { ...node.data, hName: "a" };
      tag(node, TARGET, node.target);

      if (node.position?.start.offset !== undefined)
        links.push({ target: node.target, offset: node.position.start.offset });

      return SKIP;
    }
  });

  const first = tree.children[0];

  if (first?.type === "heading" && first.depth === 1) tag(first, TITLE, "");

  links.sort((a, b) => a.offset - b.offset);
  tree.data = { ...tree.data, rzLinks: links };
};

const KIT_REMARK: PluggableList = [remarkWikilinks, rhizomeLinks];

const parser = unified().use(remarkParse).use(remarkGfm).use(remarkWikilinks).use(rhizomeLinks);

/**
 * The document's internal links in source order, as `Markdown` renders them.
 * Pure and synchronous. Code spans, fences, indented code, and raw HTML hold
 * no links, and a reference definition by itself is not one.
 */
export function markdownLinks(source: string): MarkdownLink[] {
  return parser.runSync(parser.parse(source), source).data?.rzLinks ?? [];
}

type BlockTag = "p" | "li" | "h1" | "h2" | "h3" | "h4" | "h5" | "h6" | "tr";

// The blocks a mark can land on: the innermost of these holding the link.
const BLOCK_TAGS: readonly BlockTag[] = ["p", "li", "h1", "h2", "h3", "h4", "h5", "h6", "tr"];

const BLOCKS = new Set<string>(BLOCK_TAGS);

function contains(node: Element, at: number) {
  const start = node.position?.start.offset;
  const end = node.position?.end.offset;

  return start !== undefined && end !== undefined && start <= at && at < end;
}

function hasInnerBlock(node: { children: ElementContent[] }, at: number): boolean {
  return node.children.some(
    (child) =>
      child.type === "element" &&
      (BLOCKS.has(child.tagName) ? contains(child, at) : hasInnerBlock(child, at)),
  );
}

const innermost = (node: Element | undefined, at: number) =>
  node !== undefined && contains(node, at) && !hasInnerBlock(node, at);

type BlockProps = ExtraProps & { children?: ReactNode; className?: string };

function withClass(className: string | undefined, added: string) {
  return className ? `${className} ${added}` : added;
}

// A table row keeps its cells; the label goes inside the first one.
function labelledCells(children: ReactNode, label: ReactNode) {
  const cells = Children.toArray(children);

  for (const [index, cell] of cells.entries()) {
    if (!isValidElement<{ children?: ReactNode }>(cell)) continue;
    cells[index] = cloneElement(cell, undefined, label, cell.props.children);

    return cells;
  }

  return [label, ...cells];
}

function blockComponent(tag: BlockTag, mark: MarkdownMark | undefined, skipTitle: boolean) {
  return function Block({ node, children, className, ...rest }: BlockProps) {
    if (
      skipTitle &&
      tag === "h1" &&
      node?.properties[TITLE] !== undefined &&
      (mark === undefined || !contains(node, mark.at))
    )
      return null;

    if (mark === undefined || !innermost(node, mark.at))
      return createElement(tag, { ...rest, className }, children);

    const label = createElement(
      "span",
      { className: "rz-markdown-mark-label", key: "label" },
      mark.label,
    );

    const props = { ...rest, className: withClass(className, "rz-markdown-mark"), ref: mark.ref };

    return tag === "tr"
      ? createElement(tag, props, ...labelledCells(children, label))
      : createElement(tag, props, label, children);
  };
}

type LinkProps = ExtraProps & { children?: ReactNode; href?: string };

type ImageProps = ExtraProps & { src?: string | Blob; alt?: string };

function useComponents(
  resolveLink: MarkdownProps["resolveLink"],
  mark: MarkdownMark | undefined,
  skipTitle: boolean,
): Components {
  const at = mark?.at;
  const label = mark?.label;
  const ref = mark?.ref;

  return useMemo(() => {
    const current = at !== undefined && label !== undefined ? { at, label, ref } : undefined;
    const block = (tag: BlockTag) => blockComponent(tag, current, skipTitle);

    const components: Components = {
      p: block("p"),
      li: block("li"),
      h1: block("h1"),
      h2: block("h2"),
      h3: block("h3"),
      h4: block("h4"),
      h5: block("h5"),
      h6: block("h6"),
      tr: block("tr"),
      a: ({ node, children, href, ...rest }: LinkProps) => {
        const target = node?.properties[TARGET];

        if (isString(target)) {
          if (!resolveLink) return <span>{children}</span>;
          const to = resolveLink(target);

          if (to)
            return (
              <NodeLink node={to.node} view={to.view}>
                {children}
              </NodeLink>
            );

          return (
            <span className="rz-markdown-unresolved" title={`Unresolved target: ${target}`}>
              {children} <span className="rz-markdown-unresolved-label">(unresolved)</span>
            </span>
          );
        }

        // The host's URL rule emptied an unsafe scheme; the label stays as text.
        if (!href) return <>{children}</>;

        if (href.startsWith("#"))
          return (
            <a href={href} {...rest}>
              {children}
            </a>
          );

        return (
          <a href={href} target="_blank" rel="noreferrer noopener" {...rest}>
            {children}
          </a>
        );
      },
      // No image loads from a view: the alt text and the source stand in.
      img: ({ src, alt }: ImageProps) => (
        <span className="rz-markdown-image">
          {alt}
          {isString(src) && src ? (
            <>
              {" "}
              <code>{src}</code>
            </>
          ) : null}
        </span>
      ),
    };

    return components;
  }, [resolveLink, at, label, ref, skipTitle]);
}

/**
 * One whole Markdown document with GitHub-flavored Markdown, highlighted code,
 * and Mermaid, through the host's renderer. Wikilinks and relative Markdown
 * links are internal links that `resolveLink` turns into node links. Raw HTML
 * stays literal, unsafe URL schemes become plain text, and images never load.
 */
export function Markdown({
  source,
  className,
  resolveLink,
  mark,
  skipTitle = false,
}: MarkdownProps) {
  const components = useComponents(resolveLink, mark, skipTitle);

  return (
    <div className={withClass(className, "markdown rz-markdown")}>
      <HostMarkdown remarkPlugins={KIT_REMARK} components={components}>
        {source}
      </HostMarkdown>
    </div>
  );
}
