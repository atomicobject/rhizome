// @vitest-environment node

import { mkdir, mkdtemp, rm, utimes, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";

import {
  fingerprintForStats,
  loadSupervisorEnv,
  resolveDevConfig,
  waitForServeOrigin,
} from "./devSupervisorRuntime";

describe("dev supervisor", () => {
  const tempDirs: string[] = [];

  afterEach(async () => {
    await Promise.all(tempDirs.splice(0).map((dir) => rm(dir, { recursive: true, force: true })));
  });

  it("defaults to sticky random-port backend restarts", () => {
    const config = resolveDevConfig({
      RZM_DEV_REPO_ROOT: "/repo",
      RZM_DEV_BINARY: "bin/current/rzm",
    });

    expect(config.binaryPath).toBe("/repo/bin/current/rzm");
    expect(config.webDir).toBe("/repo/web");
    expect(config.vaultPath).toBe("/repo");
    expect(config.backendPort).toBeNull();
    expect(config.skipVite).toBe(false);
  });

  it("honors explicit overrides for split workflows", () => {
    const config = resolveDevConfig({
      RZM_DEV_REPO_ROOT: "/repo",
      RZM_DEV_BINARY: "bin/custom/rzm",
      RZM_DEV_WEB_DIR: "frontend",
      RZM_DEV_VAULT: "testdata/integration/python-app/vault",
      RZM_DEV_HOST: "0.0.0.0",
      RZM_DEV_PORT: "9911",
      RZM_DEV_SKIP_VITE: "1",
    });

    expect(config.binaryPath).toBe("/repo/bin/custom/rzm");
    expect(config.webDir).toBe("/repo/frontend");
    expect(config.vaultPath).toBe("/repo/testdata/integration/python-app/vault");
    expect(config.backendHost).toBe("0.0.0.0");
    expect(config.backendPort).toBe(9911);
    expect(config.skipVite).toBe(true);
  });

  it("fingerprints binary changes from mtime, size, and inode", () => {
    const stats = { mtimeMs: 10, size: 2048, ino: 7 };
    const first = fingerprintForStats(stats);

    expect(fingerprintForStats({ ...stats })).toBe(first);
    expect(fingerprintForStats({ ...stats, mtimeMs: 11 })).not.toBe(first);
    expect(fingerprintForStats({ ...stats, size: 4096 })).not.toBe(first);
    expect(fingerprintForStats({ ...stats, ino: 8 })).not.toBe(first);
  });

  it("reads the active origin from serve discovery", async () => {
    const root = await mkdtemp(path.join(os.tmpdir(), "rhizome-dev-supervisor-"));
    tempDirs.push(root);
    await mkdir(path.join(root, ".rhizome"), { recursive: true });
    await writeFile(
      path.join(root, ".rhizome", "runtime.json"),
      JSON.stringify({ HTTPURL: "http://127.0.0.1:43123" }),
    );

    await expect(waitForServeOrigin(root, 200)).resolves.toBe("http://127.0.0.1:43123");
  });

  it.each([
    ["stale", 222, 1000],
    ["wrong PID", 111, 3000],
  ])(
    "waits past a %s manifest before admitting the backend",
    async (_name, initialPID, initialMtime) => {
      const root = await mkdtemp(path.join(os.tmpdir(), "rhizome-dev-supervisor-fresh-"));
      tempDirs.push(root);
      const manifestPath = path.join(root, ".rhizome", "runtime.json");
      await mkdir(path.join(root, ".rhizome"), { recursive: true });
      await writeFile(
        manifestPath,
        JSON.stringify({ PID: initialPID, HTTPURL: "http://127.0.0.1:43123" }),
      );
      await utimes(manifestPath, initialMtime / 1000, initialMtime / 1000);

      const pending = waitForServeOrigin(root, {
        timeoutMs: 1000,
        minMtimeMs: 2000,
        expectedPID: 222,
      });

      let settled = false;
      void pending.then(() => {
        settled = true;
      });
      await new Promise((resolve) => setTimeout(resolve, 150));
      expect(settled).toBe(false);
      await writeFile(
        manifestPath,
        JSON.stringify({ PID: 222, HTTPURL: "http://127.0.0.1:43124" }),
      );
      await utimes(manifestPath, 3, 3);

      await expect(pending).resolves.toBe("http://127.0.0.1:43124");
    },
  );

  it("parses quoted .env values for spawned child processes", async () => {
    const root = await mkdtemp(path.join(os.tmpdir(), "rhizome-dev-supervisor-env-"));
    tempDirs.push(root);
    await writeFile(
      path.join(root, ".env"),
      'VOYAGE_API_KEY="quoted-key"\nOPENAI_API_KEY=plain-key\n',
    );

    expect(loadSupervisorEnv(root)).toEqual({
      OPENAI_API_KEY: "plain-key",
      VOYAGE_API_KEY: "quoted-key",
    });
  });
});
