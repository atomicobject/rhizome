// Deep links are shared by hand as well as written by the app, so the fragment
// carrying a node ref obeys the usual URL contract: it is percent-encoded on
// write and decoded once on read. `#^story-001` is written as `#%5Estory-001`
// and a heading containing a literal `%` survives the round trip.

export function encodeURLFragment(fragment: string): string {
  const body = fragment.startsWith("#") ? fragment.slice(1) : fragment;

  if (!body) return "";

  return `#${encodeURIComponent(body)}`;
}

export function decodeURLFragment(hash: string): string {
  if (!hash) return "";

  try {
    return decodeURIComponent(hash);
  } catch {
    return hash;
  }
}
