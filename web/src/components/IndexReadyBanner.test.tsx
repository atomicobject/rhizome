import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import type { StatusResponse } from "../api/types";
import { withFakeEventSource } from "../test/fakeEventSource";
import { deferredReply, jsonReply, withFakeFetch } from "../test/fakeFetch";
import { IndexReadyBanner } from "./IndexReadyBanner";

function makeStatus(state: StatusResponse["indexState"]): StatusResponse {
  return {
    vaultName: "test",
    vaultPath: "/tmp/test",
    embeddings: false,
    codeIndex: false,
    validation: {
      status: "never_ran",
      health: "never_checked",
      generation: 0,
      publishedGeneration: 0,
    },
    updatedAt: new Date().toISOString(),
    ready: true,
    indexState: state,
  };
}

describe("IndexReadyBanner", () => {
  const http = withFakeFetch();
  const sources = withFakeEventSource();
  let statusCalls = 0;

  beforeEach(() => {
    statusCalls = 0;
  });

  function statusReplies(...states: StatusResponse["indexState"][]) {
    http.on("GET", "/api/v1/status", () => {
      const next = (states.length > 1 ? states.shift() : states[0]) ?? "ready";
      statusCalls += 1;

      return jsonReply(makeStatus(next));
    });
  }

  it("renders nothing while status has not resolved", () => {
    const pending = deferredReply<StatusResponse>();
    http.on("GET", "/api/v1/status", () => pending.promise);

    const { container } = render(<IndexReadyBanner />);
    expect(container.firstChild).toBeNull();

    pending.resolve(makeStatus("ready"));
  });

  it("hides when the initial status is already ready", async () => {
    statusReplies("ready");

    render(<IndexReadyBanner />);

    await waitFor(() => expect(statusCalls).toBe(1));
    expect(screen.queryByTestId("index-ready-banner")).toBeNull();
  });

  it("shows the banner during initialization and hides on index.changed -> ready", async () => {
    statusReplies("initializing", "ready");

    render(<IndexReadyBanner />);

    const banner = await screen.findByTestId("index-ready-banner");
    expect(banner).toHaveTextContent("Preparing index…");

    await waitFor(() => expect(sources.instances).toHaveLength(1));
    const source = sources.latest();
    expect(source.url).toBe("/api/v1/events");

    await act(async () => {
      source.emit("", "index.changed");
    });

    await waitFor(() => {
      expect(screen.queryByTestId("index-ready-banner")).toBeNull();
    });
    expect(statusCalls).toBe(2);
  });

  it("closes the EventSource when the component unmounts", async () => {
    statusReplies("initializing");

    const { unmount } = render(<IndexReadyBanner />);
    await screen.findByTestId("index-ready-banner");

    await waitFor(() => expect(sources.instances).toHaveLength(1));
    const source = sources.latest();

    unmount();
    expect(source.closed).toBe(true);
  });
});
