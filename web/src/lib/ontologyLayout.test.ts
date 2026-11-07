import { afterEach, expect, it, vi } from "vitest";
import { packOntologyLayout } from "./ontologyLayout";
import { buildOntologyGraph, type OntologyGraphModel } from "./ontologyGraph";

afterEach(() => vi.unstubAllGlobals());

it("runs packing in a worker and releases it when the geometry returns", async () => {
  const terminate = vi.fn();
  const input = buildOntologyGraph({ schemaPresent: true, types: [] });

  class FakeWorker {
    onmessage?: (event: { data: unknown }) => void;
    terminate = terminate;
    postMessage(graph: OntologyGraphModel) {
      expect(graph).toBe(input);
      queueMicrotask(() => this.onmessage?.({ data: { graph: input } }));
    }
  }

  vi.stubGlobal("Worker", FakeWorker);
  await expect(packOntologyLayout(input)).resolves.toEqual(input);
  expect(terminate).toHaveBeenCalledOnce();
});

it("falls back to synchronous packing when the worker fails", async () => {
  const terminate = vi.fn();

  class FailedWorker {
    onerror?: (event: { message: string; preventDefault: () => void }) => void;
    terminate = terminate;
    postMessage() {
      queueMicrotask(() => this.onerror?.({ message: "worker failed", preventDefault: vi.fn() }));
    }
  }

  vi.stubGlobal("Worker", FailedWorker);
  await expect(packOntologyLayout({ nodes: [], edges: [] })).resolves.toEqual({
    nodes: [],
    edges: [],
  });
  expect(terminate).toHaveBeenCalledOnce();
});

for (const failure of ["construction", "message"] as const) {
  it(`falls back when worker ${failure} fails`, async () => {
    class BrokenWorker {
      onmessageerror?: () => void;
      terminate() {}
      constructor() {
        if (failure === "construction") throw new Error("unavailable");
      }
      postMessage() {
        queueMicrotask(() => this.onmessageerror?.());
      }
    }

    vi.stubGlobal("Worker", BrokenWorker);
    await expect(packOntologyLayout({ nodes: [], edges: [] })).resolves.toEqual({
      nodes: [],
      edges: [],
    });
  });
}
