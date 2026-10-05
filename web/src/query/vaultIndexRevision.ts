// Share the existing vault event stream with imperative pane reads. A revision
// also catches invalidation that arrives while a failed request is in flight.
let revision = 0;

const listeners = new Set<() => void>();

export function getVaultIndexRevision(): number {
  return revision;
}

export function subscribeVaultIndexRevision(listener: () => void): () => void {
  listeners.add(listener);

  return () => listeners.delete(listener);
}

export function advanceVaultIndexRevision(): void {
  revision += 1;
  listeners.forEach((listener) => listener());
}
