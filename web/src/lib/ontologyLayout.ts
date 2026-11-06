import { layoutOntologyGraph, type OntologyGraphModel } from "./ontologyGraph";
import { packComponents } from "./ontologyPacking";

export function computeOntologyLayout(graph: OntologyGraphModel): Promise<OntologyGraphModel> {
  // Bundled ELK cannot initialize inside another worker. Offload the expensive packing pass.
  return layoutOntologyGraph(graph, packOntologyLayout);
}

export function packOntologyLayout(graph: OntologyGraphModel): Promise<OntologyGraphModel> {
  if (typeof Worker === "undefined") return Promise.resolve(packComponents(graph));

  return new Promise((resolve, reject) => {
    let worker: Worker | undefined;
    let finished = false;

    const fallback = () => {
      if (finished) return;
      finished = true;
      worker?.terminate();

      try {
        resolve(packComponents(graph));
      } catch (error) {
        reject(error);
      }
    };

    try {
      worker = new Worker(new URL("./ontologyLayout.worker.ts", import.meta.url), {
        type: "module",
      });
      worker.onmessage = (event: MessageEvent<{ graph?: OntologyGraphModel; error?: string }>) => {
        if (finished) return;

        if (!event.data.graph) {
          fallback();

          return;
        }

        finished = true;
        worker?.terminate();
        resolve(event.data.graph);
      };

      worker.onerror = (event) => {
        event.preventDefault();
        fallback();
      };

      worker.onmessageerror = fallback;
      worker.postMessage(graph);
    } catch {
      fallback();
    }
  });
}
