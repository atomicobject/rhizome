import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  createHTMLViewer,
  revokeHTMLViewer,
  type HTMLViewerSession,
} from "../api/htmlViewerClient";

const BRIDGE_VERSION = 1;

const MAX_DOWNLOAD_BYTES = 32 * 1024 * 1024;

const MAX_REQUEST_ID_LENGTH = 128;

const MAX_ERROR_MESSAGE_LENGTH = 4096;

const MAX_NAVIGATION_VALUE_LENGTH = 4096;

const MAX_FILENAME_LENGTH = 512;

const MAX_MEDIA_TYPE_LENGTH = 200;

const MAX_SEEN_REQUESTS = 256;

const DOWNLOAD_RATE_WINDOW_MS = 60_000;

const DOWNLOAD_RATE_LIMIT = 4;

const VIEWER_START_TIMEOUT_MS = 10_000;

const VIEWER_READY_TIMEOUT_MS = 10_000;

type PendingDownload = {
  requestId: string;
  filename: string;
  mediaType: string;
  bytes: ArrayBuffer;
};

type PendingExternal = { href: string };

type ViewerMessage =
  | { type: "ready"; requestId: string }
  | { type: "error"; requestId: string; message: string }
  | {
      type: "navigate" | "external";
      requestId: string;
      authored: string;
      resolved: string;
      beside: boolean;
    }
  | {
      type: "download";
      requestId: string;
      filename: string;
      mediaType: string;
      size: number;
      data: ArrayBuffer;
    };

type ViewerCommand =
  | { type: "ack"; requestId: string }
  | { type: "set-fragment"; fragment: string }
  | { type: "download-result"; requestId: string; ok: boolean; message?: string };

const ENVELOPE_KEYS = ["version", "nonce", "viewerId", "notePath", "requestId", "type"] as const;

/* oxlint-disable anti-slop/no-runtime-typeof, anti-slop/no-unsafe-dictionary-type, anti-slop/require-safety-comment-for-type-assertion, anti-slop/no-unknown-parameters -- These helpers validate values at the postMessage and API error trust boundaries. */
function hasOnlyKeys(message: Record<string, unknown>, allowed: readonly string[]): boolean {
  return Object.keys(message).every((key) => allowed.includes(key));
}

function hasSafeCharacters(value: string): boolean {
  return Array.from(value).every((character) => {
    const code = character.codePointAt(0) ?? 0;

    return code >= 32 && code !== 127;
  });
}

function hasBoundedString(value: unknown, maxLength: number, allowEmpty = false): value is string {
  return (
    typeof value === "string" &&
    value.length <= maxLength &&
    (allowEmpty || value.length > 0) &&
    hasSafeCharacters(value)
  );
}

function hasRequestId(value: unknown): value is string {
  return hasBoundedString(value, MAX_REQUEST_ID_LENGTH);
}

function hasArrayBuffer(value: unknown): value is ArrayBuffer {
  return Object.prototype.toString.call(value) === "[object ArrayBuffer]";
}

