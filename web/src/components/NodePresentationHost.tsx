import { createContext, useContext, type ReactNode } from "react";

import { ViewHost, type ViewRuntimeProps } from "../views/ViewHost";

const NodeBodyContext = createContext<ReactNode>(null);

function NativeNodePresentation() {
  return useContext(NodeBodyContext);
}

const renderers = { read: NativeNodePresentation, source: NativeNodePresentation };

export function NodePresentationHost({ body, ...runtime }: ViewRuntimeProps & { body: ReactNode }) {
  return (
    <NodeBodyContext.Provider value={body}>
      <ViewHost {...runtime} renderers={renderers} embedded />
    </NodeBodyContext.Provider>
  );
}
