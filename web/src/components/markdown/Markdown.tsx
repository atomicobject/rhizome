import { isValidElement, memo, useEffect, useId, useMemo, useState } from "react";
import ReactMarkdown, { type Components, type Options } from "react-markdown";
import remarkGfm from "remark-gfm";
import { renderMermaid } from "./mermaid";

type MermaidState =
  | { status: "loading" }
  | { status: "rendered"; svg: string }
  | { status: "error"; message: string };

function MermaidBlock({ code }: { code: string }) {
  const reactId = useId();
  const [state, setState] = useState<MermaidState>({ status: "loading" });

  useEffect(() => {
    let active = true;
    setState({ status: "loading" });
    const id = `mermaid-${reactId.replace(/[^a-zA-Z0-9_-]/g, "")}`;
    void renderMermaid(code, id).then(
      (svg) => {
        if (active) setState({ status: "rendered", svg });
      },
      (error: Error) => {
        if (!active) return;
        setState({
          status: "error",
          message: error.message,
        });
      },
    );

    return () => {
      active = false;
    };
  }, [code, reactId]);

  if (state.status === "loading") {
    return (
      <div className="mermaid-block mermaid-block--loading" role="status">
        Rendering diagram…
      </div>
    );
  }

  if (state.status === "error") {
    return (
      <div className="mermaid-block mermaid-block--error" role="alert">
        <pre>
          <code>{code}</code>
        </pre>
        <p>Could not render diagram: {state.message}</p>
      </div>
    );
  }

  return <div className="mermaid-block" dangerouslySetInnerHTML={{ __html: state.svg }} />;
}

const defaultComponents: Components = {
  code: ({ node: _node, className, children, ...props }) => {
    if (className?.split(/\s+/).includes("language-mermaid")) {
      return <MermaidBlock code={String(children).replace(/\n$/, "")} />;
    }

    return (
      <code className={className} {...props}>
        {children}
      </code>
    );
  },
  pre: ({ node: _node, children, ...props }) => {
    if (
      isValidElement<{ className?: string }>(children) &&
      children.props.className?.split(/\s+/).includes("language-mermaid")
    ) {
      return children;
    }

    return <pre {...props}>{children}</pre>;
  },
};

type MarkdownProps = Omit<Options, "components" | "rehypePlugins" | "remarkPlugins"> & {
  components?: Components;
};

type RehypePlugin = NonNullable<Options["rehypePlugins"]>[number];

const REMARK_PLUGINS = [remarkGfm];

const NO_REHYPE_PLUGINS: RehypePlugin[] = [];

// Syntax highlighting (highlight.js grammars) loads on first use instead of in
// the entry chunk. rehype-highlight only changes fenced code (it does not
// detect languages), so a note without a fence never needs it.
const FENCE = /^ {0,3}(```|~~~)/m;

let highlightPlugins: RehypePlugin[] | null = null;

let highlightLoad: Promise<RehypePlugin[]> | null = null;

function loadHighlight() {
  highlightLoad ??= import("rehype-highlight").then((module) => {
    highlightPlugins = [module.default];

    return highlightPlugins;
  });

  return highlightLoad;
}

function useRehypePlugins(source: string): RehypePlugin[] {
  const needed = FENCE.test(source);
  const [plugins, setPlugins] = useState(() => highlightPlugins);

  useEffect(() => {
    if (!needed || plugins) return;
    let active = true;
    void loadHighlight().then((loaded) => {
      if (active) setPlugins(loaded);
    });

    return () => {
      active = false;
    };
  }, [needed, plugins]);

  return needed && plugins ? plugins : NO_REHYPE_PLUGINS;
}

/**
 * Renders Markdown through the unified pipeline. Memoized: the pipeline runs
 * again only when the source or a prop changes, so callers pass stable
 * `components` and `urlTransform` values.
 */
export const Markdown = memo(function Markdown({ components, children, ...props }: MarkdownProps) {
  const merged = useMemo(() => ({ ...defaultComponents, ...components }), [components]);
  const rehypePlugins = useRehypePlugins(children ?? "");

  return (
    <ReactMarkdown
      {...props}
      remarkPlugins={REMARK_PLUGINS}
      rehypePlugins={rehypePlugins}
      components={merged}
    >
      {children}
    </ReactMarkdown>
  );
});
