import React, { type ReactNode, useState } from "react";
import * as ReactDOMClient from "react-dom/client";
import "@testing-library/jest-dom";
import { Widget } from "@acme/widgets/react";
import nodePath from "node:path";
import path from "path";

// These two imports must continue to resolve locally before external classification.
import { libraryCall } from "@fixture/library";
import { aliasCall } from "@ui/button";

type UnknownRenderer = {
  render(value: ReactNode): void;
};

export function exerciseExternalESM(
  container: Element,
  unknownRenderer: UnknownRenderer,
): string {
  const [value] = useState<ReactNode>(React.createElement("span", null, "ready"));
  const root = ReactDOMClient.createRoot(container);
  const widget = new Widget();

  root.render(value);
  unknownRenderer.render(value); // Unknown receiver members must remain unresolved.
  console.log(widget); // Well-known JavaScript global.

  return [
    nodePath.basename("/fixture/node"),
    path.basename("/fixture/bare"),
    libraryCall(),
    aliasCall(),
  ].join(":");
}

export async function leaveComputedModuleUnknown(moduleName: string): Promise<void> {
  await import(moduleName);
}
