export const mermaid = {
  /** Escape a non-CSV Mermaid label. Add quotes where the diagram syntax requires them. */
  text(value: string): string {
    return value.replace(/[&"`#<>:;,[\](){}|]/g, (character) => `#${character.charCodeAt(0)};`);
  },

  /** Quote a sankey-beta CSV field, including embedded quotes. */
  csv(value: string): string {
    return `"${value.replaceAll('"', '""')}"`;
  },

  /** Derive a syntax-safe id using FNV-1a over the key's UTF-8 bytes. */
  id(key: string): string {
    let hash = 0x811c9dc5;

    for (const byte of new TextEncoder().encode(key)) {
      hash = Math.imul(hash ^ byte, 0x01000193);
    }

    return `n${(hash >>> 0).toString(36)}`;
  },
};
