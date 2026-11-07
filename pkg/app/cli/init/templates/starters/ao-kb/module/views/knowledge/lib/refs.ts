/** Native node identity for Knowledge views (SPEC-0133 R3, R7). Every view turns a
 * relationship endpoint into an openable ref here, so list, record, and Trace agree.
 * Pure and kit-free so `node --test` can import it. */

export type Ref = {
  notePath: string;
  kind: string;
  fragment?: string | null;
  nodeId?: string | null;
  typeName?: string | null;
  structuralFingerprint?: string | null;
};

/** A relationship endpoint as GraphQL returns it (`NodeRef`, selected with `structural`). */
export type Endpoint = {
  notePath: string;
  kind?: string | null;
  fragment?: string | null;
  nodeId?: string | null;
  typeName?: string | null;
  structural?: string | null;
  structuralFingerprint?: string | null;
};

/** The identity key of a ref: its note path, plus the fragment for an embedded node.
 * It is also the string `node(ref:)` resolves, and it equals a record's GraphQL `path`. */
export const refKey = (ref: { notePath: string; fragment?: string | null }) =>
  ref.fragment ? `${ref.notePath}#${ref.fragment}` : ref.notePath;

export type Resolved =
  | { key: string; ref: Ref; positional?: undefined }
  /** A positional record whose canonical fragment cannot be derived: the host file
   * still opens, and the record is shown as authored rather than guessed. */
  | { key: string; ref?: undefined; host: Ref; positional: true };

/** An endpoint as an openable canonical ref. */
export function recordRef(e: Endpoint): Resolved {
  const base = {
    notePath: e.notePath,
    kind: e.kind || (e.fragment || e.nodeId ? "EMBEDDED" : "NOTE"),
    nodeId: e.nodeId ?? undefined,
    typeName: e.typeName ?? undefined,
    structuralFingerprint: e.structuralFingerprint ?? e.structural ?? undefined,
  };
  if (e.fragment) return { key: refKey(e), ref: { ...base, fragment: e.fragment } };
  // ponytail: neighborhood edges omit the fragment of RECORD endpoints. A keyed record's
  // fragment is a fixed encoding of its nodeId (pkg/ontology/record_projection.go).
  // Delete this branch when RECORD endpoints carry their canonical fragment.
  const keyed = /^record-v1-([0-9a-f]+)$/.exec(e.nodeId ?? "");
  if (keyed) {
    const fragment = `record:v1:${keyed[1]}`;
    return { key: refKey({ notePath: e.notePath, fragment }), ref: { ...base, fragment } };
  }
  if (e.nodeId?.startsWith("record-pos-v1-"))
    return {
      key: `${e.notePath}#${e.nodeId}`,
      host: { notePath: e.notePath, kind: "NOTE" },
      positional: true,
    };
  return { key: e.notePath, ref: { ...base, kind: base.kind === "EMBEDDED" ? "NOTE" : base.kind } };
}

/** Whether an endpoint is a structured RECORD node rather than a Markdown note or section. */
export const isRecord = (e: Endpoint) =>
  /^record(-pos)?:/.test(e.fragment ?? "") || /^record(-pos)?-v1-/.test(e.nodeId ?? "");

/** The record an endpoint belongs to: a RECORD node is its own record, while a Markdown
 * section (such as a claim's Connections) belongs to its note. */
export const owner = (e: Endpoint): Resolved =>
  isRecord(e) ? recordRef(e) : { key: e.notePath, ref: { notePath: e.notePath, kind: "NOTE" } };
