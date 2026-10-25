import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";

import { publishVaultEvent } from "../query/vaultEvents";
import { withFakeFetch } from "../test/fakeFetch";
import { clearPreferenceStores } from "../viewPreferences/store";
import { preferenceServer } from "../viewPreferences/testServer";
import type { ViewPreferenceScope } from "../viewPreferences/types";
import { ViewPreferencesStatus } from "./ViewPreferencesStatus";

const http = withFakeFetch();

const host: ViewPreferenceScope = { viewId: "desk", context: { kind: "group", group: "IP" } };

const widget: ViewPreferenceScope = { ...host, widgetSlot: "lanes" };

afterEach(clearPreferenceStores);

it("offers Reset to shared only while the view or one of its widgets holds an override", async () => {
  const server = preferenceServer(http);
  render(<ViewPreferencesStatus scope={host} vaultKey="vault" includeWidgets />);
  await waitFor(() => expect(http.count("GET", "/api/v1/view-preferences")).toBe(1));
  await act(async () => {});

  expect(screen.queryByRole("button", { name: "Reset to shared" })).toBeNull();

  // A widget's own write makes its host refresh and offer the family reset.
  server.change(widget, { lens: "impact" });
  act(() =>
    publishVaultEvent({
      event: "view_preferences.changed",
      data: JSON.stringify({ scope: widget, vaultKey: "vault", revision: 1 }),
    }),
  );
  expect(await screen.findByRole("button", { name: "Reset to shared" })).toBeVisible();
});

it("ignores widget overrides for a host that does not reset its widgets", async () => {
  const server = preferenceServer(http);
  server.change(widget, { lens: "impact" });
  render(<ViewPreferencesStatus scope={host} vaultKey="vault" />);
  await waitFor(() => expect(http.count("GET", "/api/v1/view-preferences")).toBe(1));
  await act(async () => {});

  expect(screen.queryByRole("button", { name: "Reset to shared" })).toBeNull();
});
