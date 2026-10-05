export const ONTOLOGY_ROOT_PATH = "/ontology";

const ONTOLOGY_TYPE_PREFIX = `${ONTOLOGY_ROOT_PATH}/type/`;

export type OntologyRouteSelection = { kind: "atlas" } | { kind: "type"; name: string };

export function parseOntologyRoute(pathname: string): OntologyRouteSelection {
  if (!pathname.startsWith(ONTOLOGY_ROOT_PATH)) {
    return { kind: "atlas" };
  }

  const suffix = pathname.slice(ONTOLOGY_ROOT_PATH.length).replace(/^\/+|\/+$/g, "");

  if (!suffix) return { kind: "atlas" };

  if (suffix.startsWith("type/")) {
    const name = decodeURIComponent(suffix.slice("type/".length));

    if (name) return { kind: "type", name };
  }

  return { kind: "atlas" };
}

export function buildOntologyPath(selection: OntologyRouteSelection): string {
  if (selection.kind === "atlas") return ONTOLOGY_ROOT_PATH;

  return `${ONTOLOGY_TYPE_PREFIX}${encodeURIComponent(selection.name)}`;
}

export function isOntologyPath(pathname: string): boolean {
  return pathname === ONTOLOGY_ROOT_PATH || pathname.startsWith(`${ONTOLOGY_ROOT_PATH}/`);
}
