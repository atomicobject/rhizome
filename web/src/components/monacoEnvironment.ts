export type MonacoWorkerFactories = {
  editor: () => Worker;
  graphql: () => Worker;
  json: () => Worker;
};

type MonacoEnvironment = {
  getWorker(workerId: string, label: string): Worker;
};

type MonacoGlobal = typeof globalThis & {
  MonacoEnvironment?: MonacoEnvironment;
};

export function installMonacoEnvironment(target: MonacoGlobal, factories: MonacoWorkerFactories) {
  target.MonacoEnvironment = {
    getWorker(_workerId, label) {
      if (label === "graphql") return factories.graphql();

      if (label === "json") return factories.json();

      return factories.editor();
    },
  };
}
