import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ErrorBoundary } from "./ErrorBoundary";

function BrokenView({ shouldThrow }: { shouldThrow: boolean }) {
  if (shouldThrow) throw new Error("render exploded");

  return <div>Recovered view</div>;
}

describe("ErrorBoundary", () => {
  let consoleError: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
  });

  afterEach(() => {
    consoleError.mockRestore();
  });

  it("keeps a render failure from blanking the app and exposes details", () => {
    const { rerender } = render(
      <ErrorBoundary label="Rhizome could not render this view">
        <BrokenView shouldThrow={true} />
      </ErrorBoundary>,
    );

    expect(screen.getByRole("alert")).toHaveTextContent("Rhizome could not render this view");
    expect(screen.getByRole("alert")).toHaveTextContent("render exploded");
    expect(screen.getByText("Error details")).toBeVisible();

    rerender(
      <ErrorBoundary label="Rhizome could not render this view">
        <BrokenView shouldThrow={false} />
      </ErrorBoundary>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));

    expect(screen.getByText("Recovered view")).toBeVisible();
  });
});
