import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { publishVaultEvent } from "../query/vaultEvents";
import { StarterReadiness, type StarterState } from "./StarterReadiness";

afterEach(() => {
  vi.unstubAllGlobals();
  delete window.__RHIZOME_DESKTOP__;
});

function renderWith(state: StarterState[]) {
  const fetch = vi.fn(
    async () =>
      new Response(JSON.stringify(state), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
  );

  vi.stubGlobal("fetch", fetch);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  const view = render(
    <QueryClientProvider client={client}>
      <StarterReadiness />
    </QueryClientProvider>,
  );

  return { fetch, view, client };
}

const untrusted =
  "this repository is not trusted; run `rzm trust` in /tmp/kb to let its actions run";

it("offers one trust action, since trusting resumes setup, and keeps the raw reason in Details", async () => {
  window.__RHIZOME_DESKTOP__ = true;

  const { view, client } = renderWith([
    {
      module: "ao-kb",
      title: "AO Knowledge Base",
      state: "blocked",
      reason: untrusted,
      recovery: [
        { kind: "trust", label: "Trust this repository in Settings" },
        { kind: "retry", label: "Retry" },
      ],
    },
  ]);

  expect(
    await screen.findByRole("button", { name: "Trust this repository in Settings" }),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  expect(screen.getByRole("status")).not.toHaveTextContent("/tmp/kb");
  expect(screen.getByText(untrusted)).toBeInTheDocument();

  window.__RHIZOME_DESKTOP__ = false;
  view.rerender(
    <QueryClientProvider client={client}>
      <StarterReadiness />
    </QueryClientProvider>,
  );
  expect(screen.getByRole("status")).toHaveTextContent("run rzm trust in this repository");
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
  view.unmount();
  client.clear();
});

it("keeps a failed run's raw error in Details and retries it", async () => {
  const error =
    "ao-kb/observe: exit status 1\nError [ERR_MODULE_NOT_FOUND]: Cannot find package 'yaml'";

  const { fetch, view, client } = renderWith([
    {
      module: "ao-kb",
      title: "AO Knowledge Base",
      state: "failed",
      reason: error,
      recovery: [{ kind: "retry", label: "Retry" }],
    },
  ]);

  expect(await screen.findByRole("status")).toHaveTextContent("AO Knowledge Base · Setup failed");
  expect(screen.getByRole("status")).not.toHaveTextContent("exit status 1");
  const details = screen.getByText("Details").closest("details")!;
  expect(within(details).getByText(/Cannot find package 'yaml'/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await waitFor(() =>
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/starter-readiness/ao-kb",
      expect.objectContaining({ method: "POST", body: '{"kind":"retry"}' }),
    ),
  );
  view.unmount();
  client.clear();
});

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
  expect(await screen.findByRole("status")).toHaveTextContent(
    "artifact and Artifact collide in existing.graphql",
  );
  view.unmount();
  client.clear();
});
