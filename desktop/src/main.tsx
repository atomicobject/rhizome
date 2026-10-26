import { createRoot } from "react-dom/client";
import { App } from "./App";
import { FinderApp } from "./Finder";
import "./style.css";
createRoot(document.getElementById("root")!).render(
  window.__RHIZOME_FINDER__ ? <FinderApp /> : <App />,
);
