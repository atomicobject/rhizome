import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { publishVaultEvent } from "../query/vaultEvents";
import { StarterReadiness, type StarterState } from "./StarterReadiness";

afterEach(() => vi.unstubAllGlobals());

it("keeps the actual blocker visible and posts the selected product through recovery", async () => {
  let state: StarterState[] = [
    {
      module: "ao-kb",
      title: "AO Knowledge Base",
      state: "required_input",
      reason: "Choose a product",
      recovery: [
        { kind: "choice", label: "Select product", input: "product", choices: ["alpha", "beta"] },
      ],
    },
  ];

  const fetch = vi.fn(async (_url: string, init?: RequestInit) => {
    if (init?.method === "POST")
      state = [{ module: "ao-kb", title: "AO Knowledge Base", state: "ready" }];

    return new Response(JSON.stringify(state), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  });

  vi.stubGlobal("fetch", fetch);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  const view = render(
    <QueryClientProvider client={client}>
      <StarterReadiness />
    </QueryClientProvider>,
  );

  expect(await screen.findByRole("status")).toBeInTheDocument();
  fireEvent.change(screen.getByRole("combobox"), { target: { value: "beta" } });
  fireEvent.click(screen.getByRole("button", { name: "Use this product" }));
  await waitFor(() =>
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/starter-readiness/ao-kb",
      expect.objectContaining({ method: "POST", body: '{"kind":"choice","value":"beta"}' }),
    ),
  );
  publishVaultEvent({ event: "starter-readiness.changed" });
  await waitFor(() => expect(screen.queryByRole("combobox")).not.toBeInTheDocument());
  state = [
    {
      module: "ao-kb",
      title: "AO Knowledge Base",
      state: "blocked",
      reason: "artifact and Artifact collide in existing.graphql",
    },
  ];
  publishVaultEvent({ event: "stream.reconnected" });
  expect(
    await screen.findByText("artifact and Artifact collide in existing.graphql"),
  ).toBeInTheDocument();
  view.unmount();
  client.clear();
});
