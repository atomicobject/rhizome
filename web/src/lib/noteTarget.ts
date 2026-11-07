export type NoteTarget = {
  path: string;
  /** Query authored by the target document, without the leading `?`. */
  query: string | null;
  fragment: string | null;
};

export function splitNoteTarget(target: string): NoteTarget {
  const hashIndex = target.indexOf("#");
  const beforeFragment = hashIndex === -1 ? target : target.slice(0, hashIndex);
  const queryIndex = beforeFragment.indexOf("?");

  return {
    path: queryIndex === -1 ? beforeFragment : beforeFragment.slice(0, queryIndex),
    query: queryIndex === -1 ? null : beforeFragment.slice(queryIndex + 1) || null,
    fragment: hashIndex === -1 ? null : target.slice(hashIndex + 1) || null,
  };
}

export function joinNoteTarget({
  path,
  query,
  fragment,
}: {
  path: string;
  query?: string | null;
  fragment?: string | null;
}): string {
  return `${path}${query ? `?${query}` : ""}${fragment ? `#${fragment}` : ""}`;
}
