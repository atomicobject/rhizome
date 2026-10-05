// The public vault event stream's event names, and an in-page hub that lets
// other host code (framed custom views) see each event the app bridge receives.
// EventSource has no wildcard listener, so every consumer subscribes by name.

export const INDEX_EVENTS = [
  "index.changed",
  "index.invalidated",
  "node.changed",
  "schema.invalidated",
  "query_recipe.invalidated",
  "capabilities.invalidated",
  "edit_session.invalidated",
] as const;

export const VALIDATION_EVENTS = ["validate.changed", "validation.invalidated"] as const;

/** A view folder under `.rhizome/views` changed. `{ id, kind, data: { folder } }` */
export const VIEWS_CHANGED_EVENT = "views.changed";

export const VAULT_EVENTS = [
  ...INDEX_EVENTS,
  ...VALIDATION_EVENTS,
  VIEWS_CHANGED_EVENT,
  "view_preferences.changed",
] as const;

/**
 * Not a server event: the stream reopened after an outage. The server does not
 * replay events missed meanwhile, so listeners refresh everything.
 */
export const STREAM_RECONNECTED_EVENT = "stream.reconnected";

/** One event as the server sent it; `data` is the raw payload text, omitted when empty. */
export type VaultEvent = { event: string; data?: string };

const listeners = new Set<(event: VaultEvent) => void>();

export function subscribeVaultEvents(listener: (event: VaultEvent) => void): () => void {
  listeners.add(listener);

  return () => listeners.delete(listener);
}

export function publishVaultEvent(event: VaultEvent) {
  for (const listener of listeners) listener(event);
}
