export const title = "How an indexed change reaches the embeddings (live scheduler)";

const ticket = (v) => {
  if (v.comp === "running") return "Text at the provider";
  if (v.comp === "done") return "Vectors waiting to publish";
  if (v.spin) return "Retrying at once: file newer than index";
  if (v.backoff === "priority") return "Retry delayed by a writer";
  if (v.backoff === "provider") return "Retry delayed by the provider";
  if (v.ticket === "ready") return "Ticket ready";
  if (v.emb !== v.idx) return "Embeddings stale, no ticket";
  return "Embeddings current";
};

export const state = (v) => ({
  Embeddings: ticket(v),
  "Interactive writer": v.hold ? "Writer waiting for the lock" : "No writer",
});

export const group = (v) =>
  v.comp !== "idle" ? "working" : v.ticket !== "none" ? "waiting" : "settled";
export const colors = { working: "blue", waiting: "yellow", settled: "teal" };

const labels = {
  EditOutside: "a file is edited",
  EventArrives: "the file event arrives",
  EventOverflows: "events overflow, or a folder or rename event arrives",
  WriterArrives: "a save or CLI write asks for the lock",
  WriterSaves: "the writer saves the file",
  WriterLeaves: "the writer finishes",
  BatchRunsWhole: "the watcher batch indexes the change",
  DerivedStarts: "the scheduler sends the text",
  DerivedSpins: "the scheduler finds the file changed and tries again",
  ProviderReturns: "the provider returns vectors",
  ProviderFails: "the provider fails",
  ComputeCancelledByWriter: "the request is cancelled for the writer",
  BackoffExpires: "the delay ends",
  DerivedPublishes: "the vectors are published and the ticket is acknowledged",
  DerivedDiscards: "the vectors are set aside as out of date",
};
export const step = (action) => labels[action] ?? action;
