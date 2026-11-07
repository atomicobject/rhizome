import type { OntologyGraphModel } from "./ontologyGraph";
import { computeOntologyLayout } from "./ontologyLayout";

/** Keep geometry across navigation; counts and descriptions always come from the current atlas. */
export function createOntologyLayoutCache(
  compute: (graph: OntologyGraphModel) => Promise<OntologyGraphModel>,
) {
  const entries = new Map<string, Promise<OntologyGraphModel>>();

  return async (graph: OntologyGraphModel): Promise<OntologyGraphModel> => {
    const key = JSON.stringify([
      graph.nodes.map(({ id, width, height }) => [id, width, height]),
      graph.edges,
    ]);

    let pending = entries.get(key);

    if (!pending) {
      pending = compute(graph);
      entries.set(key, pending);

      // Layouts belong to this loaded application version and cannot survive an algorithm change.
      if (entries.size > 4) entries.delete(entries.keys().next().value!);
    } else {
      entries.delete(key);
      entries.set(key, pending);
    }

    let laidOut: OntologyGraphModel;

    try {
      laidOut = await pending;
    } catch (error) {
      if (entries.get(key) === pending) entries.delete(key);
      throw error;
    }

    const current = new Map(graph.nodes.map((node) => [node.id, node]));

    return {
      nodes: laidOut.nodes.map((node) => {
        const latest = current.get(node.id)!;
        const result = { ...latest, position: { ...node.position }, data: { ...latest.data } };

        if (node.data.selfRelations) result.data.selfRelations = [...node.data.selfRelations];

        return result;
      }),
      edges: laidOut.edges.map((edge) => {
        const result = { ...edge };

        if (edge.data) result.data = { ...edge.data };

        if (edge.points) result.points = edge.points.map((point) => ({ ...point }));

        return result;
      }),
    };
  };
}

export const cachedOntologyLayout = createOntologyLayoutCache(computeOntologyLayout);