function parseViewerMessage(
  value: unknown,
  session: HTMLViewerSession,
  notePath: string,
): ViewerMessage | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  // SAFETY: every field used below is checked before the parsed message leaves this boundary.
  const message = value as Record<string, unknown>;

  if (
    message.version !== BRIDGE_VERSION ||
    message.nonce !== session.nonce ||
    message.viewerId !== session.id ||
    message.notePath !== notePath ||
    !hasRequestId(message.requestId) ||
    typeof message.type !== "string"
  ) {
    return null;
  }

  if (message.type === "ready") {
    if (!hasOnlyKeys(message, [...ENVELOPE_KEYS, "href"])) return null;

    if ("href" in message && !hasBoundedString(message.href, MAX_NAVIGATION_VALUE_LENGTH, true)) {
      return null;
    }

    return { type: "ready", requestId: message.requestId };
  }

  if (message.type === "error") {
    if (!hasOnlyKeys(message, [...ENVELOPE_KEYS, "scope", "message"])) return null;

    if (!hasBoundedString(message.message, MAX_ERROR_MESSAGE_LENGTH)) return null;

    if (
      "scope" in message &&
      (!hasBoundedString(message.scope, 32) ||
        (message.scope !== "download" && message.scope !== "resource"))
    ) {
      return null;
    }

    return {
      type: "error",
      requestId: message.requestId,
      message: message.message,
    };
  }

  if (message.type === "navigate" || message.type === "external") {
    if (!hasOnlyKeys(message, [...ENVELOPE_KEYS, "target", "resolved", "beside"])) return null;

    if (
      !hasBoundedString(message.target, MAX_NAVIGATION_VALUE_LENGTH, true) ||
      !hasBoundedString(message.resolved, MAX_NAVIGATION_VALUE_LENGTH, true) ||
      ("beside" in message && typeof message.beside !== "boolean")
    ) {
      return null;
    }

    return {
      type: message.type,
      requestId: message.requestId,
      authored: message.target,
      resolved: message.resolved,
      beside: message.beside === true,
    };
  }

  if (message.type === "download") {
    if (
      !hasOnlyKeys(message, [...ENVELOPE_KEYS, "filename", "mediaType", "size", "encoding", "data"])
    ) {
      return null;
    }

    if (
      !hasBoundedString(message.filename, MAX_FILENAME_LENGTH, true) ||
      !hasBoundedString(message.mediaType, MAX_MEDIA_TYPE_LENGTH, true) ||
      message.encoding !== "arrayBuffer" ||
      typeof message.size !== "number" ||
      !Number.isSafeInteger(message.size) ||
      message.size < 0 ||
      !hasArrayBuffer(message.data)
    ) {
      return null;
    }

    return {
      type: "download",
      requestId: message.requestId,
      filename: message.filename,
      mediaType: message.mediaType,
      size: message.size,
      data: message.data,
    };
  }

  return null;
}
/* oxlint-enable anti-slop/no-runtime-typeof, anti-slop/no-unsafe-dictionary-type, anti-slop/require-safety-comment-for-type-assertion, anti-slop/no-unknown-parameters */

type Props = {
  path: string;
  query?: string | null;
  fragment?: string | null;
  generation?: string | null;
  visible: boolean;
  applicationHref: string;
  /** Chrome-free page mode: the viewer is the whole document. */
  bare?: boolean;
  onNavigate: (target: string, beside: boolean, authored: string) => void;
};

/** The same note route with `bare=1`, which AppShell renders without chrome. */
export function bareNoteHref(applicationHref: string): string {
  const url = new URL(applicationHref, "http://rhizome.invalid");
  url.searchParams.set("bare", "1");

  return `${url.pathname}${url.search}${url.hash}`;
}

function safeFilename(authoredFilename: string): string {
  const basename = authoredFilename.replaceAll("\\", "/").split("/").pop() || "";

  const sanitized = Array.from(basename, (character) => {
    const code = character.codePointAt(0) ?? 0;

    return code < 32 || code === 127 || ":".includes(character) ? "-" : character;
  }).join("");

  let value = sanitized
    .replace(/[. ]+$/g, "")
    .trim()
    .slice(0, 180);

  if (!value) value = "download";

  if (/^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)/i.test(value)) value = `_${value}`;

  return value;
}

function safeMediaType(authoredMediaType: string): string {
  if (authoredMediaType.length > MAX_MEDIA_TYPE_LENGTH || /[\r\n]/.test(authoredMediaType)) {
    return "application/octet-stream";
  }

  return authoredMediaType || "application/octet-stream";
}

function displayError(value: Error | null, fallback: string): string {
  const message = value?.message || "";

  const cleaned = Array.from(message)
    .filter((character) => {
      const code = character.codePointAt(0) ?? 0;

      return code >= 32 && code !== 127;
    })
    .join("")
    .trim();

  if (!cleaned) return fallback;

  return cleaned.length > MAX_ERROR_MESSAGE_LENGTH
    ? `${cleaned.slice(0, MAX_ERROR_MESSAGE_LENGTH - 1)}…`
    : cleaned;
}

