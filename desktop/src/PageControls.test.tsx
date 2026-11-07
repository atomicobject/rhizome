import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { PageSections, PageTools } from "./PageControls";
import type { PageState } from "./api";

const state = {
  section: "tasks",
  search: "",
  tasks: { needs: 3 },
  issues: 0,
} as PageState;

afterEach(() => {
  delete window.__RHIZOME_PLATFORM__;
});

it("keeps every section available through the compact selector", () => {
  const command = vi.fn();
  render(<PageSections state={state} onCommand={command} />);
  const select = screen.getByRole("combobox", { name: "Current section" });
  expect(select).toHaveValue("tasks");
  expect(screen.getByRole("option", { name: "Tasks (3)" })).toBeInTheDocument();
  expect(screen.getAllByRole("option")).toHaveLength(6);
  fireEvent.change(select, { target: { value: "graphql" } });
  expect(command).toHaveBeenCalledWith({ command: "section", section: "graphql" });
});

it("retains an accessible search input and Linux shortcut when compact", () => {
  window.__RHIZOME_PLATFORM__ = "linux";
  const command = vi.fn();
  const ref = { current: null };
  render(<PageTools state={state} searchRef={ref} onCommand={command} />);
  const input = screen.getByRole("searchbox", { name: "Search this project" });
  expect(ref.current).toBe(input);
  expect(screen.getByText("Ctrl K")).toBeInTheDocument();
  fireEvent.change(input, { target: { value: "find this" } });
  fireEvent.submit(screen.getByRole("search"));
  expect(command).toHaveBeenCalledWith({ command: "search", query: "find this" });
});
