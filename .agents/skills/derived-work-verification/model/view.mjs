// Labels for reading DerivedWork.tla results with the /statechart skill.
export const title = "How a committed change reaches derived output (one kind)";

const values = (o) => Object.values(o ?? {});
const tickets = (v) => values(v.tix).filter((t) => t.present);

const structural = (v) => {
  if (v.batch.phase === "marked") return "Batch marked, rows not yet committed";
  if (tickets(v).some((t) => !t.ready)) return "Unready ticket forces full recovery";
  if (v.resync) return "Full discovery owed";
  if (v.pend.length > 0) return "File change waiting for the watcher";
  return "Rows match the files";
};

const runLabels = {
  prepare: "Group waiting to prepare",
  compute: "Text at the provider",
  publish: "Publishing result batches",
  final: "Result waiting to publish",
};

const outputsCurrent = (v) =>
  Object.entries(v.out).every(([, built]) =>
    Object.entries(built).every(([path, value]) => v.idx[path] === value));

const derived = (v) => {
  if (v.sch.phase !== "idle") return runLabels[v.sch.phase];
  if (tickets(v).length > 0) return "Tickets waiting for the scheduler";
  return outputsCurrent(v) ? "Outputs current" : "Outputs stale with no ticket";
};

export const state = (v) => ({ "Structural rows": structural(v), "Derived work": derived(v) });

export const group = (v) =>
  v.sch.phase !== "idle" || v.batch.phase !== "idle"
    ? "working"
    : tickets(v).length > 0 || v.resync
      ? "waiting"
      : "settled";
export const colors = { working: "blue", waiting: "yellow", settled: "teal" };

const deleted = (from, to) =>
  Object.keys(from.tix).some((s) => from.tix[s].present && !to.tix[s].present);

export const step = (action, from, to) => {
  switch (action) {
    case "Edit": return "a file is edited";
    case "DirectSave": return "a save publishes rows without a ticket";
    case "EventLost": return "a file event is lost";
    case "Restart": return "the runtime restarts";
    case "BatchMarks": return "the watcher batch marks tickets";
    case "BatchCompletes": return "the batch commits rows and activates tickets";
    case "BatchInterrupted": return "the batch is cancelled after marking";
    case "SchedulerReads": return "the scheduler reads ready tickets";
    case "Prepare": return to.sch.phase === "compute" ? "the group is planned and witnessed" : "every member has changed";
    case "ComputeReturns": return "the provider returns vectors";
    case "PassFails": return "the pass fails or is preempted";
    case "PublishBatch": return "a result batch is published for current members";
    case "NotesFinal":
    case "OntologyFinal":
      if (to.sch.phase === "prepare" && action === "OntologyFinal") return "a member changed, so survivors are replanned";
      return deleted(from, to) ? "the run acknowledges its tickets" : "every member has changed";
    case "ExplicitIndex": return "an explicit index runs";
    case "Done": return null;
    default: return action;
  }
};