function validateViewerSession(session: HTMLViewerSession): HTMLViewerSession {
  if (
    !hasBoundedString(session.id, MAX_REQUEST_ID_LENGTH) ||
    !hasBoundedString(session.nonce, MAX_REQUEST_ID_LENGTH) ||
    !hasBoundedString(session.url, MAX_NAVIGATION_VALUE_LENGTH)
  ) {
    throw new Error("HTML viewer returned an invalid session");
  }

  let parsed: URL;

  try {
    parsed = new URL(session.url);
  } catch {
    throw new Error("HTML viewer returned an invalid session URL");
  }

  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new Error("HTML viewer returned an unsupported session URL");
  }

  return session;
}

function isAllowedMessageOrigin(origin: string, viewerURL: string): boolean {
  if (origin === "" || origin === "null") return true;

  try {
    return origin === new URL(viewerURL).origin;
  } catch {
    return false;
  }
}

function rememberRequest(seen: Set<string>, key: string): boolean {
  if (seen.has(key)) return false;
  seen.add(key);

  while (seen.size > MAX_SEEN_REQUESTS) {
    const oldest = seen.values().next().value;

    if (oldest === undefined) break;
    seen.delete(oldest);
  }

  return true;
}

function consumeDownloadAllowance(timestamps: number[], now: number): boolean {
  while (timestamps.length && now - timestamps[0] >= DOWNLOAD_RATE_WINDOW_MS) timestamps.shift();

  if (timestamps.length >= DOWNLOAD_RATE_LIMIT) return false;
  timestamps.push(now);

  return true;
}

function fileSizeLabel(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;

  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;

  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}

