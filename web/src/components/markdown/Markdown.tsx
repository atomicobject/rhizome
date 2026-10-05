import { isValidElement, useEffect, useId, useState } from "react";
import ReactMarkdown, { type Components, type Options } from "react-markdown";
import rehypeHighlight from "rehype-highlight";
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

export function Markdown({ components, ...props }: MarkdownProps) {
  return (
    <ReactMarkdown
      {...props}
      remarkPlugins={[remarkGfm]}
      rehypePlugins={[rehypeHighlight]}
      components={{ ...defaultComponents, ...components }}
    />
  );
}
