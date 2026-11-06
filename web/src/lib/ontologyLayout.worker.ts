import type { OntologyGraphModel } from "./ontologyGraph";
import { packComponents } from "./ontologyPacking";

self.onmessage = (event: MessageEvent<OntologyGraphModel>) => {
  try {
    self.postMessage({ graph: packComponents(event.data) });
  } catch (error) {
    self.postMessage({ error: error instanceof Error ? error.message : String(error) });
  }
};
