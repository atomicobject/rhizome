import type { WikiLinkResolvedTarget, WikiLinkSuggestion } from "@atomic-editor/editor";
import { searchNotes } from "../../api/client";
import type { RenderedFile } from "../../api/types";

type RhizomeLinkSource = {
  currentPath: string;
  rendered?: RenderedFile | null;
};

export function createRhizomeLinkAdapter(source: RhizomeLinkSource) {
  const rendered = source.rendered ?? null;
  const currentPath = source.currentPath.split("#")[0];
  const resolutionCache = new Map<string, Promise<ResolvedLink>>();

  const resolveLink = (rawTarget: string): Promise<ResolvedLink> => {
    const target = rawTarget.trim();
    const renderedTarget = resolveRenderedWikiTarget(target, rendered, currentPath);

    if (renderedTarget) return Promise.resolve({ path: renderedTarget, confirmed: true });
    const cached = resolutionCache.get(target);

    if (cached) return cached;
    const pending = resolveThroughSearch(target, currentPath);
    resolutionCache.set(target, pending);

    return pending;
  };

  return {
    async resolveWikiLink(target: string): Promise<WikiLinkResolvedTarget> {
      const resolved = await resolveLink(target);

      return {
        target: resolved.path || target,
        label: labelForWikiTarget(target, resolved.path || target),
        status: resolved.confirmed ? "resolved" : "missing",
      };
    },
    // Explicit vault paths open even before search or rendered metadata
    // confirms them; only name and alias links need a unique claimant.
    resolveTarget: async (target: string) => (await resolveLink(target)).path,
    async suggestWikiLinks(query: string): Promise<WikiLinkSuggestion[]> {
      const response = await searchNotes(query.trim(), { limit: 12 });

      return (response.matches || []).map((match, index) => ({
        target: match.path,
        label: match.title || noteName(match.path),
        detail: match.path,
        boost: 12 - index,
      }));
    },
  };
}

function resolveRenderedWikiTarget(
  rawTarget: string,
  rendered: RenderedFile | null,
  sourcePath = rendered?.path,
): string | null {
  const target = rawTarget.trim();
  const currentPath = sourcePath?.split("#")[0] || "";

  if (!target) return null;

  if (target.startsWith("#")) {
    return currentPath ? `${currentPath}${target}` : null;
  }

  if (target.startsWith("^")) {
    return currentPath ? `${currentPath}#${target}` : null;
  }

  const links = rendered?.links || [];
  const { base, fragment } = splitTarget(target);
  const baseCandidate = base.endsWith(".md") ? base : `${base}.md`;

  const direct = links.find(
    (link) =>
      sameTarget(link.text, target) ||
      sameTarget(link.target, target) ||
      sameTarget(link.text, baseCandidate) ||
      sameTarget(link.target, baseCandidate),
  );

  if (direct) return direct.target;

  const baseMatch = links.find(
    (link) =>
      sameTarget(link.text, base) ||
      sameTarget(link.target, base) ||
      sameTarget(link.text, baseCandidate) ||
      sameTarget(link.target, baseCandidate),
  );

  if (!baseMatch) return null;

  return fragment ? `${basePath(baseMatch.target)}#${fragment}` : baseMatch.target;
}

type ResolvedLink = { path: string | null; confirmed: boolean };

const unresolved: ResolvedLink = { path: null, confirmed: false };

async function resolveThroughSearch(
  target: string,
  currentPath: string | undefined,
): Promise<ResolvedLink> {
  const { base, fragment } = splitTarget(target);

  if (!base && currentPath)
    return { path: `${basePath(currentPath)}#${fragment}`, confirmed: true };

  if (!base) return unresolved;
  const explicitPath = explicitVaultPath(base, currentPath);
  const query = explicitPath ? noteName(explicitPath) : base.replace(/\.md$/i, "");
  const response = await searchNotes(query, { limit: 12 });
  const matches = response.matches || [];

  if (explicitPath) {
    const pathMatches = matches.filter((match) =>
      sameTarget(stripMarkdownSuffix(match.path), stripMarkdownSuffix(explicitPath)),
    );

    const confirmed = pathMatches.length === 1;
    const path = confirmed ? pathMatches[0].path : withMarkdownSuffix(explicitPath);

    return { path: fragment ? `${path}#${fragment}` : path, confirmed };
  }

  const exact = matches.filter((match) => {
    const path = stripMarkdownSuffix(match.path);
    const candidate = stripMarkdownSuffix(base);

    return (
      sameTarget(path, candidate) ||
      sameTarget(match.title, base) ||
      sameTarget(noteName(match.path), base)
    );
  });

  const selected = exact.length === 1 ? exact[0] : matches.length === 1 ? matches[0] : null;

  if (!selected) return unresolved;

  return { path: fragment ? `${selected.path}#${fragment}` : selected.path, confirmed: true };
}

function explicitVaultPath(target: string, currentPath: string | undefined) {
  if (!target.includes("/")) return null;

  const parts = target.startsWith(".")
    ? [...(currentPath ? basePath(currentPath).split("/").slice(0, -1) : []), ...target.split("/")]
    : target.replace(/^\/+/, "").split("/");

  const normalized: string[] = [];

  for (const part of parts) {
    if (!part || part === ".") continue;

    if (part === "..") {
      if (normalized.length === 0) return null;
      normalized.pop();
      continue;
    }

    normalized.push(part);
  }

  return normalized.join("/");
}

function splitTarget(target: string) {
  const hashIndex = target.indexOf("#");

  return hashIndex < 0
    ? { base: target, fragment: "" }
    : {
        base: target.slice(0, hashIndex),
        fragment: target.slice(hashIndex + 1),
      };
}

function sameTarget(left: string | undefined, right: string) {
  return left?.trim().toLocaleLowerCase() === right.trim().toLocaleLowerCase();
}

function basePath(target: string) {
  return target.split("#")[0];
}

function noteName(path: string) {
  const filename = basePath(path).split("/").pop() || path;

  return filename.replace(/\.md$/i, "");
}

function stripMarkdownSuffix(path: string) {
  return path.replace(/\.md$/i, "");
}

function withMarkdownSuffix(path: string) {
  return path.endsWith(".md") ? path : `${path}.md`;
}

function labelForWikiTarget(rawTarget: string, resolvedTarget: string) {
  if (rawTarget.startsWith("#") || rawTarget.startsWith("^")) {
    return rawTarget.replace(/^#?\^?/, "") || "Local link";
  }

  return noteName(resolvedTarget || rawTarget);
}
