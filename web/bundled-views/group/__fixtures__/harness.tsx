// Test helpers for the group views, over the synthetic fixtures in groups.ts.
//
// - fixtureModel(group, docs?): the GroupModel the loader would build.
// - collectionModel(name): the model of a type or interface collection, over
//   the collection fixtures in collections.ts.
// - groupRoutes(overrides?), collectionRoutes(overrides?): fake HTTP routes
//   answering every request the loader makes, for the kit's `renderView` harness.
// - renderGroupView(load, options): renders a view module for a group, or for
//   another `context`, as the workspace frames it when `embedded`, and records
//   what it posts to the host.
import type { DisplayGroup, TypeDoc, ViewContext, ViewModuleProps } from "@rhizome/kit";
import type { ComponentType } from "react";
import { onTestFinished, vi } from "vitest";

import { jsonReply, type FakeFetch } from "../../../src/test/fakeFetch";
import type { ViewRoutes } from "../../../kit/testing.tsx";
import {
  parseRecords,
  parseTableChoice,
  parseTargetViews,
  parseTypeLabels,
  type JsonObject,
  type JsonValue,
} from "../api.ts";
import { buildGroupModel, collectionGroup, planMembers, type GroupModel } from "../model.ts";
import {
  COLLECTION_CATALOG,
  COLLECTION_DATA,
  COLLECTION_DOCS,
  COLLECTION_GROUPS,
  COLLECTION_SUMMARIES,
} from "./collections.ts";
import {
  DISPLAY_GROUPS,
  NOW,
  RECORDS_DATA,
  TYPE_DOCS,
  TYPE_SUMMARIES,
  VIEW_CATALOG,
} from "./groups.ts";

/**
 * The model for a fixture group. `docs` replaces type documentation (to drop a
 * parent role, say), `group` replaces the group's membership, `groups` adds
 * other display groups, and `data` replaces the GraphQL records data.
 */
export function fixtureModel(
  name: string,
  options: {
    docs?: Record<string, TypeDoc>;
    group?: DisplayGroup;
    groups?: readonly DisplayGroup[];
    data?: JsonObject;
  } = {},
): GroupModel {
  const docs = new Map<string, TypeDoc>(Object.entries(options.docs ?? TYPE_DOCS));
  const group = options.group ?? DISPLAY_GROUPS.groups.find((candidate) => candidate.name === name);

  if (!group) throw new Error(`No fixture group ${name}`);
  const types = planMembers(group).flatMap((plan) => plan.concreteTypes);

  return buildGroupModel({
    group,
    groups: [
      ...(options.groups ?? []),
      ...DISPLAY_GROUPS.groups.filter((candidate) => candidate.name !== name),
      group,
    ],
    docs: Object.fromEntries(docs),
    records: parseRecords(
      options.data ?? RECORDS_DATA,
      types.flatMap((type) => docs.get(type) ?? []),
    ),
    views: parseTargetViews(VIEW_CATALOG, "group", name),
    typeLabels: parseTypeLabels(TYPE_SUMMARIES),
  });
}

/** The model of a fixture type or interface collection, as `useCollectionModel` builds it. */
export function collectionModel(
  name: string,
  options: { docs?: Record<string, TypeDoc>; data?: JsonObject } = {},
): GroupModel {
  const docs = new Map<string, TypeDoc>(Object.entries(options.docs ?? COLLECTION_DOCS));
  const group = collectionGroup(COLLECTION_GROUPS.groups, name);

  if (!group) throw new Error(`No fixture collection ${name}`);
  const kind = group.members[0].kind;
  const types = planMembers(group).flatMap((plan) => plan.concreteTypes);

  return buildGroupModel({
    group,
    groups: COLLECTION_GROUPS.groups,
    docs: Object.fromEntries(docs),
    records: parseRecords(
      options.data ?? COLLECTION_DATA,
      types.flatMap((type) => docs.get(type) ?? []),
    ),
    views: parseTargetViews(COLLECTION_CATALOG, kind, name),
    tableChoice: parseTableChoice(COLLECTION_CATALOG, kind, name),
    typeLabels: parseTypeLabels(COLLECTION_SUMMARIES),
  });
}

