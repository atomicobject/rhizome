import { describe, expect, it, vi } from "vitest";
import { deferredReply, jsonReply, withFakeFetch } from "../test/fakeFetch";
import { PreferenceStore } from "./store";
import { preferenceServer } from "./testServer";
import type { ViewPreferenceResponse, ViewPreferenceScope } from "./types";

const http = withFakeFetch();

const scope: ViewPreferenceScope = { viewId: "recovery", context: { kind: "standalone" } };

describe("preference recovery", () => {
  it.each([400, 413, 422])(
    "keeps controls hydrated after terminal migration status %i while preserving source evidence",
    async (status) => {
      preferenceServer(http);
      const source = "legacy-invalid-preferences";
      window.localStorage.setItem(source, "browser evidence");
      const acknowledged = vi.fn(() => window.localStorage.removeItem(source));
      http.on("POST", "/api/v1/view-preferences/import", () =>
        jsonReply({ error: "invalid legacy preferences" }, status),
      );
      const migration = { migrationId: source, values: { density: "compact" }, acknowledged };
      const store = new PreferenceStore(scope, "vault", [migration]);
      await store.refresh();
      expect(store.getSnapshot()).toMatchObject({ loading: false, values: {} });
      expect(store.getSnapshot().error?.message).toContain("invalid legacy preferences");
      expect(acknowledged).not.toHaveBeenCalled();
      expect(window.localStorage.getItem(source)).toBe("browser evidence");
      await store.patch({ set: { density: "comfortable" } });
      await store.resetAll();
      store.addMigrations([migration]);
      await store.retry();
      expect(store.getSnapshot()).toMatchObject({
        loading: false,
        pending: false,
        error: null,
        values: {},
      });
      expect(http.requests("POST", "/api/v1/view-preferences/import")).toHaveLength(1);
      expect(window.localStorage.getItem(source)).toBe("browser evidence");
      window.localStorage.removeItem(source);
    },
  );

  it.each([401, 403, 500])("keeps migration status %i retryable", async (status) => {
    preferenceServer(http);
    http.on("POST", "/api/v1/view-preferences/import", () =>
      jsonReply({ error: "temporarily unavailable" }, status),
    );
    const acknowledged = vi.fn();

    const store = new PreferenceStore(scope, "vault", [
      { migrationId: "retry-source", values: { density: "compact" }, acknowledged },
    ]);

    await expect(store.refresh()).rejects.toThrow("temporarily unavailable");
    expect(acknowledged).not.toHaveBeenCalled();
    preferenceServer(http);
    await store.retry();
    expect(store.getSnapshot()).toMatchObject({ values: { density: "compact" }, error: null });
    expect(acknowledged).toHaveBeenCalledOnce();
  });

  it("accepts an authoritative lower revision after the database is replaced", async () => {
    const original = preferenceServer(http);
    original.change(scope, { density: "compact" });
    original.change(scope, { density: "dense" });
    const store = new PreferenceStore(scope, "vault", []);
    await store.refresh();
    expect(store.getSnapshot()).toMatchObject({ revision: 2, values: { density: "dense" } });
    const replacement = preferenceServer(http);
    await store.refresh();
    expect(store.getSnapshot()).toMatchObject({ revision: 0, values: {} });
    await store.patch({ set: { density: "normal" } });
    expect(replacement.read(scope).values).toEqual({ density: "normal" });
  });

  it("keeps a committed write when an older read completes after it", async () => {
    const server = preferenceServer(http);
    server.change(scope, { density: "compact" });
    const store = new PreferenceStore(scope, "vault", []);
    await store.refresh();
    const old = { ...server.read(scope), values: { ...server.read(scope).values } };
    const delayed = deferredReply<ViewPreferenceResponse>();
    let first = true;
    http.on("GET", "/api/v1/view-preferences", () => {
      if (first) {
        first = false;

        return delayed.promise;
      }

      return jsonReply(server.read(scope));
    });
    const refresh = store.refresh();
    await store.patch({ set: { density: "normal" } });
    delayed.resolve(old);
    await refresh;
    expect(store.getSnapshot()).toMatchObject({ revision: 2, values: { density: "normal" } });
  });

  it("keeps a newer authoritative read when an earlier write response arrives late", async () => {
    const server = preferenceServer(http);
    server.change(scope, { density: "compact" });
    const store = new PreferenceStore(scope, "vault", []);
    await store.refresh();
    const delayed = deferredReply<ViewPreferenceResponse>();
    http.on("PATCH", "/api/v1/view-preferences", () => {
      server.change(scope, { density: "normal" });

      return delayed.promise;
    });
    const patch = store.patch({ set: { density: "normal" } });
    await vi.waitFor(() =>
      expect(http.requests("PATCH", "/api/v1/view-preferences")).toHaveLength(1),
    );
    const committed = { ...server.read(scope), values: { ...server.read(scope).values } };
    server.change(scope, { density: "normal", remote: true });
    await store.refresh();
    delayed.resolve(committed);
    await patch;
    expect(store.getSnapshot()).toMatchObject({
      revision: 3,
      values: { density: "normal", remote: true },
    });
  });
});
