import type { Fetcher } from "@graphiql/toolkit";
import { GraphiQL } from "graphiql";
import "graphiql/style.css";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import JsonWorker from "monaco-editor/esm/vs/language/json/json.worker?worker";
import GraphQLWorker from "./graphql.worker?worker";
import {
  BookOpen,
  Copy,
  History,
  Keyboard,
  type LucideIcon,
  Merge,
  RefreshCw,
  Settings,
  WandSparkles,
} from "lucide-react";
import {
  cloneElement,
  isValidElement,
  type ReactNode,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { useAppearance } from "../lib/appearance";
import { vaultLabel } from "../lib/labels";
import { installMonacoEnvironment } from "./monacoEnvironment";
import { WorkbenchSidebar } from "./WorkbenchSidebar";

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

// GraphiQL's Monaco themes inherit VS Code's syntax and widget colours (bright
// blue, brown, green), which miss AA on Rhizome's dark surface and match
// nothing else in the app. These restate the --syntax-* and surface tokens
// (tokens.css) under GraphiQL's own theme names, in both appearances.
function rhizomeMonacoTheme(dark: boolean) {
  const pick = (light: string, darkValue: string) => (dark ? darkValue : light);
  const surface = pick("#ffffff", "#1c1b1a");
  const tint = pick("#f5f5f3", "#242321");
  const ink = pick("1f1e1d", "ecebe9");
  const meta = pick("5c5853", "b3ada6");
  const keyword = pick("8a3a74", "dca0cb");
  const string = pick("0b6a66", "5fd6d0");
  const number = pick("1f6f91", "8ccbe6");
  const type = pick("3f5a75", "b0c4d9");
  const property = pick("7a5410", "e8c27e");

  return {
    base: dark ? "vs-dark" : "vs",
    inherit: true,
    rules: [
      { token: "", foreground: ink },
      { token: "comment", foreground: pick("706c68", "9a948d") },
      { token: "keyword", foreground: keyword },
      // Field and argument names are most of a query, so they stay ink.
      { token: "key.identifier", foreground: ink },
      { token: "argument.identifier", foreground: property },
      { token: "type.identifier", foreground: type },
      { token: "annotation", foreground: keyword },
      { token: "string", foreground: string },
      { token: "number", foreground: number },
      { token: "delimiter", foreground: meta },
      { token: "operator", foreground: meta },
      // JSON results: keys ink, values by kind.
      { token: "string.key.json", foreground: ink },
      { token: "string.value.json", foreground: string },
      { token: "number.json", foreground: number },
      { token: "keyword.json", foreground: keyword },
      { token: "delimiter.bracket.json", foreground: meta },
    ],
    colors: {
      "editor.foreground": `#${ink}`,
      // Brackets stay one quiet ink instead of rotating through VS Code's hues.
      "editorBracketHighlight.foreground1": `#${meta}`,
      "editorBracketHighlight.foreground2": `#${meta}`,
      "editorBracketHighlight.foreground3": `#${meta}`,
      "editorBracketHighlight.foreground4": `#${meta}`,
      "editorBracketHighlight.foreground5": `#${meta}`,
      "editorBracketHighlight.foreground6": `#${meta}`,
      "editorIndentGuide.background1": pick("#edece9", "#292826"),
      "editorIndentGuide.activeBackground1": pick("#cfcdc8", "#45433f"),
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

// GraphiQL's own rail holds its plugins and dialogs. The section sidebar takes
// its place, pressing the rail's (hidden) buttons so GraphiQL keeps owning
// each panel's state, resize, and storage.
const DOCS = "Documentation Explorer";

const HISTORY = "History";

const RAIL_ACTIONS: { label: string; icon: LucideIcon; selector: string }[] = [
  { label: "Refetch schema", icon: RefreshCw, selector: 'button[aria-label^="Re-fetch"]' },
  { label: "Keyboard shortcuts", icon: Keyboard, selector: 'button[data-value="short-keys"]' },
  { label: "Settings", icon: Settings, selector: 'button[data-value="settings"]' },
];

function SidebarRow({
  icon: Icon,
  label,
  current,
  onClick,
}: {
  icon: LucideIcon;
  label: string;
  current?: boolean;
  onClick: () => void;
}) {
  return (
    <li>
      <button
        type="button"
        className="sidebar-nav__row"
        aria-current={current ? "true" : undefined}
        onClick={onClick}
      >
        <Icon aria-hidden="true" size={16} strokeWidth={1.5} className="sidebar-nav__icon" />
        <span className="sidebar-nav__name">{label}</span>
      </button>
    </li>
  );
}

/**
 * One of GraphiQL's toolbar buttons with the app's outline icon in place of
 * its own, and a short visible label where the icon alone is unclear.
 */
function withIcon(button: ReactNode, Icon: LucideIcon, label?: string) {
  if (!isValidElement<{ label?: string }>(button)) return button;

  // The accessible name and tooltip start with the visible label (WCAG 2.5.3).
  const named = label ? { label: (button.props.label ?? "").replace(/^\S+/, label) } : undefined;

  return cloneElement(
    button,
    named,
    <Icon aria-hidden="true" size={16} strokeWidth={1.5} className="graphiql-toolbar-icon" />,
    label ? <span className="graphiql-toolbar-label">{label}</span> : null,
  );
}

function GraphQLExplorer({
  vaultName,
  sidebarTitle,
  sidebarHint,
}: {
  vaultName?: string | null;
  sidebarTitle?: string | null;
  sidebarHint?: string | null;
}) {
  // GraphiQL's own theme toggle would disagree with the Appearance setting.
  const { resolved } = useAppearance();
  useRhizomeMonacoTheme(resolved);
  const panel = useRef<HTMLElement>(null);
  const [plugin, setPlugin] = useState<string | null>(null);

  // Stable: GraphiQL rebuilds its store, and opens a fresh tab, when this changes.
  const onTogglePlugin = useCallback(
    (visible: { title: string } | null) => setPlugin(visible?.title ?? null),
    [],
  );

  const press = (selector: string) =>
    panel.current?.querySelector<HTMLButtonElement>(`.graphiql-sidebar ${selector}`)?.click();

  const pressPlugin = (title: string) => press(`button[aria-label$="${title}"]`);

  return (
    <div className="workspace workbench graphql-workspace">
      <WorkbenchSidebar title={sidebarTitle} titleHint={sidebarHint}>
        <nav className="sidebar-nav" aria-label="GraphQL navigation">
          <div className="sidebar-nav__head">
            <h1>GraphQL</h1>
          </div>
          <ul className="sidebar-nav__list">
            <SidebarRow
              icon={BookOpen}
              label="Docs"
              current={plugin === DOCS}
              onClick={() => pressPlugin(DOCS)}
            />
            <SidebarRow
              icon={History}
              label="History"
              current={plugin === HISTORY}
              onClick={() => pressPlugin(HISTORY)}
            />
          </ul>
          <ul className="sidebar-nav__list sidebar-nav__list--foot">
            {RAIL_ACTIONS.map((action) => (
              <SidebarRow
                key={action.label}
                icon={action.icon}
                label={action.label}
                onClick={() => press(action.selector)}
              />
            ))}
          </ul>
        </nav>
      </WorkbenchSidebar>
      <main
        ref={panel}
        className="workbench__panel graphql-explorer"
        aria-labelledby="graphql-explorer-title"
      >
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
            onTogglePluginVisibility={onTogglePlugin}
          >
            {/* The header names the vault, so the wordmark slot stays empty. */}
            <GraphiQL.Logo>{null}</GraphiQL.Logo>
            <GraphiQL.Toolbar>
              {({ prettify, merge, copy }) => (
                <>
                  {withIcon(prettify, WandSparkles, "Format")}
                  {withIcon(merge, Merge, "Merge")}
                  {withIcon(copy, Copy)}
                </>
              )}
            </GraphiQL.Toolbar>
          </GraphiQL>
        </div>
      </main>
    </div>
  );
}

export default GraphQLExplorer;