/** Routes answering a collection loader's requests from the collection fixtures. */
export function collectionRoutes(overrides: ViewRoutes = {}): ViewRoutes {
  const routes: ViewRoutes = {
    "GET /api/v1/display-groups": () => jsonReply(COLLECTION_GROUPS),
    "GET /api/v1/views": () => jsonReply(COLLECTION_CATALOG),
    "GET /api/v1/ontology/types": () => jsonReply(COLLECTION_SUMMARIES),
    "POST /api/v1/graphql": () => jsonReply({ data: COLLECTION_DATA }),
  };

  for (const [name, doc] of Object.entries(COLLECTION_DOCS)) {
    routes[`GET /api/v1/ontology/types/${name}`] = () => jsonReply({ type: doc, count: 0 });
  }

  return { ...routes, ...overrides };
}

/** Routes answering the loader's requests from the fixtures; `overrides` replace any of them. */
export function groupRoutes(overrides: ViewRoutes = {}): ViewRoutes {
  const routes: ViewRoutes = {
    "GET /api/v1/display-groups": () => jsonReply(DISPLAY_GROUPS),
    "GET /api/v1/views": () => jsonReply(VIEW_CATALOG),
    "GET /api/v1/ontology/types": () => jsonReply(TYPE_SUMMARIES),
    "POST /api/v1/graphql": () => jsonReply({ data: RECORDS_DATA }),
  };

  for (const [name, doc] of Object.entries(TYPE_DOCS)) {
    routes[`GET /api/v1/ontology/types/${name}`] = () => jsonReply({ type: doc, count: 0 });
  }

  return { ...routes, ...overrides };
}

type ViewModule = { default: ComponentType<ViewModuleProps> };

/** A message a framed view posted to the workspace, such as `rhizome:open-node`. */
export type PostedMessage = {
  type: string;
  ref?: JsonValue;
  name?: string;
  path?: string;
  scope?: JsonValue;
  id?: string;
  context?: JsonValue;
};

/**
 * Render a view module for `group` through the kit's harness, with `Date` set
 * to `now` (the fixtures' NOW by default). With `embedded`, the view runs
 * framed by a workspace whose `postMessage` is recorded in `posted`, so
 * navigation can be asserted. Modules are reloaded so the kit sees the frame.
 */
export async function renderGroupView(
  load: () => Promise<ViewModule>,
  options: {
    group?: string;
    /** Replaces the group context, to open the view for a type or interface. */
    context?: ViewContext;
    routes?: ViewRoutes;
    embedded?: boolean;
    now?: number;
    /** A fake shared across mounts, such as one holding remembered preferences. */
    http?: FakeFetch;
  } = {},
) {
  const posted: PostedMessage[] = [];

  // Relative times and staleness read the clock; timers stay real.
  vi.useFakeTimers({ toFake: ["Date"], now: options.now ?? NOW });
  onTestFinished(() => {
    vi.useRealTimers();
  });

  if (options.embedded) {
    const frame = document.createElement("iframe");
    document.body.append(frame);
    const parent = frame.contentWindow;
    const descriptor = Object.getOwnPropertyDescriptor(window, "parent");

    if (!parent || !descriptor) throw new Error("renderGroupView needs a frame window");
    Object.defineProperty(window, "parent", { configurable: true, value: parent });
    vi.spyOn(parent, "postMessage").mockImplementation((message: PostedMessage) => {
      posted.push(message);
    });

    onTestFinished(() => {
      Object.defineProperty(window, "parent", descriptor);
      frame.remove();
    });
  }

  vi.resetModules();
  const { renderView } = await import("../../../kit/testing.tsx");

  const view = await renderView(load, {
    context: options.context ?? { kind: "group", group: options.group ?? "Planning" },
    view: { id: "group.test", name: "Group test", origin: "bundled" },
    routes: options.routes ?? groupRoutes(),
    http: options.http,
  });

  return { ...view, posted };
}
