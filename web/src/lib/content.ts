import type { FileView, RenderedFile } from "../api/types";
import { encodeURLFragment } from "./noteDeepLink";

/** The only part of a note payload link resolution reads. */
type LinkCarrier = Pick<FileView, "links"> | null;

const NOTE_FILE_SUFFIX = /\.(?:md|html?)$/i;

/** Preserve supported note suffixes while retaining extensionless Markdown links. */
export function normalizeNotePath(path: string): string {
  return path && !NOTE_FILE_SUFFIX.test(path) ? `${path}.md` : path;
}

// Fenced blocks (closed or running to the end) and inline code spans keep
// their wikilink text literally, as Obsidian renders them. A fence closes only
// on a run of its own character at least as long as the opening run.
const CODE_OR_WIKILINK = new RegExp(
  [
    String.raw`(?:^|(?<=\n)) {0,3}((\x60|~)\2{2,})[^\r\n]*(?:\r?\n[\s\S]*?(?:\n {0,3}\1\2*[ \t]*(?=\r?\n|$)|$)|$)`,
    String.raw`(?<!\x60)(\x60+)[^\x60\n](?:[^\n]*?[^\x60\n])?\3(?!\x60)`,
    String.raw`!?\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`,
  ].join("|"),
  "g",
);

export function convertWikilinks(content: string) {
  return content.replace(
    CODE_OR_WIKILINK,
    (match, _fence, _fenceChar, _tick, target?: string, label?: string) => {
      if (target === undefined) return match;
      const text = (label || target).trim();
      const href = `rhizome://note/${encodeURIComponent(target.trim())}`;

      return `[${text}](${href})`;
    },
  );
}

export function stripFrontmatter(content: string) {
  const normalized = content.replace(/\r\n/g, "\n");
  const match = normalized.match(/^\s*---\s*\n[\s\S]*?\n---\s*\n/);

  if (!match) {
    return content;
  }

  return normalized.slice(match[0].length);
}

export function resolveLinkTarget(href: string | undefined, file: FileView | null) {
  return resolveInternalTarget(href, file);
}

export function resolveRenderedLinkTarget(href: string | undefined, rendered: RenderedFile | null) {
  return resolveInternalTarget(href, rendered);
}

/** Resolve only indexed link evidence, leaving URL-relative fallback to the caller. */
export function resolveKnownRenderedLinkTarget(
  href: string | undefined,
  rendered: RenderedFile | null,
) {
  return resolveInternalTarget(href, rendered, false);
}

export function buildNoteWebHref(target: string) {
  const hashIndex = target.indexOf("#");
  const beforeHash = hashIndex >= 0 ? target.slice(0, hashIndex) : target;
  const noteHash = hashIndex >= 0 ? target.slice(hashIndex) : "";
  const queryIndex = beforeHash.indexOf("?");
  const notePath = queryIndex >= 0 ? beforeHash.slice(0, queryIndex) : beforeHash;
  const noteQuery = queryIndex >= 0 ? beforeHash.slice(queryIndex + 1) : "";

  const location =
    typeof window === "undefined" ? { pathname: "/notes", search: "" } : window.location;

  const pathname = location.pathname.startsWith("/notes") ? location.pathname : "/notes";
  const params = new URLSearchParams(location.search);
  params.set("note", notePath);

  if (noteQuery) params.set("noteQuery", noteQuery);
  else params.delete("noteQuery");
  const query = params.toString();

  return `${pathname}${query ? `?${query}` : ""}${encodeURLFragment(noteHash)}`;
}

function resolveInternalTarget(href: string | undefined, file: LinkCarrier, allowFallback = true) {
  if (!href) {
    return null;
  }

  let raw = href;

  if (href.startsWith("rhizome://note/")) {
    raw = href.slice("rhizome://note/".length);

    try {
      raw = decodeURIComponent(raw);
    } catch {
      // Hand-authored links can contain literal or malformed percent escapes.
      // Preserve their target, as we do for URL fragments.
    }
  }

  if (!file?.links) {
    return href.startsWith("rhizome://note/") ? raw : null;
  }

  if (/^[a-z]+:\/\//i.test(raw) || raw.startsWith("mailto:")) {
    return null;
  }

  const hashIndex = raw.indexOf("#");
  const base = hashIndex >= 0 ? raw.slice(0, hashIndex) : raw;
  const fragment = hashIndex >= 0 ? raw.slice(hashIndex + 1) : "";
  const baseCandidate = normalizeNotePath(base);
  const candidate = fragment ? `${baseCandidate}#${fragment}` : baseCandidate;

  const direct = file.links.find(
    (link) =>
      link.target === raw ||
      link.text === raw ||
      link.target === candidate ||
      link.text === candidate,
  );

  if (direct) {
    return direct.target;
  }

  const baseMatch = file.links.find(
    (link) =>
      link.target === base ||
      link.text === base ||
      link.target === baseCandidate ||
      link.text === baseCandidate,
  );

  if (baseMatch) {
    return fragment ? `${baseMatch.target}#${fragment}` : baseMatch.target;
  }

  return allowFallback ? (fragment ? `${baseCandidate}#${fragment}` : baseCandidate) : null;
}
