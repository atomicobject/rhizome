declare module "graphology-layout-forceatlas2" {
  import type Graph from "graphology";

  /** Mirrors graphology-layout-forceatlas2/defaults.js. */
  export interface ForceAtlas2Settings {
    linLogMode?: boolean;
    outboundAttractionDistribution?: boolean;
    adjustSizes?: boolean;
    edgeWeightInfluence?: number;
    scalingRatio?: number;
    strongGravityMode?: boolean;
    gravity?: number;
    slowDown?: number;
    barnesHutOptimize?: boolean;
    barnesHutTheta?: number;
  }

  export interface ForceAtlas2Options {
    iterations?: number;
    settings?: ForceAtlas2Settings;
  }

  export function inferSettings(graph: Graph, options?: ForceAtlas2Settings): ForceAtlas2Settings;
  export function assign(graph: Graph, options?: ForceAtlas2Options): void;

  const forceAtlas2: {
    inferSettings: typeof inferSettings;
    assign: typeof assign;
  };

  export default forceAtlas2;
}
