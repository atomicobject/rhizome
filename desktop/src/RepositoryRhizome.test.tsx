import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { RepositoryRhizome, type RepositoryRhizomeProps } from "./RepositoryRhizome";

function props(overrides: Partial<RepositoryRhizomeProps> = {}): RepositoryRhizomeProps {
  return {
    name: "acme",
    worktree: {
      path: "/work/acme",
      configured: true,
      hasDatabase: true,
      trusted: true,
      trustRequired: false,
      authority: "managed",
      pinnedVersion: "v0.51.0",
    },
    app: { path: "/app/rzm", version: "v0.51.0", bundled: true },
    onSelect: vi.fn().mockResolvedValue(undefined),
    onChoose: vi.fn().mockResolvedValue(true),
    onAdvance: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}
function show(p = props(), unsupported = true) {
  const onReload = vi.fn().mockResolvedValue(undefined);
  const view = render(
    <RepositoryRhizome {...p} unsupported={unsupported} disabled={false} onReload={onReload} />,
  );
  return { ...view, onReload };
}

describe("repository Rhizome version", () => {
  it("offers the app when no newer pin exists and reloads after selecting it", async () => {
    const p = props();
    const { onReload } = show(p);
    expect(screen.getByRole("radio", { name: /Pinned version, v0.51.0/ })).toBeChecked();
    expect(screen.getByText("Trusted on this Mac")).toBeVisible();
    const action = screen.getByRole("button", {
      name: "Use the app's Rhizome for this repository",
    });
    expect(action).toHaveClass("primary");
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
    expect(screen.getByText(/acme runs Rhizome v0.51.0/)).toBeVisible();
    fireEvent.click(action);
    await waitFor(() => expect(onReload).toHaveBeenCalledOnce());
    expect(p.onSelect).toHaveBeenCalledWith("/app/rzm");
    expect(vi.mocked(p.onSelect).mock.invocationCallOrder[0]).toBeLessThan(
      onReload.mock.invocationCallOrder[0],
    );
  });

  it("leads with the pin advance when one is available", async () => {
    const p = props();
    p.worktree.pinOffer = { state: "advance", pinned: "v0.51.0", available: "v0.52.0" };
    const { onReload } = show(p);
    const action = screen.getByRole("button", { name: "Update acme to v0.52.0" });
    expect(action).toHaveClass("primary");
    expect(screen.getByRole("button", { name: "Use the app's Rhizome instead" })).not.toHaveClass(
      "primary",
    );
    fireEvent.click(action);
    await waitFor(() => expect(onReload).toHaveBeenCalledOnce());
    expect(p.onAdvance).toHaveBeenCalledOnce();
  });

  it("reflects app and custom choices, restores the pin, and opens the picker", async () => {
    const p = props({ chosen: "/app/rzm" });
    const { rerender, onReload } = show(p, false);
    expect(screen.getByRole("radio", { name: /The app's Rhizome/ })).toBeChecked();
    fireEvent.click(screen.getByRole("radio", { name: /Pinned version/ }));
    await waitFor(() => expect(p.onSelect).toHaveBeenCalledWith(null));
    await waitFor(() => expect(onReload).toHaveBeenCalledOnce());
    rerender(
      <RepositoryRhizome
        {...p}
        chosen="/custom/rzm"
        unsupported={false}
        disabled={false}
        onReload={onReload}
      />,
    );
    expect(screen.getByRole("radio", { name: /Another executable/ })).toBeChecked();
    expect(screen.getByText("/custom/rzm")).toBeVisible();
    fireEvent.click(screen.getByRole("radio", { name: /Another executable/ }));
    await waitFor(() => expect(p.onChoose).toHaveBeenCalledOnce());
  });

  it("keeps the actual bundled executable available with a global override", async () => {
    const p = props({
      app: {
        path: "/custom/global-rzm",
        version: "v0.50.0",
        bundled: false,
        bundledPath: "/app/rzm",
      },
    });
    show(p);
    fireEvent.click(
      screen.getByRole("button", { name: "Use the app's Rhizome for this repository" }),
    );
    await waitFor(() => expect(p.onSelect).toHaveBeenCalledWith("/app/rzm"));
  });

  it("shows source authority without offering ineffective overrides", () => {
    const p = props();
    p.worktree = { ...p.worktree, authority: "development", executable: "/source/bin/rzm" };
    show(p, false);
    expect(screen.getByText(/Development source checkout/)).toBeVisible();
    expect(screen.getByText("/source/bin/rzm")).toBeVisible();
    expect(screen.queryByRole("radio")).toBeNull();
  });

  it.each(["external", "global"] as const)(
    "adapts %s authority and hides an unavailable app",
    (authority) => {
      const p = props({ app: null });
      p.worktree = { ...p.worktree, authority, pinnedVersion: undefined };
      show(p, false);
      expect(
        screen.getByRole("radio", {
          name: authority === "external" ? /External binary manager/ : /Global Rhizome/,
        }),
      ).toBeChecked();
      expect(screen.queryByRole("radio", { name: /Pinned version/ })).toBeNull();
      expect(screen.queryByRole("radio", { name: /The app's Rhizome/ })).toBeNull();
    },
  );

  it("reports a failed selection without claiming success or reloading", async () => {
    const p = props({
      onSelect: vi.fn().mockRejectedValue({
        code: "runtime_error",
        message: "Cannot restart an attached runtime.",
      }),
    });
    const { onReload } = show(p);
    fireEvent.click(
      screen.getByRole("button", { name: "Use the app's Rhizome for this repository" }),
    );
    expect(await screen.findByText("Cannot restart an attached runtime.")).toBeVisible();
    expect(onReload).not.toHaveBeenCalled();
  });
});
