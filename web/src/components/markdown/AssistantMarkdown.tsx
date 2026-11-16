import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

const REMARK_PLUGINS = [remarkGfm];

// A wide table scrolls inside its message instead of widening the transcript.
const COMPONENTS: Components = {
  table: ({ node: _node, ...props }) => (
    <div className="assistant-table">
      <table {...props} />
    </div>
  ),
};

/** An agent's Markdown answer, with GitHub tables, in the chat pane or on the Agent page. */
export function AssistantMarkdown({ children }: { children: string }) {
  return (
    <ReactMarkdown remarkPlugins={REMARK_PLUGINS} components={COMPONENTS}>
      {children}
    </ReactMarkdown>
  );
}
