import { type LayoutInput, layoutPositions } from "./graphLayout";

self.onmessage = (event: MessageEvent<LayoutInput>) => {
  self.postMessage(layoutPositions(event.data));
};
