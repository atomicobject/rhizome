import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach } from "vitest";

import { clearPreferenceStores } from "../viewPreferences/store";
import { preferenceServer } from "../viewPreferences/testServer";

/** One request as seen by the fake `fetch`. `path` is the pathname; query lives in `query`. */
export type FakeFetchRequest = {
  readonly method: string;
  readonly url: string;
  readonly path: string;
  readonly query: URLSearchParams;
  readonly headers: Headers;
  readonly body: string | null;
  readonly signal: AbortSignal | null;
};

export type FakeFetchResponder = (request: FakeFetchRequest) => Response | Promise<Response>;

type FakeFetchRoute = {
  readonly method: string;
  readonly path: string;
  readonly when?: (request: FakeFetchRequest) => boolean;
  readonly responder: FakeFetchResponder;
};

export type DeferredReply<T> = {
  readonly promise: Promise<Response>;
  resolve(body: T, status?: number): void;
  reject(error: Error): void;
};

type JsonPrimitive = string | number | boolean | null;

export type JsonValue = JsonPrimitive | JsonValue[] | { [key: string]: JsonValue };

export type JsonObject = { [key: string]: JsonValue };

export type GraphQLRequestBody = {
  readonly operationName?: string;
  readonly query?: string;
  readonly variables?: JsonObject;
};

