type DevConfig = {
  repoRoot: string;
  binaryPath: string;
  webDir: string;
  vaultPath: string;
  backendHost: string;
  backendPort: number | null;
  skipVite: boolean;
};

// @ts-expect-error runtime JS helper lacks first-class TypeScript types
import * as runtime from "../scripts/dev_supervisor.mjs";

// SAFETY: scripts/dev_supervisor.mjs `export function resolveDevConfig(env = process.env)` reads
// only RZM_DEV_* string env vars and returns exactly the DevConfig fields above; devSupervisor.test.ts
// covers binaryPath, webDir, vaultPath, backendPort, skipVite.
export const resolveDevConfig = runtime.resolveDevConfig as (
  env?: Record<string, string | undefined>,
) => DevConfig;

// SAFETY: scripts/dev_supervisor.mjs `export function fingerprintForStats(stats)` reads only
// stats.mtimeMs, stats.size and optional stats.ino, and returns a template string.
export const fingerprintForStats = runtime.fingerprintForStats as (stats: {
  mtimeMs: number;
  size: number;
  ino?: number;
}) => string;

// SAFETY: scripts/dev_supervisor.mjs `export function loadSupervisorEnv(repoRoot)` takes a path and
// returns an object built only from parsed dotenv string key/value pairs.
export const loadSupervisorEnv = runtime.loadSupervisorEnv as (
  repoRoot: string,
) => Record<string, string>;

type WaitForServeOriginOptions = {
  timeoutMs?: number;
  minMtimeMs?: number;
  expectedPID?: number | null;
};

// SAFETY: scripts/dev_supervisor.mjs `export async function waitForServeOrigin(vaultPath, options)`
// accepts either a number timeout or the { timeoutMs, minMtimeMs, expectedPID } object it destructures,
// and resolves to the discovered origin string.
export const waitForServeOrigin = runtime.waitForServeOrigin as (
  vaultPath: string,
  options?: number | WaitForServeOriginOptions,
) => Promise<string>;
