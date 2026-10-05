import { decodeJson, isJsonObject, isString } from "../api/parse";
import { VAULT_EVENT_MESSAGE } from "../lib/customViewMessages";
import {
  STREAM_RECONNECTED_EVENT,
  subscribeVaultEvents,
  type VaultEvent,
} from "../query/vaultEvents";
import { refreshPreferenceStores, invalidatePreferenceFamily } from "./store";
import { isPreferenceScope } from "./types";

export const VIEW_PREFERENCES_EVENT = "view_preferences.changed";

function receive({ event, data }: VaultEvent) {
  if (event === STREAM_RECONNECTED_EVENT) refreshPreferenceStores();

  if (event !== VIEW_PREFERENCES_EVENT) return;
  const payload = decodeJson(data, isJsonObject);
  const body = payload && isJsonObject(payload.data) ? payload.data : payload;

  if (body && isPreferenceScope(body.scope)) {
    const vaultKey = isString(body.vaultKey) ? body.vaultKey : undefined;

    if (body.includeSlots === true) invalidatePreferenceFamily(body.scope, vaultKey);
    else refreshPreferenceStores(body.scope, vaultKey);
  }
}

let connected = false;

export function connectPreferenceEvents() {
  if (connected) return;
  connected = true;
  subscribeVaultEvents(receive);
  window.addEventListener("message", ({ source, origin, data }: MessageEvent) => {
    if (
      source !== window.parent ||
      origin !== window.location.origin ||
      !isJsonObject(data) ||
      data.type !== VAULT_EVENT_MESSAGE ||
      !isString(data.event)
    )
      return;
    const event: VaultEvent = { event: data.event };

    if (isString(data.data)) event.data = data.data;
    receive(event);
  });
  window.addEventListener("focus", () => refreshPreferenceStores());
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) refreshPreferenceStores();
  });
}
