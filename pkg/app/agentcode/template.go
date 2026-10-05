package agentcode

const clientModuleTemplate = `import { spawn } from "node:child_process";
import { isAbsolute } from "node:path";

const CONTRACT_HASH = "__CONTRACT_HASH__";
const SELECTED = __SELECTED__;
const PROTOCOL_VERSION = "1";
const DEFAULT_TIMEOUT_MS = __DEFAULT_CALL_TIMEOUT_MS__;
const DEFAULT_OUTPUT_LIMIT = 1024 * 1024;
const CLOSE_GRACE_MS = 1_000;
const MAX_QUEUED_CALLS = 32;

export class CodeModeError extends Error {
  constructor(code, message, details = {}) {
    super(message); this.name = "CodeModeError"; this.code = code; this.details = details;
  }
}

function requireOptions(options) {
  if (!options || typeof options !== "object") throw new CodeModeError("invalid_options", "client options are required");
  if (typeof options.executablePath !== "string" || !isAbsolute(options.executablePath)) throw new CodeModeError("invalid_options", "executablePath must be absolute");
  if (typeof options.vaultPath !== "string" || !isAbsolute(options.vaultPath)) throw new CodeModeError("invalid_options", "vaultPath must be absolute");
  if (options.sessionId !== undefined && (typeof options.sessionId !== "string" || !options.sessionId)) throw new CodeModeError("invalid_options", "sessionId must be a non-empty string");
  if (options.readWrite !== undefined && typeof options.readWrite !== "boolean") throw new CodeModeError("invalid_options", "readWrite must be boolean");
  for (const [name, value, max] of [["concurrency", options.concurrency, 32], ["timeoutMs", options.timeoutMs, 300_000], ["outputLimitBytes", options.outputLimitBytes, 1024 * 1024]]) {
    if (value !== undefined && (!Number.isInteger(value) || value < 1 || value > max)) throw new CodeModeError("invalid_options", name + " is outside its allowed range");
  }
}

function requireInput(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new CodeModeError("invalid_input", "input must be an object");
}

function fault(frame) {
  const data = frame.error?.data;
  return new CodeModeError(data?.code || "protocol_error", frame.error?.message || "JSON-RPC request failed", data || {});
}

export function createClient(options) {
  requireOptions(options);
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const outputLimit = options.outputLimitBytes ?? DEFAULT_OUTPUT_LIMIT;
  const command = ["agent", "code", "serve", ...(options.readWrite ? ["--read-write"] : [])];
  const child = spawn(options.executablePath, command, {
    cwd: options.vaultPath, shell: false,
    // The headless server never renders terminal cells, so skip tcell's eager rune-width table.
    env: { ...process.env, RZM_SKIP_REPO_DELEGATE: "1", TCELL_MINIMIZE: "1" }, stdio: ["pipe", "pipe", "pipe"]
  });
  let closed = false, childClosed = false, fatalError, closePromise, nextID = 1, stdout = Buffer.alloc(0), stderr = "";
  const pending = new Map();
  const queue = [];
  let draining = false, active = 0;
  const concurrency = options.concurrency ?? 8;
  let resolveStopped;
  const stopped = new Promise(resolve => { resolveStopped = resolve; });
  const startup = request("initialize", { protocolVersion: PROTOCOL_VERSION, selected: SELECTED, contractHash: CONTRACT_HASH, sessionId: options.sessionId }, timeoutMs)
    .then(result => {
      if (result.protocolVersion !== PROTOCOL_VERSION || result.contractHash !== CONTRACT_HASH) throw new CodeModeError("code_mode_artifact_stale", "generated contract does not match the executable", { result });
      return result;
    });
  startup.catch(() => {});

  function fail(error) {
    if (fatalError) return;
    fatalError = error;
    for (const entry of pending.values()) {
      clearTimeout(entry.timer); clearTimeout(entry.grace);
      entry.acknowledge();
      entry.reject(new CodeModeError(error.code, error.message, { ...error.details, mayHaveExecuted: entry.method === "call" }));
    }
    pending.clear();
    for (const job of queue.splice(0)) {
      job.cleanup(); job.reject(new CodeModeError(error.code, error.message, { ...error.details, mayHaveExecuted: false }));
    }
  }
  function send(frame) {
    if (closed || fatalError) throw fatalError || new CodeModeError("client_closed", "client is closed");
    const payload = JSON.stringify({ jsonrpc: "2.0", ...frame }) + "\n";
    if (Buffer.byteLength(payload) > outputLimit) throw new CodeModeError("input_limit_exceeded", "request exceeds outputLimitBytes");
    child.stdin.write(payload);
  }
  function request(method, params, deadline, onAcknowledged = () => {}) {
    const id = nextID++;
    return new Promise((resolve, reject) => {
      let acknowledge;
      const acknowledged = new Promise(done => { acknowledge = done; });
      onAcknowledged(acknowledged);
      const entry = { resolve, reject, method, acknowledge, timer: undefined, grace: undefined };
      entry.timer = setTimeout(() => {
        try { send({ method: "$/cancelRequest", params: { id } }); } catch (error) { pending.delete(String(id)); acknowledge(); reject(error); return; }
        entry.grace = setTimeout(() => { if (pending.has(String(id))) reject(new CodeModeError("deadline_exceeded", "call deadline was not acknowledged", { mayHaveExecuted: method === "call" })); }, CLOSE_GRACE_MS);
      }, deadline);
      pending.set(String(id), entry);
      try { send({ id, method, params }); } catch (error) { clearTimeout(entry.timer); pending.delete(String(id)); acknowledge(); reject(error); }
    });
  }
  function readFrames(chunk) {
    stdout = Buffer.concat([stdout, chunk]);
    for (;;) {
      const line = stdout.indexOf(10);
      if ((line < 0 ? stdout.length : line + 1) > outputLimit) { fail(new CodeModeError("output_limit_exceeded", "server output exceeded " + outputLimit + " bytes", { stderr })); child.kill("SIGKILL"); return; }
      if (line < 0) return;
      const raw = stdout.subarray(0, line); stdout = stdout.subarray(line + 1);
      let frame; try { frame = JSON.parse(raw.toString("utf8")); } catch { fail(new CodeModeError("invalid_protocol_output", "server emitted malformed JSON", { stderr })); child.kill("SIGKILL"); return; }
      const entry = pending.get(String(frame.id)); if (!entry) continue;
      pending.delete(String(frame.id)); clearTimeout(entry.timer); clearTimeout(entry.grace); entry.acknowledge();
      if (frame.jsonrpc !== "2.0") entry.reject(new CodeModeError("invalid_protocol_output", "server omitted JSON-RPC version", { frame }));
      else if (frame.error) entry.reject(fault(frame)); else entry.resolve(frame.result);
    }
  }
  child.stdout.on("data", readFrames);
  child.stderr.on("data", chunk => { stderr = (stderr + chunk.toString("utf8")).slice(-64 * 1024); });
  child.stdin.on("error", error => fail(new CodeModeError("server_io_failed", error.message, { stderr })));
  child.on("error", error => fail(new CodeModeError("spawn_failed", error.message, { stderr })));
  child.once("close", (code, signal) => { childClosed = true; resolveStopped({ code, signal }); if (!closed) fail(new CodeModeError("server_exited", "code-mode server exited", { code, signal, stderr })); });

  async function invoke(operation, input, callOptions = {}) {
    requireInput(input);
    if (!callOptions || typeof callOptions !== "object" || Array.isArray(callOptions)) throw new CodeModeError("invalid_options", "call options must be an object");
    if (callOptions.signal !== undefined && (typeof callOptions.signal?.addEventListener !== "function" || typeof callOptions.signal?.removeEventListener !== "function")) throw new CodeModeError("invalid_options", "signal must be an AbortSignal");
    if (callOptions.sessionId !== undefined && (typeof callOptions.sessionId !== "string" || !callOptions.sessionId)) throw new CodeModeError("invalid_options", "sessionId must be a non-empty string");
    if (!SELECTED.includes(operation)) throw new CodeModeError("operation_not_generated", "operation was not generated: " + operation);
    const callTimeout = callOptions.timeoutMs ?? timeoutMs;
    if (!Number.isInteger(callTimeout) || callTimeout < 1 || callTimeout > 300_000) throw new CodeModeError("invalid_options", "timeoutMs is outside its allowed range");
    if (callOptions.signal?.aborted) throw new CodeModeError("cancelled", "call was cancelled before execution");
    let encoded, snapshot;
    try { encoded = JSON.stringify(input); snapshot = JSON.parse(encoded); } catch { throw new CodeModeError("invalid_input", "input must be JSON serializable"); }
    requireInput(snapshot);
    if (Buffer.byteLength(encoded) > outputLimit) throw new CodeModeError("input_limit_exceeded", "input exceeds outputLimitBytes", { mayHaveExecuted: false });
    if (closed || fatalError) throw fatalError || new CodeModeError("client_closed", "client is closed");
    if (queue.length >= MAX_QUEUED_CALLS) throw new CodeModeError("queue_full", "client call queue is full", { mayHaveExecuted: false });
    const expires = Date.now() + callTimeout;
    return new Promise((resolve, reject) => {
      const abandon = (code, message) => {
        const index = queue.indexOf(job);
        if (index < 0) return;
        queue.splice(index, 1); job.cleanup(); reject(new CodeModeError(code, message, { mayHaveExecuted: false }));
      };
      const aborted = () => abandon("cancelled", "call was cancelled while queued");
      const timer = setTimeout(() => abandon("deadline_exceeded", "call deadline exceeded while queued"), callTimeout);
      const job = { resolve, reject, expires,
        cleanup: () => { clearTimeout(timer); callOptions.signal?.removeEventListener("abort", aborted); },
        run: remaining => execute(operation, snapshot, callOptions, remaining, acknowledged => { job.acknowledged = acknowledged; })
      };
      callOptions.signal?.addEventListener("abort", aborted, { once: true });
      queue.push(job);
      void drain();
    });
  }

  async function drain() {
    if (draining) return;
    draining = true;
    try {
      await startup;
      while (queue.length && active < concurrency) {
        const job = queue.shift(); job.cleanup();
        if (closed || fatalError) { job.reject(fatalError || new CodeModeError("client_closed", "client is closed")); continue; }
        const remaining = job.expires - Date.now();
        if (remaining <= 0) { job.reject(new CodeModeError("deadline_exceeded", "call deadline exceeded while queued", { mayHaveExecuted: false })); continue; }
        active++;
        void (async () => {
          try { job.resolve(await job.run(remaining)); } catch (error) { job.reject(error); }
          finally { await job.acknowledged; active--; void drain(); }
        })();
      }
    } catch (error) { fail(error); }
    finally { draining = false; }
  }

  async function execute(operation, snapshot, callOptions, callTimeout, onAcknowledged) {
    if (callOptions.signal?.aborted) throw new CodeModeError("cancelled", "call was cancelled before execution", { mayHaveExecuted: false });
    const id = nextID;
    const abort = () => {
      const entry = pending.get(String(id)); if (!entry) return;
      try { send({ method: "$/cancelRequest", params: { id } }); } catch {}
      clearTimeout(entry.timer); clearTimeout(entry.grace);
      entry.grace = setTimeout(() => { if (pending.has(String(id))) entry.reject(new CodeModeError("cancelled", "call cancellation was not acknowledged", { mayHaveExecuted: true })); }, CLOSE_GRACE_MS);
    };
    callOptions.signal?.addEventListener("abort", abort, { once: true });
    try { return await request("call", { operation, input: snapshot, timeoutMs: callTimeout, sessionId: callOptions.sessionId }, callTimeout, onAcknowledged); }
    finally { callOptions.signal?.removeEventListener("abort", abort); }
  }

  function waitForStopped(timeout) {
    if (childClosed) return Promise.resolve(true);
    return new Promise(resolve => { const timer = setTimeout(() => resolve(false), timeout); stopped.then(() => { clearTimeout(timer); resolve(true); }); });
  }
  const client = { close: () => {
    if (closePromise) return closePromise;
    closed = true;
    closePromise = (async () => {
    const shutdownID = nextID++;
    try { child.stdin.write(JSON.stringify({ jsonrpc: "2.0", id: shutdownID, method: "shutdown" }) + "\n"); } catch {}
    if (!await waitForStopped(CLOSE_GRACE_MS)) { child.kill("SIGTERM"); if (!await waitForStopped(200)) { child.kill("SIGKILL"); if (!await waitForStopped(200)) { const error = new CodeModeError("shutdown_timeout", "code-mode server did not exit after shutdown", { stderr }); fail(error); throw error; } } }
    fail(new CodeModeError("client_closed", "client closed"));
    })();
    return closePromise;
  }};
  for (const operation of SELECTED) client[operation.replace(/_([a-z])/g, (_, letter) => letter.toUpperCase())] = (input, callOptions) => invoke(operation, input, callOptions);
  return client;
}
`
