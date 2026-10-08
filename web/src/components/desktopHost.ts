import { useEffect, useEffectEvent } from "react";

import { isJsonObject, isString } from "../api/parse";
import type { ValidationHealth } from "../api/types";

declare global {
  interface Window {
    /** Set by Rhizome Desktop's content view initialization script. */
    __RHIZOME_DESKTOP__?: boolean;
  }
}

// Rhizome Desktop draws the workspace header in its native toolbar (SPEC-0119).
// The page has no native permissions: it reports its state through the
// document title, and the app sends it commands as `rhizome:desktop` events.

export const SECTIONS = ["notes", "ontology", "explorer", "agent", "graphql"] as const;

export type Section = (typeof SECTIONS)[number];

export type DesktopCommand =
  | { command: "section"; section: Section }
  | { command: "search"; query: string }
  | { command: "issues" }
  | { command: "shortcuts" };

export type DesktopReport = {
  section: Section;
  search: string;
  issues?: number | null;
  health?: ValidationHealth;
};

/** The app marks its content view with an initialization script. */
export function inDesktop(): boolean {
  return window.__RHIZOME_DESKTOP__ === true;
}

export function desktopTitle(report: DesktopReport): string {
  return `rhizome-desktop:${JSON.stringify({ v: 1, ...report })}`;
}

function readCommand(event: Event): DesktopCommand | null {
  const detail: unknown = event instanceof CustomEvent ? event.detail : null;

  if (!isJsonObject(detail)) return null;
  const { command } = detail;

  if (command === "section") {
    const section = SECTIONS.find((name) => name === detail.section);

    return section ? { command, section } : null;
  }

  if (command === "search") return isString(detail.query) ? { command, query: detail.query } : null;

  if (command === "issues" || command === "shortcuts") return { command };

  return null;
}

/** Calls `handle` for each command the app sends the page. */
export function useDesktopCommands(handle: (command: DesktopCommand) => void) {
  const onCommand = useEffectEvent(handle);

  useEffect(() => {
    const listener = (event: Event) => {
      const command = readCommand(event);

      if (command) onCommand(command);
    };

    window.addEventListener("rhizome:desktop", listener);

    return () => window.removeEventListener("rhizome:desktop", listener);
  }, []);
}
