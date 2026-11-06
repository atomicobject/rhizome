/** Pure AO e9dce951 locator rules. resolvePassage consumes already-read source files;
 * missing map entries mean unavailable files. No IO or hash verification occurs here.
 * splitLines matches all Python str.splitlines separators, including CRLF and Unicode.
 * Body fallback line numbers are relative to the trimmed body, as in AO.
 */
export const HASH_STATUS = 'Passage hash not verified by Rhizome';
export type SourceText = { path: string; paths: string[]; body: string; scheme?: string };
export type PassageResult = (
  | { status: 'resolved'; path: string; startLine: number; endLine: number; lines: string[]; before: string[]; after: string[] }
  | { status: 'document-only' }
  | { status: 'unsupported'; scheme: string }
  | { status: 'malformed' }
  | { status: 'unresolved'; reason: 'unknown source' | 'selector names nothing' | 'not a local text file' | 'span not found' }
  | { status: 'source-text-missing' }
) & { hashStatus: typeof HASH_STATUS };
export function safeLocalPath(path: string): boolean {
  return !!path && !/^(?:[\\/]|[a-z][a-z\d+.-]*:)/i.test(path) && !path.split(/[\\/]/).includes('..') && !path.includes('\0');
}
export function splitLines(text: string): string[] {
  if (!text) return [];
  const lines = text.split(/\r\n|[\n\r\v\f\x1c-\x1e\x85\u2028\u2029]/);
  if (/[\n\r\v\f\x1c-\x1e\x85\u2028\u2029]$/.test(text)) lines.pop();
  return lines;
}
export function noteBody(text: string): string {
  return text.replace(/^\uFEFF/, '').replace(/\r\n?/g, '\n').replace(/^---\n(?:(.*?)\n)?---(?:\n|$)/s, '').replace(/^(?:[ \t]*\n)+/, '');
}
export function sourceReference(body: string, recorded?: string): string | undefined {
  if (recorded) return recorded;
  let inside = false;
  for (const line of body.split('\n')) {
    if (/^##\s+Sources\s*$/i.test(line.trim())) { inside = true; continue; }
    if (inside && line.startsWith('## ')) break;
    const match = inside && /\[\[([^\]|#^\n]+)/.exec(line);
    if (match) return linkReference(match[1]);
  }
}
export function linkReference(ref: string): string {
  return ref.trim().replace(/\/+$/, '').split('/').pop()?.replace(/\.md$/, '') ?? '';
}
const linePattern = /^L(\d+)(?:-L(\d+))?$/;
const timestampPattern = /^\d{1,2}:\d{2}(?::\d{2})?(?:-\d{1,2}:\d{2}(?::\d{2})?)?$/;
export type LocatorSelection = { status: 'selected'; span: string; entry: string | undefined } | { status: 'malformed' } | { status: 'unresolved'; reason: 'selector names nothing' };
/** Parse the selector without IO; undefined entry means AO's default-file rule. */
export function selectLocator(paths: string[], locator: string): LocatorSelection {
  let span = locator, entry: string | undefined;
  if (locator.includes('::')) {
    const at = locator.indexOf('::'), prefix = locator.slice(0, at).trim();
    const basenameMatches = paths.filter(p => p.split('/').pop() === prefix);
    if (!prefix || /^[-+]\d+$/.test(prefix) || /^0+$/.test(prefix)) return { status: 'malformed' };
    if (/^\d+$/.test(prefix) || paths.includes(prefix) || basenameMatches.length) {
      span = locator.slice(at + 2);
      if (!span.trim()) return { status: 'malformed' };
      entry = /^\d+$/.test(prefix) ? paths[Number(prefix) - 1] : paths.includes(prefix) ? prefix : basenameMatches.length === 1 ? basenameMatches[0] : undefined;
      if (!entry) return { status: 'unresolved', reason: 'selector names nothing' };
    }
  }
  return { status: 'selected', span, entry };
}
export function resolvePassage(source: SourceText | undefined, locator: string | undefined, files: ReadonlyMap<string, string>, sourceRef?: string): PassageResult {
  const result = <T extends object>(value: T): T & { hashStatus: typeof HASH_STATUS } => ({ ...value, hashStatus: HASH_STATUS });
  if (sourceRef?.startsWith('view:') || sourceRef?.startsWith('review:')) return result({ status: 'unsupported', scheme: sourceRef.split(':')[0] });
  if (!source) return result({ status: 'unresolved', reason: 'unknown source' });
  if (locator === undefined || locator === '') return result({ status: 'document-only' });
  const selected = selectLocator(source.paths, locator);
  if (selected.status !== 'selected') return result(selected);
  let { span, entry } = selected;
  if (entry && (!safeLocalPath(entry) || !/\.(md|txt)$/i.test(entry) || !files.has(entry))) return result({ status: 'unresolved', reason: 'not a local text file' });
  entry ??= source.paths.find(p => safeLocalPath(p) && /\.(md|txt)$/i.test(p) && files.has(p));
  const text = entry === undefined ? source.body.replace(/^(?:[ \t]*\n)+/, '') : files.get(entry);
  if (text === undefined || (!entry && !text.trim())) return result({ status: 'source-text-missing' });
  span = span.trim();
  if (!span) return result({ status: 'malformed' });
  const scheme = ['line', 'section', 'timestamp'].includes(source.scheme ?? '') ? source.scheme : linePattern.test(span) ? 'line' : timestampPattern.test(span) ? 'timestamp' : 'section';
  if (scheme === 'timestamp') return result({ status: 'unsupported', scheme });
  const lines = splitLines(text.replace(/^\uFEFF/, ''));
  let start = 0, end = 0;
  if (scheme === 'line') {
    const match = linePattern.exec(span);
    if (!match) return result({ status: 'malformed' });
    start = Number(match[1]) - 1; end = Number(match[2] ?? match[1]);
    if (start < 0 || end <= start || end > lines.length) return result({ status: 'unresolved', reason: 'span not found' });
  } else {
    const segments = span.split('>').map(s => s.trim());
    if (segments.some(s => !s)) return result({ status: 'malformed' });
    const headings: { index: number; level: number; text: string }[] = [];
    let fenced = false;
    lines.forEach((line, index) => {
      if (/^\s*(```|~~~)/.test(line)) { fenced = !fenced; return; }
      const match = !fenced && /^(#{1,6})[ \t]+(.*?)[ \t]*#*[ \t]*$/.exec(line);
      if (match) headings.push({ index, level: match[1].length, text: match[2].trim().normalize('NFC') });
    });
    let lo = 0, hi = lines.length, level = 0;
    for (const segment of segments) {
      const hit = headings.find(h => lo <= h.index && h.index < hi && h.level > level && h.text === segment.normalize('NFC'));
      if (!hit) return result({ status: 'unresolved', reason: 'span not found' });
      start = hit.index; level = hit.level;
      end = headings.find(h => h.index > start && h.level <= level)?.index ?? lines.length;
      lo = start + 1; hi = Math.min(end, hi); end = hi;
    }
  }
  return result({ status: 'resolved', path: entry ?? source.path, startLine: start + 1, endLine: end, lines: lines.slice(start, end), before: lines.slice(Math.max(0, start - 3), start), after: lines.slice(end, end + 3) });
}