export function HTMLNoteViewer({
  path,
  query,
  fragment,
  generation,
  visible,
  applicationHref,
  bare = false,
  onNavigate,
}: Props) {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [refreshGeneration, setRefreshGeneration] = useState(0);
  const [session, setSession] = useState<HTMLViewerSession | null>(null);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pendingDownload, setPendingDownload] = useState<PendingDownload | null>(null);
  const [pendingExternal, setPendingExternal] = useState<PendingExternal | null>(null);
  const readyRef = useRef(false);
  const frameFailureRef = useRef(false);
  const pendingDownloadRef = useRef<PendingDownload | null>(null);
  const seenRequestsRef = useRef(new Set<string>());
  const downloadTimestampsRef = useRef<number[]>([]);

  useEffect(() => {
    const controller = new AbortController();
    let created: HTMLViewerSession | null = null;
    let startupTimedOut = false;

    const startupTimer = window.setTimeout(() => {
      startupTimedOut = true;
      controller.abort();
      setError("HTML viewer did not start before the request timed out");
    }, VIEWER_START_TIMEOUT_MS);

    readyRef.current = false;
    frameFailureRef.current = false;
    pendingDownloadRef.current = null;
    seenRequestsRef.current.clear();
    downloadTimestampsRef.current = [];
    setSession(null);
    setReady(false);
    setError(null);
    setPendingDownload(null);
    setPendingExternal(null);
    void createHTMLViewer(path, query, fragment, controller.signal)
      .then((next) => {
        const validated = validateViewerSession(next);

        if (controller.signal.aborted) {
          void revokeHTMLViewer(validated.id).catch(() => {});

          return;
        }

        created = validated;
        setSession(validated);
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted && !startupTimedOut) {
          setError(
            displayError(cause instanceof Error ? cause : null, "HTML viewer could not start"),
          );
        }
      })
      .finally(() => {
        window.clearTimeout(startupTimer);
      });

    return () => {
      controller.abort();
      window.clearTimeout(startupTimer);
      setSession(null);

      if (created) void revokeHTMLViewer(created.id).catch(() => {});
    };
  }, [generation, path, query, refreshGeneration]);

  useLayoutEffect(() => {
    if (!session) return;
    readyRef.current = false;
    frameFailureRef.current = false;

    const readyTimer = window.setTimeout(() => {
      if (!readyRef.current && !frameFailureRef.current) {
        setError("HTML viewer did not become ready before the request timed out");
      }
    }, VIEWER_READY_TIMEOUT_MS);

    return () => window.clearTimeout(readyTimer);
  }, [session]);

  const postToViewer = useCallback(
    (message: ViewerCommand) => {
      if (!session || !iframeRef.current?.contentWindow) return;

      try {
        iframeRef.current.contentWindow.postMessage(
          {
            version: BRIDGE_VERSION,
            nonce: session.nonce,
            viewerId: session.id,
            notePath: path,
            ...message,
          },
          "*",
        );
      } catch (cause: unknown) {
        frameFailureRef.current = true;
        setError(displayError(cause instanceof Error ? cause : null, "HTML viewer is unavailable"));
      }
    },
    [path, session],
  );

  useEffect(() => {
    if (!ready) return;
    postToViewer({ type: "set-fragment", fragment: fragment || "" });
  }, [fragment, postToViewer, ready]);

  useLayoutEffect(() => {
    if (!session) return;

    const receive = (event: MessageEvent<unknown>) => {
      if (event.source !== iframeRef.current?.contentWindow) return;

      if (!isAllowedMessageOrigin(event.origin, session.url)) return;
      const message = parseViewerMessage(event.data, session, path);

      if (!message) return;
      const requestKey = `${message.type}:${message.requestId}`;

      if (!rememberRequest(seenRequestsRef.current, requestKey)) return;

      if (message.type === "ready") {
        readyRef.current = true;
        setReady(true);
        postToViewer({ type: "ack", requestId: message.requestId });

        return;
      }

      if (message.type === "error") {
        frameFailureRef.current = true;
        setError(displayError(new Error(message.message), "HTML prototype reported an error"));

        return;
      }

      if (message.type === "navigate" || message.type === "external") {
        let destination: URL;

        try {
          destination = new URL(message.resolved);
        } catch {
          setError("The prototype requested an invalid link");

          return;
        }

        const viewerOrigin = new URL(session.url).origin;

        if (message.type === "navigate" && destination.origin === viewerOrigin) {
          let decodedPath: string;

          try {
            decodedPath = decodeURIComponent(destination.pathname.replace(/^\//, ""));
          } catch {
            setError("The prototype requested an invalid link");

            return;
          }

          const target = `${decodedPath}${destination.search}${destination.hash}`;
          onNavigate(target, message.beside, message.authored);
        } else if (destination.protocol === "https:" || destination.protocol === "http:") {
          setPendingExternal({ href: destination.href });
        } else {
          setError(`The prototype requested an unsupported ${destination.protocol || "link"}`);
        }

        return;
      }

      if (message.type !== "download") return;

      if (pendingDownloadRef.current) {
        postToViewer({
          type: "download-result",
          requestId: message.requestId,
          ok: false,
          message: "Another download is already awaiting a response",
        });

        return;
      }

      if (
        message.size < 0 ||
        message.size > MAX_DOWNLOAD_BYTES ||
        message.data.byteLength !== message.size ||
        !consumeDownloadAllowance(downloadTimestampsRef.current, Date.now())
      ) {
        postToViewer({
          type: "download-result",
          requestId: message.requestId,
          ok: false,
          message:
            message.size < 0 ||
            message.size > MAX_DOWNLOAD_BYTES ||
            message.data.byteLength !== message.size
              ? "Invalid download request"
              : "Download request limit reached; wait before trying again",
        });

        return;
      }

      const nextPendingDownload = {
        requestId: message.requestId,
        filename: safeFilename(message.filename),
        mediaType: safeMediaType(message.mediaType),
        bytes: message.data,
      };

      pendingDownloadRef.current = nextPendingDownload;
      setPendingDownload(nextPendingDownload);
    };

    window.addEventListener("message", receive);

    return () => window.removeEventListener("message", receive);
  }, [onNavigate, path, postToViewer, session]);

  const completeDownload = useCallback(() => {
    const current = pendingDownloadRef.current;

    if (!current) return;

    try {
      const objectURL = URL.createObjectURL(new Blob([current.bytes], { type: current.mediaType }));
      const anchor = document.createElement("a");
      anchor.href = objectURL;
      anchor.download = current.filename;
      anchor.click();
      URL.revokeObjectURL(objectURL);
      postToViewer({ type: "download-result", requestId: current.requestId, ok: true });
    } catch (cause: unknown) {
      postToViewer({
        type: "download-result",
        requestId: current.requestId,
        ok: false,
        message: displayError(
          cause instanceof Error ? cause : null,
          "Download could not be prepared",
        ),
      });
      setError(
        displayError(cause instanceof Error ? cause : null, "Download could not be prepared"),
      );
    } finally {
      pendingDownloadRef.current = null;
      setPendingDownload(null);
    }
  }, [postToViewer]);

  const cancelDownload = useCallback(() => {
    const current = pendingDownloadRef.current;

    if (!current) return;
    postToViewer({
      type: "download-result",
      requestId: current.requestId,
      ok: false,
      message: "Download canceled",
    });
    pendingDownloadRef.current = null;
    setPendingDownload(null);
  }, [postToViewer]);

  const handleFrameError = useCallback(() => {
    frameFailureRef.current = true;
    setError("HTML viewer could not load its content");
  }, []);

  const attachFrame = useCallback(
    (frame: HTMLIFrameElement | null) => {
      iframeRef.current = frame;

      if (!frame) return;
      frame.addEventListener("error", handleFrameError);

      return () => {
        frame.removeEventListener("error", handleFrameError);
        iframeRef.current = null;
      };
    },
    [handleFrameError],
  );

  const frameTitle = useMemo(() => `Interactive HTML note: ${path}`, [path]);

  return (
    <section
      className={`html-note-viewer${visible ? "" : " is-hidden"}`}
      aria-label="HTML note viewer"
    >
      <div className="html-note-viewer__toolbar">
        <span className="html-note-viewer__format">HTML</span>
        <span className="html-note-viewer__state" role="status">
          {error ? "Viewer problem" : ready ? "" : "Loading prototype…"}
        </span>
        <span className="html-note-viewer__spacer" />
        <button type="button" onClick={() => setRefreshGeneration((value) => value + 1)}>
          Refresh
        </button>
        {bare ? (
          <a href={applicationHref}>Open in Rhizome</a>
        ) : (
          <a href={bareNoteHref(applicationHref)} target="_blank" rel="noreferrer">
            Open in browser tab
          </a>
        )}
      </div>
      {error && (
        <div className="html-note-viewer__problem" role="alert">
          {error}
        </div>
      )}
      {pendingExternal && (
        <div className="html-note-viewer__action">
          <span title={pendingExternal.href}>Prototype link</span>
          <button
            type="button"
            onClick={() => window.open(pendingExternal.href, "_blank", "noopener,noreferrer")}
          >
            Open external link
          </button>
          <button type="button" onClick={() => setPendingExternal(null)}>
            Cancel
          </button>
        </div>
      )}
      {pendingDownload && (
        <div className="html-note-viewer__action">
          <span>
            {pendingDownload.filename} · {fileSizeLabel(pendingDownload.bytes.byteLength)}
          </span>
          <button type="button" onClick={completeDownload}>
            Download
          </button>
          <button type="button" onClick={cancelDownload}>
            Cancel
          </button>
        </div>
      )}
      {session && (
        <iframe
          ref={attachFrame}
          className="html-note-viewer__frame"
          src={session.url}
          title={frameTitle}
          sandbox="allow-scripts"
          referrerPolicy="no-referrer"
          hidden={!visible}
        />
      )}
    </section>
  );
}
