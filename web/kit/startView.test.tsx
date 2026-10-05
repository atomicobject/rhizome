import { act, render, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { afterEach, expect, it, vi } from "vitest";

import { VAULT_EVENT_MESSAGE } from "../src/lib/customViewMessages";
import { withFakeFetch } from "../src/test/fakeFetch";
import { startView, type ViewConfig, type ViewModule } from "./startView";

const http = withFakeFetch();

const board: ViewConfig = {
  id: "board",
  name: "Board",
  origin: "repository",
  folder: "board",
  stamp: "/views/_stamp/board",
  stampValue: "1",
  check: "/views/_check/board",
};

let stop: () => void = () => undefined;

afterEach(() => {
  stop();
  document.getElementById("rhizome-view-invocation")?.remove();
});

function changed(folder: string) {
  window.dispatchEvent(
    new MessageEvent("message", {
      source: window.parent,
      origin: window.location.origin,
      data: {
        type: VAULT_EVENT_MESSAGE,
        event: "views.changed",
        data: JSON.stringify({ id: "e-1", kind: "views.changed", data: { folder } }),
      },
    }),
  );
}

async function start(load: () => Promise<ViewModule>, view: ViewConfig = board) {
  const reload = vi.fn();
  const rendered: ReactElement[] = [];

  http.on("GET", "/views/_stamp/board", () => new Response("1"));
  await act(async () => {
    stop = await startView(load, view, {
      hosted: true,
      reload,
      render: (element) => {
        rendered.push(element);
        render(element);
      },
    });
  });

  return { reload, rendered };
}

it("reloads a hosted view whose module failed to load once its folder changes", async () => {
  http.on("GET", "/views/_check/board", () => new Response("board/main.tsx: unexpected token"));

  const { reload } = await start(async () => {
    throw new Error("Failed to fetch dynamically imported module");
  });

  expect(screen.getByRole("alert").textContent).toContain("Board failed to load");
  expect(screen.getByRole("alert").textContent).toContain("unexpected token");
  changed("other");
  expect(reload).not.toHaveBeenCalled();
  changed("board");
  expect(reload).toHaveBeenCalledTimes(1);
});

it("reloads a hosted module that mounts itself, without rendering it", async () => {
  const { reload, rendered } = await start(async () => ({}));

  expect(rendered).toEqual([]);
  changed("board");
  expect(reload).toHaveBeenCalledTimes(1);
});

it("reloads for a change that arrives while the module is loading", async () => {
  let finish: (module: ViewModule) => void = () => undefined;

  const loading = start(() => new Promise<ViewModule>((resolve) => (finish = resolve)));

  await waitFor(() => expect(http.requests("GET", "/views/_stamp/board")).toHaveLength(1));
  changed("board");
  finish({ default: () => <p>Board</p> });
  const { reload } = await loading;

  expect(reload).toHaveBeenCalledTimes(1);
  expect(screen.getByText("Board")).toBeTruthy();
});

it("reloads a hosted view edited between page serve and startup", async () => {
  http.on("GET", "/views/_stamp/board", () => new Response("2"));
  const reload = vi.fn();

  await act(async () => {
    stop = await startView(async () => ({ default: () => null }), board, {
      hosted: true,
      reload,
      render,
    });
  });
  await waitFor(() => expect(reload).toHaveBeenCalledTimes(1));
});

it("reloads a hosted view after a reconnect only when its folder changed meanwhile", async () => {
  let served = "1";
  const reload = vi.fn();

  http.on("GET", "/views/_stamp/board", () => new Response(served));
  await act(async () => {
    stop = await startView(async () => ({ default: () => null }), board, {
      hosted: true,
      reload,
      render,
    });
  });

  const reconnected = () =>
    window.dispatchEvent(
      new MessageEvent("message", {
        source: window.parent,
        origin: window.location.origin,
        data: { type: VAULT_EVENT_MESSAGE, event: "stream.reconnected" },
      }),
    );

  reconnected();
  await waitFor(() => expect(http.requests("GET", "/views/_stamp/board")).toHaveLength(2));
  expect(reload).not.toHaveBeenCalled();

  // An edit while the stream was down sent its views.changed to no one.
  served = "2";
  reconnected();
  await waitFor(() => expect(reload).toHaveBeenCalledTimes(1));
});

it("never reloads a bundled view and checks no stamp for it", async () => {
  const { reload } = await start(async () => ({ default: () => null }), {
    id: "group.briefing",
    name: "Briefing",
    origin: "bundled",
    folder: "board",
  });

  changed("board");
  expect(reload).not.toHaveBeenCalled();
  expect(http.requests("GET", "/views/_stamp/board")).toHaveLength(0);
});

it("shows why a view with an invalid invocation could not start", async () => {
  const script = document.createElement("script");
  script.id = "rhizome-view-invocation";
  script.type = "application/json";
  script.textContent = JSON.stringify({ view: { id: "board" }, context: { kind: "standalone" } });
  document.head.append(script);
  const load = vi.fn(async () => ({ default: () => null }));

  const { reload } = await start(load);

  expect(screen.getByRole("alert").textContent).toContain("Board could not start");
  expect(screen.getByRole("alert").textContent).toContain("Invalid server view invocation");
  expect(load).not.toHaveBeenCalled();
  changed("board");
  expect(reload).toHaveBeenCalledTimes(1);
});
