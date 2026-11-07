import type { PublicNode } from "../api/publicGraphQLTypes";

export function nativeRecordNode(target: string): PublicNode {
  const [path, fragment] = target.split("#", 2);

  const ref = {
    ref: target,
    kind: fragment ? "EMBEDDED" : "NOTE",
    notePath: path,
    path,
    fragment,
    nodeId: fragment ? "booking" : undefined,
  };

  const title = fragment ? "Booking service" : "services";

  const source = fragment
    ? {
        representation: "source_value",
        text: '{"name":"Booking service"}',
        mediaType: "application/json",
        format: "yaml",
        value: {
          kind: "object",
          entries: [{ key: "name", value: { kind: "string", scalar: title } }],
        },
      }
    : undefined;

  return {
    ref,
    title,
    nodeId: ref.nodeId || path,
    nodeKind: ref.kind,
    path,
    format: "yaml",
    content: source?.text || "services: []",
    locator: { sourceLocator: target, status: "linkable", exists: true, requiresFix: false },
    workspace: {
      source,
      fields: [],
      collections: [],
      relationGroups: [],
      structure: [],
      bodies: [
        {
          ref,
          title,
          source,
          locator: fragment ? "EMBEDDED" : "FILE",
          markdown: "",
          fields: [],
          collections: [],
          blocks: [],
        },
      ],
      capabilities: {
        canEdit: false,
        canEditFields: false,
        canEditCollections: false,
        canNavigateChildren: true,
        canSubscribe: true,
        readOnlyReason: "Managed by AO KB. Change it with the AO KB skills.",
      },
      status: {
        dirty: false,
        validation: { issueCount: 0 },
        freshness: {},
        session: {},
        hasWarnings: false,
      },
      loaded: { rendered: true, assessment: false, structure: true, relations: true },
      version: "native-v1",
    },
  };
}
