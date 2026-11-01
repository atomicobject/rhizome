// Local state under .rhizome/compounding/: owner-only, written atomically
// (temp file plus rename) while one lock is held. Model results are
// content-addressed in cache/; attempts go to a separate append-only ledger,
// so a failure is never mistaken for a result.

import { createHash, randomBytes } from "node:crypto";
import { appendFileSync, chmodSync, closeSync, fstatSync, mkdirSync, openSync, readFileSync, readSync, renameSync, rmSync, writeFileSync } from "node:fs";
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
  const ownerFile = join(lockDir, "owner.json");
  let held = null;
  // A dead owner's lock is removed only under a second lock, after checking
  // the owner is still the one seen dead, so a contender acting on an old
  // reading cannot remove a lock another process has just taken.
  // ponytail: a crash inside this window leaves the guard behind; the error
  // names it for the person to delete.
  const reclaim = (owner) => {
    const guard = `${lockDir}.reclaim`;
    try {
      mkdirSync(guard, { mode: 0o700 });
    } catch (err) {
      if (err.code === "EEXIST") throw new Error(`${guard} exists: another run is recovering the lock; if no run is active, delete it`);
      throw err;
    }
    try {
      if (JSON.stringify(readJson(ownerFile, null)) !== JSON.stringify(owner)) return;
      const tomb = tmpName(lockDir);
      renameSync(lockDir, tomb);
      rmSync(tomb, { recursive: true, force: true });
    } catch (err) {
      if (err.code !== "ENOENT") throw err;
    } finally {
      rmSync(guard, { recursive: true, force: true });
    }
  };
  return {
    dir,
    path: (...parts) => join(dir, ...parts),
    read: (name, fallback) => readJson(join(dir, name), fallback),
    write: (name, value) => writeJson(join(dir, name), value),

    // The lock directory appears whole, owner file included, through one
    // rename. A lock whose process died on this host is taken over.
    lock() {
      const token = randomBytes(8).toString("hex");
      const staged = tmpName(lockDir);
      mkdirSync(staged, { mode: 0o700 });
      writeFileSync(join(staged, "owner.json"), JSON.stringify({ pid: process.pid, host: hostname(), at: new Date().toISOString(), token }));
      try {
        for (let attempt = 0; attempt < 2; attempt++) {
          try {
            renameSync(staged, lockDir);
            held = token;
            return;
          } catch (err) {
            if (!["EEXIST", "ENOTEMPTY", "EPERM"].includes(err.code)) throw err;
          }
          const owner = readJson(ownerFile, null);
          if (owner && (owner.host !== hostname() || alive(owner.pid))) throw new Error(`${dir} is locked by pid ${owner.pid} on ${owner.host} since ${owner.at}`);
          reclaim(owner);
        }
        throw new Error(`could not take the lock at ${lockDir}`);
      } finally {
        rmSync(staged, { recursive: true, force: true });
      }
    },
    unlock() {
      if (held && readJson(ownerFile, null)?.token === held) rmSync(lockDir, { recursive: true, force: true });
      held = null;
    },

    cacheGet: (key) => readJson(cacheFile(key), null),
    cachePut: (key, value) => writeJson(cacheFile(key), value),
    // One short line per append; a torn last line is skipped on read, and
    // terminated first here so it cannot swallow the entry written after it.
    ledger(entry) {
      const file = join(dir, "ledger.jsonl");
      let torn = false;
      try {
        const fd = openSync(file, "r");
        const { size } = fstatSync(fd);
        const last = Buffer.alloc(1);
        if (size) readSync(fd, last, 0, 1, size - 1);
        closeSync(fd);
        torn = size > 0 && last[0] !== 0x0a;
      } catch {}
      appendFileSync(file, (torn ? "\n" : "") + JSON.stringify({ at: new Date().toISOString(), ...entry }) + "\n", { mode: 0o600 });
    },
  };
}
