import { QueryClientProvider } from "@tanstack/react-query";
import { createRoot } from "react-dom/client";

import { appQueryClient } from "./api/queryClient";
import { AppShell } from "./components/AppShell";
import { VaultInvalidationBridge } from "./query/VaultInvalidationBridge";

import "highlight.js/styles/github.css";
import "./base.css";
import "./agent.css";
import "./explorer.css";
import "./ontology.css";
import "./configured-view.css";
import "./markdown-editor.css";
import "./notes-shell.css";
import "./components/notePreview/notePreview.css";

const rootElement = document.getElementById("root");

if (!rootElement) {
  throw new Error("missing #root mount element");
}

const root = createRoot(rootElement);

root.render(
  <QueryClientProvider client={appQueryClient}>
    <VaultInvalidationBridge />
    <AppShell />
  </QueryClientProvider>,
);
