import { fetchJSON } from "./client";

export type HTMLViewerSession = {
  id: string;
  url: string;
  nonce: string;
  expiresAt: string;
};

export function createHTMLViewer(
  path: string,
  query: string | null | undefined,
  fragment: string | null | undefined,
  signal?: AbortSignal,
): Promise<HTMLViewerSession> {
  return fetchJSON<HTMLViewerSession>("/api/v1/html-viewers", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path, query: query || "", fragment: fragment || "" }),
    signal,
  });
}

export async function revokeHTMLViewer(id: string): Promise<void> {
  const response = await fetch(`/api/v1/html-viewers/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });

  if (!response.ok && response.status !== 404) {
    throw new Error(`Could not close HTML viewer (${response.status})`);
  }
}
