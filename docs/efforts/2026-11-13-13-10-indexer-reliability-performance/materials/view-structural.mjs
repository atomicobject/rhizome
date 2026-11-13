export const title = "How a file change reaches the index (live watcher)";

const watcher = (v) => {
  if (v.lane === "batch") return v.bscope === "full" ? "Whole-vault batch running" : "One-path batch running";
  if (v.input === "full") return v.why === "yield" ? "Whole vault queued after a yield" : "Whole vault queued";
  if (v.input === "path") return "One path queued";
  if (v.evt) return "Change not seen yet";
  return v.idx === v.disk ? "Caught up" : "Index behind, nothing queued";
};

export const state = (v) => ({
  Watcher: watcher(v),
  "Interactive writer": v.hold ? "Writer waiting for the lock" : "No writer",
});

export const group = (v) => (v.lane === "batch" ? "working" : v.input !== "none" || v.evt ? "waiting" : "settled");
export const colors = { working: "blue", waiting: "yellow", settled: "teal" };

const labels = {
  EditOutside: "a file is edited",
  EventArrives: "the file event arrives",
  EventOverflows: "events overflow, or a folder or rename event arrives",
  WriterArrives: "a save or CLI write asks for the lock",
  WriterSaves: "the writer saves the file",
  WriterLeaves: "the writer finishes",
  BatchStarts: "the watcher starts a batch",
  BatchCompletes: "the batch commits",
  BatchYields: "the batch yields to the writer",
  BatchFails: "the batch fails",
};
export const step = (action) => labels[action] ?? action;
