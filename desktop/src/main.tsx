import { createRoot } from "react-dom/client";
import { App } from "./App";
import { FinderApp } from "./Finder";
import "./style.css";
document.documentElement.dataset.platform = window.__RHIZOME_PLATFORM__ ?? "macos";
createRoot(document.getElementById("root")!).render(
  window.__RHIZOME_FINDER__ ? <FinderApp /> : <App />,
);
