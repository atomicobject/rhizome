// Runs mine with fake clients and SIGKILLs itself once N model calls have
// returned, leaving a held lock and a half-finished run behind.
//   node crash-run.mjs STATE REPO N
import { SESSIONS, fakeClients, fakeDiscover } from "./fakes.mjs";

const { mine } = await import(new URL("../../templates/starters/session-memory/agents/skills/session-memory/scripts/mine.mjs", import.meta.url));
const [state, repo, n] = process.argv.slice(2);
await mine({ root: repo, state, budget: 5, harness: "claude", concurrency: 1, discover: fakeDiscover([SESSIONS.s1, SESSIONS.s2]), log: () => {} }, fakeClients({ killAfter: Number(n) }));
