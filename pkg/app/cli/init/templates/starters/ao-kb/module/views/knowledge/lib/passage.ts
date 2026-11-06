/** On-demand browser IO for AO citations. Supply SourceText metadata and the
 * assertion's provenanceLocator field to readPassage. Source body should
 * come from noteBody(raw source note), not unprocessed GraphQL content (which may include frontmatter).
 * No raw evidence is indexed. No kit dependency; readText can be injected in tests.
 */
import { resolvePassage, safeLocalPath, selectLocator, HASH_STATUS } from './locators.ts';
import type { SourceText, PassageResult } from './locators.ts';
export type ReadText = (path: string) => Promise<string | undefined>;
/** Missing files return undefined; refusals and read failures throw, never enable fallback. */
export async function readVaultText(path: string): Promise<string | undefined> {
  if (!safeLocalPath(path)) return undefined;
  const response = await fetch(`/api/v1/files/view?path=${encodeURIComponent(path)}`);
  const data: unknown = await response.json();
  if (response.ok && data && typeof data === 'object' && 'content' in data && typeof data.content === 'string') return data.content;
  if (data && typeof data === 'object' && 'error' in data && typeof data.error === 'string' && /no such file or directory/i.test(data.error)) return undefined;
  if (path !== '.kb/index/kb-weights.json' && !/^sources\/_raw\/.+\.(md|txt)$/i.test(path)) throw new Error('Source text could not be read');
  const action = await fetch('/api/v1/actions/ao-kb/read-evidence', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path }) });
  const envelope: unknown = await action.json();
  if (action.ok && envelope && typeof envelope === 'object') {
    if ('status' in envelope && envelope.status === 'ok' && 'result' in envelope) {
      const value = envelope.result;
      if (value && typeof value === 'object' && 'content' in value && typeof value.content === 'string') return value.content;
    }
    if ('reason' in envelope && typeof envelope.reason === 'string' && /ENOENT:.*no such file or directory/.test(envelope.reason)) return undefined;
  }
  throw new Error('Source text could not be read');
}
export async function readPassage(source: SourceText | undefined, locator: string | undefined, sourceRef?: string, readText: ReadText = readVaultText): Promise<PassageResult> {
  const files = new Map<string, string>();
  if (source && locator && !sourceRef?.startsWith('view:') && !sourceRef?.startsWith('review:')) {
    const selection = selectLocator(source.paths, locator);
    if (selection.status !== 'selected') return resolvePassage(source, locator, files, sourceRef);
    const candidates = selection.entry === undefined ? source.paths : [selection.entry];
    for (const path of candidates) {
      if (!safeLocalPath(path) || !/\.(md|txt)$/i.test(path)) continue;
      try {
        const text = await readText(path);
        if (text !== undefined) { files.set(path, text); break; }
      } catch { return { status: 'unresolved', reason: 'not a local text file', hashStatus: HASH_STATUS }; }
    }
  }
  return resolvePassage(source, locator, files, sourceRef);
}
