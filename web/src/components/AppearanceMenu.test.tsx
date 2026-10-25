import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { applyStored } from "../lib/appearance";
import { AppearanceMenu } from "./AppearanceMenu";

const root = document.documentElement;

afterEach(() => {
  localStorage.clear();
  delete root.dataset.appearance;
  delete root.dataset.theme;
});

describe("AppearanceMenu", () => {
  it("sets and remembers the appearance and theme on the document root", async () => {
    render(<AppearanceMenu />);

    fireEvent.click(screen.getByRole("button", { name: "Appearance" }));
    fireEvent.click(screen.getByRole("button", { name: "Dark" }));
    fireEvent.click(screen.getByRole("button", { name: "Spruce" }));

    expect(root.dataset.appearance).toBe("dark");
    expect(root.dataset.theme).toBe("spruce");
    expect(localStorage.getItem("rhizome:appearance")).toBe("dark");
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Spruce" })).toHaveAttribute(
        "aria-pressed",
        "true",
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "Match system" }));

    expect(root.dataset.appearance).toBeUndefined();
  });

  it("restores a stored choice and ignores unknown values", () => {
    localStorage.setItem("rhizome:appearance", "light");
    localStorage.setItem("rhizome:theme", "neon");

    applyStored();

    expect(root.dataset.appearance).toBe("light");
    expect(root.dataset.theme).toBe("graphite");
  });

  it("closes on Escape and returns focus to the trigger", () => {
    render(<AppearanceMenu />);

    const trigger = screen.getByRole("button", { name: "Appearance" });

    fireEvent.click(trigger);
    fireEvent.keyDown(screen.getByRole("dialog", { name: "Appearance" }), { key: "Escape" });

    expect(screen.queryByRole("dialog", { name: "Appearance" })).toBeNull();
    expect(trigger).toHaveFocus();
  });
});
