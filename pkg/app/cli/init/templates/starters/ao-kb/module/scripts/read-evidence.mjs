// Read-only action: stdin {path}, stdout {content}. The runner sets cwd to the
// vault root. Only AO archived text and the fixed saved-analysis file are allowed.
import { readFile, realpath } from 'node:fs/promises';
import { relative, resolve, sep } from 'node:path';
import { pathToFileURL } from 'node:url';
export async function readEvidence(root, path) {
  if (typeof path !== 'string' || !path || /^(?:[\\/]|[a-z][a-z\d+.-]*:)/i.test(path) || path.split(/[\\/]/).includes('..') || path.includes('\0')) throw new Error('Invalid local path');
  const raw = /^sources\/_raw\/.+\.(md|txt)$/i.test(path);
  if (path !== '.kb/index/kb-weights.json' && !raw) throw new Error('Path is outside AO evidence');
  const base = await realpath(root);
  const target = await realpath(resolve(base, path));
  const rel = relative(base, target);
  if (rel === '..' || rel.startsWith(`..${sep}`) || resolve(base, rel) !== target) throw new Error('Path is outside vault root');
  // A symlink may not turn an allowed name into a read of another vault file.
  if (rel.split(sep).join('/') !== path) throw new Error('Evidence symlinks are not supported');
  return { content: await readFile(target, 'utf8') };
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    let input = ''; for await (const chunk of process.stdin) input += chunk;
    const { path } = JSON.parse(input);
    console.log(JSON.stringify(await readEvidence(process.cwd(), path)));
  } catch (error) {
    console.error(error instanceof Error ? error.message : 'Evidence read failed');
    process.exitCode = 2;
  }
}
