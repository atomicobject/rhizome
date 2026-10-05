import type { Fetcher } from "@graphiql/toolkit";
import { GraphiQL } from "graphiql";
import "graphiql/style.css";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import JsonWorker from "monaco-editor/esm/vs/language/json/json.worker?worker";
import GraphQLWorker from "./graphql.worker?worker";
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

function GraphQLExplorer() {
  return (
    <main className="graphql-explorer">
      <GraphiQL
        fetcher={graphQLExplorerFetcher}
        defaultQuery={GRAPHQL_EXPLORER_QUERY}
        initialVariables={GRAPHQL_EXPLORER_VARIABLES}
      />
    </main>
  );
}

export default GraphQLExplorer;