/** Build a JSON `Response` the way the Rhizome API serves it. */
export function jsonReply<T>(body: T, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

export function emptyValidationSummariesReply(request: FakeFetchRequest): Response {
  // SAFETY: test callers register this responder only for the typed validation-summary request.
  const body = JSON.parse(request.body || "{}") as {
    generation?: number;
    scopes?: Array<{ kind: string; key?: string }>;
  };

  return jsonReply({
    generation: body.generation ?? 0,
    summaries: (body.scopes ?? []).map((scope) => ({
      scope,
      issueCount: 0,
      affectedFileCount: 0,
      affectedNoteCount: 0,
      repairActionCount: 0,
    })),
  });
}

export function emptyValidationGroupsReply(request: FakeFetchRequest): Response {
  // SAFETY: test callers register this responder only for the typed issue-group request.
  const body = JSON.parse(request.body || "{}") as { generation?: number };

  return jsonReply({ generation: body.generation ?? 0, groups: [] });
}

/** A reply the test completes later, so request completions can be reordered. */
export function deferredReply<T>(): DeferredReply<T> {
  let resolveResponse: (response: Response) => void = () => {};

  let rejectResponse: (error: Error) => void = () => {};

  const promise = new Promise<Response>((resolve, reject) => {
    resolveResponse = resolve;
    rejectResponse = reject;
  });

  return {
    promise,
    resolve: (body, status = 200) => resolveResponse(jsonReply(body, status)),
    reject: (error) => rejectResponse(error),
  };
}

function isTextBody(body: BodyInit | null | undefined): body is string {
  return typeof body === "string";
}

function isJsonObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isGraphQLRequestBody(value: unknown): value is GraphQLRequestBody {
  if (!isJsonObject(value)) return false;
  const operationName = value.operationName;
  const query = value.query;
  const variables = value.variables;

  return (
    (operationName === undefined || typeof operationName === "string") &&
    (query === undefined || typeof query === "string") &&
    (variables === undefined || isJsonObject(variables))
  );
}

/** Parse a JSON request body through a type guard; throws when the body is missing or malformed. */
export function decodeJSONBody<T>(
  request: FakeFetchRequest,
  isBody: (value: unknown) => value is T,
): T {
  const label = `${request.method} ${request.path}`;

  if (request.body === null) throw new Error(`fakeFetch: ${label} has no body`);
  const parsed: unknown = JSON.parse(request.body);

  if (!isBody(parsed)) throw new Error(`fakeFetch: ${label} body did not match the expected type`);

  return parsed;
}

export function graphQLRequestBody(request: FakeFetchRequest): GraphQLRequestBody {
  return decodeJSONBody(request, isGraphQLRequestBody);
}

function toRequest(
  input: RequestInfo | URL,
  init?: RequestInit,
): FakeFetchRequest | Promise<FakeFetchRequest> {
  if (input instanceof Request) {
    const request = new Request(input, init);
    const parsed = new URL(request.url);

    const finish = (body: string | null): FakeFetchRequest => ({
      method: request.method.toUpperCase(),
      url: request.url,
      path: parsed.pathname,
      query: parsed.searchParams,
      headers: new Headers(request.headers),
      body,
      signal: request.signal,
    });

    return request.body === null ? finish(null) : request.clone().text().then(finish);
  }

  const url = String(input);
  const parsed = new URL(url, "http://localhost");
  const method = init?.method ?? "GET";
  const body = init?.body;

  return {
    method: method.toUpperCase(),
    url,
    path: parsed.pathname,
    query: parsed.searchParams,
    headers: new Headers(init?.headers),
    body: isTextBody(body) ? body : null,
    signal: init?.signal ?? null,
  };
}

function untilAborted(reply: Promise<Response>, signal: AbortSignal | null): Promise<Response> {
  if (!signal) return reply;

  return new Promise<Response>((resolve, reject) => {
    const onAbort = () => reject(signal.reason ?? new DOMException("Aborted", "AbortError"));
    signal.addEventListener("abort", onAbort, { once: true });
    reply.then(resolve, reject).finally(() => signal.removeEventListener("abort", onAbort));
  });
}

export class FakeFetch {
  readonly calls: FakeFetchRequest[] = [];
  readonly unmatched: FakeFetchRequest[] = [];
  private routes: FakeFetchRoute[] = [];
  private originalFetch: PropertyDescriptor | undefined;
  private installed = false;

  readonly fetch = (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const request = toRequest(input, init);

    return request instanceof Promise
      ? request.then((resolved) => this.respond(resolved))
      : this.respond(request);
  };

  private respond(request: FakeFetchRequest): Promise<Response> {
    this.calls.push(request);
    const { signal } = request;

    if (signal?.aborted) {
      return Promise.reject(signal.reason ?? new DOMException("Aborted", "AbortError"));
    }

    const route = this.matchRoute(request);

    if (!route) {
      this.unmatched.push(request);

      return Promise.reject(new Error(this.describeUnmatched(request)));
    }

    // Clone so a shared Response (e.g. one deferred reply answering several
    // requests) is never consumed twice.
    const reply = Promise.resolve()
      .then(() => route.responder(request))
      .then((response) => response.clone());

    return untilAborted(reply, signal);
  }

  /** Register a responder; later registrations win, so tests can override `beforeEach` defaults. */
  on(
    method: string,
    path: string,
    responder: FakeFetchResponder,
    options: { when?: (request: FakeFetchRequest) => boolean } = {},
  ): this {
    this.routes.push({ method: method.toUpperCase(), path, when: options.when, responder });

    return this;
  }

  json<T>(method: string, path: string, body: T, status = 200): this {
    return this.on(method, path, () => jsonReply(body, status));
  }

  onGraphQL(operationName: string, responder: FakeFetchResponder): this {
    return this.on("POST", "/api/v1/graphql", responder, {
      when: (request) => graphQLRequestBody(request).operationName === operationName,
    });
  }

  requests(method?: string, path?: string): FakeFetchRequest[] {
    const wanted = method?.toUpperCase();

    return this.calls.filter(
      (call) => (!wanted || call.method === wanted) && (!path || call.path === path),
    );
  }

  count(method: string, path: string): number {
    return this.requests(method, path).length;
  }

  reset(): void {
    this.routes = [];
    this.calls.length = 0;
    this.unmatched.length = 0;
  }

  install(): this {
    if (this.installed) return this;
    this.originalFetch = Object.getOwnPropertyDescriptor(globalThis, "fetch");
    Object.defineProperty(globalThis, "fetch", {
      configurable: true,
      writable: true,
      value: this.fetch,
    });
    this.installed = true;

    return this;
  }

  /** Puts the real `fetch` back and fails the test if any request had no responder. */
  restore(): void {
    if (!this.installed) return;
    this.installed = false;

    if (this.originalFetch) {
      Object.defineProperty(globalThis, "fetch", this.originalFetch);
    } else {
      Reflect.deleteProperty(globalThis, "fetch");
    }

    if (this.unmatched.length > 0) {
      const lines = this.unmatched.map((request) => `  ${request.method} ${request.path}`);
      throw new Error(`fakeFetch: unmatched requests\n${lines.join("\n")}`);
    }
  }

  private matchRoute(request: FakeFetchRequest): FakeFetchRoute | undefined {
    for (let index = this.routes.length - 1; index >= 0; index -= 1) {
      const candidate = this.routes[index];

      if (
        candidate.method === request.method &&
        candidate.path === request.path &&
        (candidate.when?.(request) ?? true)
      ) {
        return candidate;
      }
    }

    return undefined;
  }

  private describeUnmatched(request: FakeFetchRequest): string {
    const registered = this.routes.map((route) => `  ${route.method} ${route.path}`);

    return `fakeFetch: no responder for ${request.method} ${request.path}\nRegistered:\n${registered.join("\n") || "  (none)"}`;
  }
}

/** Installs a fresh fake before each test and restores (failing on unmatched requests) after. */
export function withFakeFetch(): FakeFetch {
  const fake = new FakeFetch();
  beforeEach(() => {
    fake.reset();
    clearPreferenceStores();
    fake.install();
    preferenceServer(fake);
  });
  afterEach(() => {
    // Unmount first: a throwing hook would otherwise leave this test's tree
    // mounted for the next one and turn one failure into several.
    cleanup();
    clearPreferenceStores();
    fake.restore();
  });

  return fake;
}
