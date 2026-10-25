import {
  useEffect,
  useEffectEvent,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type JSX,
  type MouseEvent,
} from "react";
import { renderDiagram } from "../src/components/markdown/mermaid";
import type { ViewNodeRef } from "../src/views/context";
import { openNode } from "./index";

type DiagramState =
  | { kind: "loading" }
  | { kind: "ready"; svg: string; source: string; linkKey: string; targets: ViewNodeRef[] }
  | { kind: "error"; message: string; source: string };

export function Diagram(props: {
  /** Build Mermaid text during render from query data. Whitespace renders nothing. */
  source: string;
  /** Accessible name. */
  label: string;
  /** Mermaid node or task id → note to open on click. Supported in flowchart, gantt, classDiagram, stateDiagram, and agentflow-beta; ignored with a console warning elsewhere. */
  links?: Record<string, ViewNodeRef>;
  className?: string;
}): JSX.Element | null {
  const { source, label, links, className } = props;
  const idPrefix = `diagram-${useId().replace(/[^a-zA-Z0-9_-]/g, "")}`;
  const [state, setState] = useState<DiagramState>({ kind: "loading" });
  const reported = useRef({ warnings: new Set<string>(), errors: new Set<string>() });
  const container = useRef<HTMLDivElement>(null);
  const markup = useMemo(() => ({ __html: state.kind === "ready" ? state.svg : "" }), [state]);
  const entries = Object.entries(links ?? {}).sort(([a], [b]) => a.localeCompare(b));

  const linkKey = JSON.stringify(
    entries.map(([id, ref]) => [id, ref.notePath, ref.kind, ref.nodeId]),
  );

  const readEntries = useEffectEvent(() => entries);
  const [width, setWidth] = useState(0);
  const hasSource = source.trim() !== "";

  // Charts fill the view's width; measured in 20px steps so resizing re-lays
  // the chart out without a render per pixel.
  useLayoutEffect(() => {
    const element = container.current;

    if (!element) return;

    const update = () => setWidth(Math.floor(element.clientWidth / 20) * 20);
    update();

    if (typeof ResizeObserver === "undefined") return;

    const observer = new ResizeObserver(update);
    observer.observe(element);

    return () => observer.disconnect();
  }, [hasSource]);

  useEffect(() => {
    if (!source.trim()) return;

    let cancelled = false;
    const targets = readEntries();
    void renderDiagram(
      source,
      idPrefix,
      true,
      targets.map(([id]) => id),
      width || undefined,
    ).then(
      (result) => {
        if (cancelled) return;

        const activeTargets = result.linksSupported ? targets : [];
        const unsupported = targets.length > 0 && !result.linksSupported;

        const missing = activeTargets
          .filter(
            ([id], n) =>
              !result.links.some((link) => link.id === id && link.href === `#rzm-link-${n}`),
          )
          .map(([id]) => id);

        if ((unsupported || missing.length) && !reported.current.warnings.has(source)) {
          reported.current.warnings.add(source);
          console.warn(
            unsupported
              ? `Diagram (${result.type}): links are supported only for flowchart, gantt, classDiagram, stateDiagram, agentflow-beta`
              : `Diagram (${result.type}) has no links for ids: ${missing.join(", ")}`,
          );
        }

        setState({
          kind: "ready",
          svg: result.svg,
          source,
          linkKey,
          targets: activeTargets.map(([, ref]) => ref),
        });
      },
      // oxlint-disable-next-line anti-slop/no-unknown-parameters -- Promise rejections can contain any value; normalize it here.
      (error: unknown) => {
        if (cancelled) return;

        const message = error instanceof Error ? error.message : String(error);

        if (!reported.current.errors.has(source)) {
          reported.current.errors.add(source);
          console.error(`Diagram error: ${message}`, source);
        }

        setState({ kind: "error", message, source });
      },
    );

    return () => {
      cancelled = true;
    };
  }, [source, linkKey, idPrefix, width]);

  useEffect(() => {
    container.current
      ?.querySelectorAll("a")
      .forEach((anchor) => anchor.setAttribute("tabindex", "0"));
  }, [state]);

  if (!source.trim()) return null;

  function onClick(event: MouseEvent<HTMLDivElement>) {
    const anchor = event.target instanceof Element ? event.target.closest("a") : null;
    const href = anchor?.getAttribute("href") ?? anchor?.getAttribute("xlink:href");

    if (!anchor) return;

    event.preventDefault();

    if (event.button !== 0 && event.button !== 1) return;

    const match = /^#rzm-link-(\d+)$/.exec(href ?? "");

    if (!match || state.kind !== "ready") return;

    const index = Number(match[1]);

    // Keep pending SVG targets, but use current section metadata for unchanged diagrams.
    const ref =
      state.source === source && state.linkKey === linkKey
        ? entries[index]?.[1]
        : state.targets[index];

    if (ref) openNode(ref, { beside: event.button === 1 || event.metaKey || event.ctrlKey });
  }

  const hasLinks = state.kind === "ready" ? /<a\b/.test(state.svg) : entries.length > 0;

  return (
    <div
      ref={container}
      className={`rhizome-diagram ${className ?? ""}`}
      role={state.kind === "ready" && !hasLinks ? "img" : "group"}
      aria-label={label}
      onClick={onClick}
      onAuxClick={onClick}
    >
      {state.kind === "loading" && (
        <div className="rhizome-diagram__loading">Rendering diagram…</div>
      )}
      {state.kind === "error" && (
        <div className="rhizome-diagram__error">
          <p>Diagram error: {state.message}</p>
          <details>
            <summary>Source</summary>
            <pre>{state.source}</pre>
          </details>
        </div>
      )}
      {state.kind === "ready" && <div dangerouslySetInnerHTML={markup} />}
    </div>
  );
}
