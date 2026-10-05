import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { withFakeFetch } from "../test/fakeFetch";
import { nativePreferenceScope } from "../viewPreferences/native";
import { preferenceServer } from "../viewPreferences/testServer";
import { baseView, execution, renderView } from "./ConfiguredView.testFixtures";

describe("invalid native preference feedback", () => {
  const http = withFakeFetch();
  const scope = nativePreferenceScope(baseView);

  it("shows an invalid density with defaults and keeps Retry and Reset usable", async () => {
    const server = preferenceServer(http);
    server.change(scope, { "native.density": "unsupported" });
    const data = execution();
    renderView({
      vaultKey: "vault",
      execution: {
        ...data,
        // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- TypeProfile.shape is the generated API contract field.
        profile: { shape: "catalog", ...data.profile, summaryField: "summary" },
      },
    });

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Some view preferences could not load.");
    expect(screen.getByRole("table")).toHaveClass("configured-view__table--two-line");
    const reads = http.requests("GET", "/api/v1/view-preferences").length;
    fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
    await waitFor(() =>
      expect(http.requests("GET", "/api/v1/view-preferences").length).toBeGreaterThan(reads),
    );
    expect(screen.getByRole("alert")).toBeVisible();
    expect(server.read(scope).values).toEqual({ "native.density": "unsupported" });
    fireEvent.click(screen.getByRole("button", { name: "Reset to shared" }));
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
    expect(server.read(scope).values).toEqual({});
    expect(screen.getByRole("table")).toHaveClass("configured-view__table--two-line");
  });

  it("keeps a malformed query preference visible until a successful Retry reads valid data", async () => {
    const server = preferenceServer(http);
    server.change(scope, { "native.sort": [{ field: "title", direction: "sideways" }] });
    renderView({ vaultKey: "vault" });
    const alert = await screen.findByRole("alert");
    expect(screen.getByRole("button", { name: "Reset to shared" })).toBeVisible();
    server.change(scope, { "native.sort": [{ field: "title", direction: "desc" }] });
    fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  });

  it("reports malformed group expansion instead of silently using the default", async () => {
    const server = preferenceServer(http);
    server.change(scope, { "native.tableGroups": { grouping: { active: "no" } } });
    renderView({ vaultKey: "vault" });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Some view preferences could not load.",
    );
    expect(screen.getByRole("button", { name: "Active 1" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    fireEvent.click(screen.getByRole("button", { name: "Reset to shared" }));
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  });
});
