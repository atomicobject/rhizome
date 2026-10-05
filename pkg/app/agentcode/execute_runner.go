package agentcode

const executeRunner = `import { format } from "node:util";

const diagnostics = [];
let diagnosticBytes = 0;
const diagnosticLimit = 65536;
const originalWrite = process.stdout.write.bind(process.stdout);
function addDiagnostic(level, values) {
  let text;
  try { text = level + ": " + format(...values); } catch { text = level + ": [unprintable diagnostic]"; }
  const remaining = diagnosticLimit - diagnosticBytes;
  if (remaining <= 0) return;
  if (Buffer.byteLength(text) > remaining) text = Buffer.from(text).subarray(0, remaining).toString("utf8");
  diagnostics.push(text); diagnosticBytes += Buffer.byteLength(text);
}
Object.defineProperty(globalThis, "console", { value: Object.freeze({
  log: (...values) => addDiagnostic("log", values), warn: (...values) => addDiagnostic("warn", values),
  error: (...values) => addDiagnostic("error", values), info: (...values) => addDiagnostic("info", values),
  debug: (...values) => addDiagnostic("debug", values),
}), writable: false, configurable: false });
process.stdout.write = (value, encoding, callback) => {
  addDiagnostic("stdout", [Buffer.isBuffer(value) ? value.toString("utf8") : value]);
  const done = typeof encoding === "function" ? encoding : callback;
  if (typeof done === "function") queueMicrotask(done);
  return true;
};

function errorRecord(error, fallback) {
  if (error && typeof error === "object" && typeof error.code === "string") {
    return { code: error.code, message: String(error.message || fallback), details: error.details ?? {} };
  }
  return { code: fallback, message: String(error?.message || error || fallback), details: {} };
}
let unhandledError;
process.on("unhandledRejection", error => { unhandledError ??= errorRecord(error, "unhandled_rejection"); });
async function emit(value) {
  let output;
  try { output = JSON.stringify({ ...value, diagnostics }) + "\n"; }
  catch { output = '{"ok":false,"result":null,"error":{"code":"execution_result_encoding_failed","message":"could not encode execution result"}}\n'; }
  await new Promise((resolve, reject) => originalWrite(output, error => error ? reject(error) : resolve()));
  process.exit(0);
}
let client, sdk, envelope;
try {
  const input = [];
  for await (const chunk of process.stdin) input.push(chunk);
  const config = JSON.parse(Buffer.concat(input).toString("utf8"));
  const moduleURL = "data:text/javascript;base64," + Buffer.from(config.module, "utf8").toString("base64");
  sdk = await import(moduleURL);
  const pendingCalls = new Set(), unobservedFailures = new Map();
  let nextCallOrder = 0;
  let acceptingCalls = true, lateCallError;
  const rzm = {};
  for (const operation of config.operations) {
    const method = operation.replace(/_([a-z])/g, (_, letter) => letter.toUpperCase());
    rzm[method] = (...args) => {
      if (!acceptingCalls) {
        const error = new sdk.CodeModeError("execution_finished", "await Rhizome operations before returning from the script", { mayHaveExecuted: false });
        lateCallError ??= errorRecord(error, "execution_finished");
        return Promise.reject(error);
      }
      const activeClient = client ??= sdk.createClient({ executablePath: config.executablePath, vaultPath: config.vaultPath, sessionId: config.sessionId, readWrite: config.readWrite });
      const promise = activeClient[method](...args);
      const record = { order: nextCallOrder++, observed: false };
      const observe = () => { record.observed = true; unobservedFailures.delete(record.order); };
      const tracked = Object.freeze({
        then: (...values) => { observe(); return promise.then(...values); },
        catch: (...values) => { observe(); return promise.catch(...values); },
        finally: (...values) => { observe(); return promise.finally(...values); },
      });
      pendingCalls.add(record);
      record.wait = promise.then(outcome => {
        pendingCalls.delete(record);
        if (!record.observed && outcome && outcome.ok === false) {
          unobservedFailures.set(record.order, { code: "unawaited_operation_failed", message: "an unawaited Rhizome operation returned a failed outcome", details: { outcome, mayHaveExecuted: true } });
        }
      }, error => {
        pendingCalls.delete(record);
        if (!record.observed) {
          unobservedFailures.set(record.order, { code: "unawaited_operation_failed", message: "an unawaited Rhizome operation failed", details: { cause: errorRecord(error, "operation_failed"), mayHaveExecuted: true } });
        }
      });
      return tracked;
    };
  }
  Object.freeze(rzm);
  const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
  let value, executionError;
  try { value = await new AsyncFunction("rzm", "input", config.code)(rzm, config.input ?? null); }
  catch (error) { executionError = errorRecord(error, "script_failed"); }
  finally { acceptingCalls = false; }
  await Promise.all(Array.from(pendingCalls, record => record.wait));
  executionError ??= lateCallError;
  if (!executionError) {
    let firstFailureOrder = Infinity;
    for (const [order, failure] of unobservedFailures) {
      if (order < firstFailureOrder) { firstFailureOrder = order; executionError = failure; }
    }
  }
  if (executionError) envelope = { ok: false, result: null, error: executionError };
  else {
    let encoded;
    try { encoded = value === undefined ? "null" : JSON.stringify(value); }
    catch (error) { envelope = { ok: false, result: null, error: errorRecord(error, "result_not_json_serializable") }; encoded = null; }
    if (encoded !== null) {
      if (encoded === undefined) envelope = { ok: false, result: null, error: { code: "result_not_json_serializable", message: "script result cannot be represented as JSON", details: {} } };
      else if (Buffer.byteLength(encoded) > 1048576) envelope = { ok: false, result: null, error: { code: "result_limit_exceeded", message: "script result exceeds 1048576 bytes", details: {} } };
      else envelope = { ok: true, result: JSON.parse(encoded), error: null };
    }
  }
} catch (error) {
  envelope = { ok: false, result: null, error: errorRecord(error, "execution_setup_failed") };
}
if (client) {
  try { await client.close(); }
  catch (error) {
    addDiagnostic("close", [error?.message || error]);
    if (!envelope || envelope.ok) envelope = { ok: false, result: null, error: errorRecord(error, "client_cleanup_failed") };
    else if (envelope.error) {
      const details = envelope.error.details;
      envelope.error.details = details && typeof details === "object" && !Array.isArray(details)
        ? { ...details, cleanup: errorRecord(error, "client_cleanup_failed") }
        : { original: details, cleanup: errorRecord(error, "client_cleanup_failed") };
    }
  }
}
await new Promise(resolve => setImmediate(resolve));
if (unhandledError && envelope?.ok) envelope = { ok: false, result: null, error: unhandledError };
await emit(envelope || { ok: false, result: null, error: { code: "execution_setup_failed", message: "execution did not produce a result", details: {} } });`
