import {link, mkdir, readFile, rm, unlink, writeFile} from 'node:fs/promises';
import path from 'node:path';
import {randomUUID} from 'node:crypto';

const locked = () => new Error('ao_refresh_locked: another refresh or index owns the bindings lock');
function running(pid) {
  try { process.kill(pid, 0); return true; }
  catch (error) { if (error.code === 'ESRCH') return false; throw error; }
}

export async function acquire(directory) {
  const token = randomUUID();
  const stage = path.join(directory, `refresh.${token}.tmp`);
  const lock = path.join(directory, 'refresh.lock');
  await mkdir(stage);
  try {
    const candidate = path.join(stage, 'owner.json');
    await writeFile(candidate, JSON.stringify({pid: process.pid, token}));
    try { await link(candidate, lock); }
    catch (error) {
      if (error.code !== 'EEXIST') throw error;
      // Only the elected, serial lifecycle worker may reclaim. Ordinary callers
      // can acquire a released lock but cannot race each other to remove one.
      if (process.env.RZM_MODULE_LIFECYCLE !== '1') throw locked();
      let owner;
      try { owner = JSON.parse(await readFile(lock, 'utf8')); }
      catch { throw locked(); }
      if (!Number.isInteger(owner.pid) || owner.pid <= 0 || !/^[a-f0-9-]{36}$/.test(owner.token) || running(owner.pid)) throw locked();
      await unlink(lock);
      await rm(path.join(directory, `refresh.${owner.token}.tmp`), {recursive: true, force: true});
      try { await link(candidate, lock); }
      catch (error) { if (error.code === 'EEXIST') throw locked(); throw error; }
    }
    return {
      directory: stage,
      async release() {
        try {
          const owner = JSON.parse(await readFile(lock, 'utf8'));
          if (owner.token === token) await unlink(lock);
        } finally { await rm(stage, {recursive: true, force: true}); }
      },
    };
  } catch (error) {
    await rm(stage, {recursive: true, force: true});
    throw error;
  }
}
