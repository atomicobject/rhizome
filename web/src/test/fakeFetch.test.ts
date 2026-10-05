import { describe, expect, it } from "vitest";

import { ApiError, fetchJSON, getPublicNodeDetail, getStatus } from "../api/client";
import type { StatusResponse } from "../api/types";
import {
  deferredReply,
  FakeFetch,
  type FakeFetchRequest,
  graphQLRequestBody,
  jsonReply,
  withFakeFetch,
} from "./fakeFetch";

type StatusStub = Pick<StatusResponse, "vaultName" | "vaultPath">;

type SeenRequest = { signal?: AbortSignal };

describe("fakeFetch", () => {
  const http = withFakeFetch();

  it("serves JSON to the real API client and logs the request", async () => {
    http.json("GET", "/api/v1/status", { vaultName: "v", vaultPath: "/tmp/v" });

    await expect(getStatus()).resolves.toMatchObject({ vaultName: "v" });
    expect(http.count("GET", "/api/v1/status")).toBe(1);
    expect(http.calls[0]?.headers.get("content-type")).toBeNull();
  });

  it("turns non-2xx JSON into ApiError through the real client", async () => {
    http.json("GET", "/api/v1/status", { error: "nope", code: "DOWN" }, 503);

    await expect(getStatus()).rejects.toMatchObject({ status: 503, code: "DOWN" });
    await expect(getStatus()).rejects.toBeInstanceOf(ApiError);
  });

  it("matches GraphQL POSTs by operation name and exposes variables", async () => {
    http.onGraphQL("PublicNodeDetail", (request) =>
      jsonReply({ data: { node: { ref: graphQLRequestBody(request).variables?.ref } } }),
    );
    http.onGraphQL("OtherOperation", () =>
      jsonReply({ data: { node: { ref: "wrong-operation" } } }),
    );

    const result = await getPublicNodeDetail("notes/a.md");
    expect(result.data).toEqual({ node: { ref: "notes/a.md" } });
    expect(http.requests("POST", "/api/v1/graphql")).toHaveLength(1);
  });

  it("lets a test reorder completions with deferred replies", async () => {
    const first = deferredReply<StatusStub>();
    const second = deferredReply<StatusStub>();
    http
      .on("GET", "/api/v1/status", () => first.promise, {
        when: (request) => request.query.get("n") === "1",
      })
      .on("GET", "/api/v1/status", () => second.promise, {
        when: (request) => request.query.get("n") === "2",
      });

    const order: string[] = [];
    const one = fetchJSON<StatusStub>("/api/v1/status?n=1").then((s) => order.push(s.vaultName));
    const two = fetchJSON<StatusStub>("/api/v1/status?n=2").then((s) => order.push(s.vaultName));
    second.resolve({ vaultName: "two", vaultPath: "" });
    await two;
    first.resolve({ vaultName: "one", vaultPath: "" });
    await one;

    expect(order).toEqual(["two", "one"]);
    expect(http.calls.map((call) => call.query.get("n"))).toEqual(["1", "2"]);
  });

  it("rejects a pending request with AbortError when its signal aborts", async () => {
    const pending = deferredReply<StatusStub>();
    http.on("GET", "/api/v1/status", () => pending.promise);
    const controller = new AbortController();

    const request = getStatus({ signal: controller.signal });
    controller.abort();

    await expect(request).rejects.toMatchObject({ name: "AbortError" });
  });

  it("preserves Request fields and applies RequestInit overrides", async () => {
    const inheritedController = new AbortController();
    const overrideController = new AbortController();

    const input = new Request("http://localhost/api/v1/status?n=1", {
      method: "POST",
      headers: { "x-source": "request" },
      body: JSON.stringify({ source: "request" }),
      signal: inheritedController.signal,
    });

    const seen: SeenRequest = {};

    const fake = new FakeFetch()
      .on("PUT", "/api/v1/status", (request) => {
        expect(request.query.get("n")).toBe("1");
        expect(request.headers.get("x-source")).toBe("init");
        expect(request.body).toBe(JSON.stringify({ source: "init" }));

        if (request.signal) seen.signal = request.signal;

        return jsonReply({ ok: true });
      })
      .install();

    await expect(
      fetch(input, {
        method: "PUT",
        headers: { "x-source": "init" },
        body: JSON.stringify({ source: "init" }),
        signal: overrideController.signal,
      }),
    ).resolves.toBeInstanceOf(Response);
    overrideController.abort();
    expect(seen.signal?.aborted).toBe(true);
    expect(inheritedController.signal.aborted).toBe(false);
    fake.restore();
  });

  it("preserves inherited Request fields and abort propagation without init", async () => {
    const controller = new AbortController();

    const input = new Request("http://localhost/api/v1/status?n=1", {
      method: "POST",
      headers: { "x-source": "request" },
      body: JSON.stringify({ source: "request" }),
      signal: controller.signal,
    });

    const pending = deferredReply<{ ok: boolean }>();
    let recordRequest: (request: FakeFetchRequest) => void = () => {};

    const requestSeen = new Promise<FakeFetchRequest>((resolve) => {
      recordRequest = resolve;
    });

    const fake = new FakeFetch()
      .on("POST", "/api/v1/status", (request) => {
        recordRequest(request);

        return pending.promise;
      })
      .install();

    const response = fetch(input);
    const request = await requestSeen;
    expect(request.query.get("n")).toBe("1");
    expect(request.headers.get("x-source")).toBe("request");
    expect(request.body).toBe(JSON.stringify({ source: "request" }));

    controller.abort();
    expect(request.signal?.aborted).toBe(true);
    await expect(response).rejects.toMatchObject({ name: "AbortError" });
    fake.restore();
  });

  it("surfaces responder errors as network failures", async () => {
    http.on("GET", "/api/v1/status", () => {
      throw new Error("offline");
    });

    await expect(getStatus()).rejects.toThrow("offline");
  });

  it("fails loudly for requests with no responder", async () => {
    const fake = new FakeFetch().install();

    await expect(fetch("/api/v1/nothing")).rejects.toThrow(
      /no responder for GET \/api\/v1\/nothing/,
    );
    expect(() => fake.restore()).toThrow(/unmatched requests\n {2}GET \/api\/v1\/nothing/);
  });
});
