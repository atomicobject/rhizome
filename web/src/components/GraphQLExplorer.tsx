import type { Fetcher } from "@graphiql/toolkit";
import { GraphiQL } from "graphiql";
import "graphiql/style.css";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import JsonWorker from "monaco-editor/esm/vs/language/json/json.worker?worker";
import GraphQLWorker from "./graphql.worker?worker";
import { useEffect } from "react";
import { useAppearance } from "../lib/appearance";
import { vaultLabel } from "../lib/labels";
import { installMonacoEnvironment } from "./monacoEnvironment";

installMonacoEnvironment(globalThis, {
  editor: () => new EditorWorker(),
  graphql: () => new GraphQLWorker(),
  json: () => new JsonWorker(),
});

export const GRAPHQL_EXPLORER_QUERY = `# Rhizome public GraphQL explorer
query RhizomePublicAPI {
  validation(firstIssues: 5) {
    ok
    issueCount
    checks {
      name
      ok
      issueCount
      issues {
        code
        path
        message
      }
    }
  }
  notes(find: "docs", first: 10) {
    pageInfo {
      returnedCount
      truncated
      queryShapeVersion
    }
    nodes {
      nodeKind
      path
      title
      resolvedType
    }
  }
}`;

export const GRAPHQL_EXPLORER_VARIABLES = "{}";

export const graphQLExplorerFetcher: Fetcher = async (graphQLParams) => {
  const response = await fetch("/api/v1/graphql", {
    method: "POST",
    headers: {
      "content-type": "application/json",
      accept: "application/json",
    },
    body: JSON.stringify(graphQLParams),
  });

  if (!response.ok) {
    throw new Error(
      `GraphQL workspace request failed (${response.status}). Check the local Rhizome service and retry.`,
    );
  }

  return response.json();
};

type MonacoThemes = {
  editor: {
    defineTheme(name: string, data: ReturnType<typeof rhizomeMonacoTheme>): void;
    setTheme(name: string): void;
  };
};

// GraphiQL's Monaco themes inherit VS Code comment and widget colours, which
// miss AA on Rhizome's dark surface and bring a pink accent. These restate
// them from the syntax and surface tokens under GraphiQL's own theme names.
function rhizomeMonacoTheme(dark: boolean) {
  const pick = (light: string, darkValue: string) => (dark ? darkValue : light);
  const surface = pick("#ffffff", "#1c1b1a");
  const tint = pick("#f5f5f3", "#242321");

  return {
    base: dark ? "vs-dark" : "vs",
    inherit: true,
    rules: [
      { token: "comment", foreground: pick("706c68", "a8a29b") },
      { token: "argument.identifier.gql", foreground: pick("5a4a87", "c4b8e3") },
    ],
    colors: {
      "editor.background": "#ffffff00",
      "scrollbar.shadow": "#ffffff00",
      focusBorder: pick("#c8313a", "#ff6b72"),
      "editorWidget.background": surface,
      "editorHoverWidget.background": surface,
      // Line numbers take the faint-ink role; the current line, muted ink.
      "editorLineNumber.foreground": pick("#6b6965", "#9c9994"),
      "editorLineNumber.activeForeground": pick("#5b5955", "#b8b5b0"),
      "editorSuggestWidget.background": surface,
      "editorSuggestWidget.selectedBackground": tint,
      "editorSuggestWidget.selectedForeground": pick("#1f1e1d", "#ecebe9"),
      "editorSuggestWidget.highlightForeground": pick("#1f1e1d", "#ecebe9"),
      "list.highlightForeground": pick("#1f1e1d", "#ecebe9"),
      "list.hoverBackground": tint,
      "menu.background": surface,
      "textLink.foreground": pick("#0b6a66", "#5fd6d0"),
    },
  };
}

/** Restyles GraphiQL's Monaco themes once GraphiQL has loaded Monaco. */
function useRhizomeMonacoTheme(appearance: "light" | "dark") {
  useEffect(() => {
    let frame = 0;

    const apply = () => {
      // SAFETY: GraphiQL's Monaco store assigns the loaded monaco-editor module
      // to globalThis.__MONACO, which exposes this editor API.
      const monaco = (globalThis as { __MONACO?: MonacoThemes }).__MONACO;

      if (!monaco) {
        frame = requestAnimationFrame(apply);

        return;
      }

      monaco.editor.defineTheme("graphiql-DARK", rhizomeMonacoTheme(true));
      monaco.editor.defineTheme("graphiql-LIGHT", rhizomeMonacoTheme(false));
      monaco.editor.setTheme(appearance === "dark" ? "graphiql-DARK" : "graphiql-LIGHT");
    };

    apply();

    return () => cancelAnimationFrame(frame);
  }, [appearance]);
}

function GraphQLExplorer({ vaultName }: { vaultName?: string | null }) {
  // GraphiQL's own theme toggle would disagree with the Appearance setting.
  const { resolved } = useAppearance();
  useRhizomeMonacoTheme(resolved);

  return (
    <div className="graphql-workspace">
      <main className="graphql-explorer" aria-labelledby="graphql-explorer-title">
        <header className="view-header">
          <div className="view-header__title-row">
            <h2 id="graphql-explorer-title">GraphQL</h2>
            <span className="view-header__about">
              Query {vaultName ? vaultLabel(vaultName) : "this vault"} through Rhizome&rsquo;s
              public API.
            </span>
          </div>
        </header>
        <div className="graphql-explorer__body">
          <GraphiQL
            forcedTheme={resolved}
            fetcher={graphQLExplorerFetcher}
            defaultQuery={GRAPHQL_EXPLORER_QUERY}
            initialVariables={GRAPHQL_EXPLORER_VARIABLES}
            defaultEditorToolsVisibility={false}
          >
            {/* The header names the vault, so the wordmark slot stays empty. */}
            <GraphiQL.Logo>{null}</GraphiQL.Logo>
          </GraphiQL>
        </div>
      </main>
    </div>
  );
}

export default GraphQLExplorer;
