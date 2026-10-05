import type * as Monaco from "monaco-editor";
// Monaco's worker entry does not publish TypeScript declarations.
// @ts-expect-error -- supported Monaco worker initialization entrypoint
import { initialize } from "monaco-editor/esm/vs/editor/editor.worker.js";
import type { ICreateData } from "monaco-graphql";
import { GraphQLWorker } from "monaco-graphql/esm/GraphQLWorker.js";

globalThis.onmessage = () => {
  initialize(
    (ctx: Monaco.worker.IWorkerContext, createData: ICreateData) =>
      new GraphQLWorker(ctx, createData),
  );
};
