// Local state under .rhizome/session-memory/: owner-only, written atomically
// (temp file plus rename) while one lock is held. Model results are
// content-addressed in cache/; attempts go to a separate append-only ledger,
// so a failure is never mistaken for a result.

import { createHash, randomBytes } from "node:crypto";
import { appendFileSync, chmodSync, mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { hostname } from "node:os";
import { dirname, join } from "node:path";

export const sha = (v) => createHash("sha256").update(typeof v === "string" ? v : JSON.stringify(v)).digest("hex");

const tmpName = (file) => `${file}.${process.pid}.${randomBytes(4).toString("hex")}.tmp`;

export function writeJson(file, value) {
  mkdirSync(dirname(file), { recursive: true, mode: 0o700 });
  const tmp = tmpName(file);
  writeFileSync(tmp, JSON.stringify(value), { mode: 0o600 });
  renameSync(tmp, file);
}

export function writeText(file, text) {
  mkdirSync(dirname(file), { recursive: true, mode: 0o700 });
  const tmp = tmpName(file);
  writeFileSync(tmp, text, { mode: 0o600 });
  renameSync(tmp, file);
}

export function readJson(file, fallback) {
  try {
    return JSON.parse(readFileSync(file, "utf8"));
  } catch (err) {
    if (err.code === "ENOENT") return fallback;
    throw err;
  }
}

// JSONL readers skip a torn last line left by a killed append.
export function readJsonl(file) {
  let text;
  try {
    text = readFileSync(file, "utf8");
  } catch (err) {
    if (err.code === "ENOENT") return [];
    throw err;
  }
  const rows = [];
  for (const line of text.split("\n")) {
    if (!line) continue;
    try {
      rows.push(JSON.parse(line));
    } catch {}
  }
  return rows;
}

const alive = (pid) => {
  try {
    process.kill(pid, 0);
    return true;
  } catch (err) {
    return err.code === "EPERM";
  }
};

export function openStore(dir) {
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  chmodSync(dir, 0o700);
  const lockDir = join(dir, "lock");
  const cacheFile = (key) => join(dir, "cache", key.slice(0, 2), `${key}.json`);
  return {
    dir,
    path: (...parts) => join(dir, ...parts),
    read: (name, fallback) => readJson(join(dir, name), fallback),
    write: (name, value) => writeJson(join(dir, name), value),

    // The lock directory appears whole, owner file included, through one
    // rename. A lock whose process died on this host is taken over.
    lock() {
      const staged = tmpName(lockDir);
      mkdirSync(staged, { mode: 0o700 });
      writeFileSync(join(staged, "owner.json"), JSON.stringify({ pid: process.pid, host: hostname(), at: new Date().toISOString() }));
      for (let attempt = 0; attempt < 2; attempt++) {
        try {
          renameSync(staged, lockDir);
          return;
        } catch (err) {
          if (!["EEXIST", "ENOTEMPTY"].includes(err.code)) throw err;
        }
        const owner = readJson(join(lockDir, "owner.json"), null);
        if (owner && (owner.host !== hostname() || alive(owner.pid))) {
          rmSync(staged, { recursive: true, force: true });
          throw new Error(`${dir} is locked by pid ${owner.pid} on ${owner.host} since ${owner.at}`);
        }
        const tomb = tmpName(lockDir);
        try {
          renameSync(lockDir, tomb);
          rmSync(tomb, { recursive: true, force: true });
        } catch {}
      }
      rmSync(staged, { recursive: true, force: true });
      throw new Error(`could not take the lock at ${lockDir}`);
    },
    unlock: () => rmSync(lockDir, { recursive: true, force: true }),

    cacheGet: (key) => readJson(cacheFile(key), null),
    cachePut: (key, value) => writeJson(cacheFile(key), value),
    // One short line per append; a torn last line is skipped on read.
    ledger: (entry) => appendFileSync(join(dir, "ledger.jsonl"), JSON.stringify({ at: new Date().toISOString(), ...entry }) + "\n", { mode: 0o600 }),
  };
}
