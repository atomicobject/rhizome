import { describe, expect, it, vi } from "vitest";
import { installMonacoEnvironment } from "./monacoEnvironment";

// jsdom has no Worker implementation, so the factories hand back a real
// EventTarget-backed stand-in that satisfies the Worker contract Monaco uses.
class StubWorker extends EventTarget implements Worker {
  onmessage: Worker["onmessage"] = null;
  onmessageerror: Worker["onmessageerror"] = null;
  onerror: Worker["onerror"] = null;

  constructor(readonly label: string) {
    super();
  }

  postMessage() {}
  terminate() {}
}

describe("installMonacoEnvironment", () => {
  it("routes GraphQL, JSON, and editor work to their bundled workers", () => {
    const editor = new StubWorker("editor");
    const graphql = new StubWorker("graphql");
    const json = new StubWorker("json");

    // SAFETY: installMonacoEnvironment only reads and writes `MonacoEnvironment`
    // on its target, so a bare object stands in for the global scope here.
    const target = {} as typeof globalThis & {
      MonacoEnvironment?: {
        getWorker(workerId: string, label: string): Worker;
      };
    };

    const factories = {
      editor: vi.fn(() => editor),
      graphql: vi.fn(() => graphql),
      json: vi.fn(() => json),
    };

    installMonacoEnvironment(target, factories);

    expect(target.MonacoEnvironment?.getWorker("1", "graphql")).toBe(graphql);
    expect(target.MonacoEnvironment?.getWorker("2", "json")).toBe(json);
    expect(target.MonacoEnvironment?.getWorker("3", "editorWorkerService")).toBe(editor);
    expect(factories.graphql).toHaveBeenCalledOnce();
    expect(factories.json).toHaveBeenCalledOnce();
    expect(factories.editor).toHaveBeenCalledOnce();
  });
});
