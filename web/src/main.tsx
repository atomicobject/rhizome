import { QueryClientProvider } from "@tanstack/react-query";
import { createRoot } from "react-dom/client";

import { appQueryClient } from "./api/queryClient";
import { AppShell } from "./components/AppShell";
import { DiagnosticRegion } from "./diagnostics/DiagnosticRegion";
import { startClientDiagnostics } from "./diagnostics/startClientDiagnostics";
import { VaultInvalidationBridge } from "./query/VaultInvalidationBridge";

import "./base.css";
import "./agent.css";
import "./explorer.css";
import "./ontology.css";
import "./configured-view.css";
import "./markdown-editor.css";
import "./notes-shell.css";
import "./finder.css";
import "./components/notePreview/notePreview.css";

const rootElement = document.getElementById("root");

if (!rootElement) {
  throw new Error("missing #root mount element");
}

startClientDiagnostics(appQueryClient);

const root = createRoot(rootElement);

root.render(
  <QueryClientProvider client={appQueryClient}>
    <VaultInvalidationBridge />
    <DiagnosticRegion name="app-shell">
      <AppShell />
    </DiagnosticRegion>
  </QueryClientProvider>,
);
