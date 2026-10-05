import type { TreeEntry } from "../api/types";

export function TreeList({
  entries,
  expanded,
  selectedPath,
  onSelect,
  onToggle,
}: {
  entries: TreeEntry[];
  expanded: Record<string, TreeEntry[]>;
  selectedPath: string | null;
  onSelect: (entry: TreeEntry) => void;
  onToggle: (entry: TreeEntry) => void;
}) {
  return (
    <ul className="tree-list">
      {entries.map((entry) => {
        const isDir = entry.kind === "dir";
        const isExpanded = !!expanded[entry.path];
        const isSelected = selectedPath === entry.path;

        return (
          <li key={entry.path} className="tree-item">
            <button
              type="button"
              className={`tree-row ${isSelected ? "tree-row--selected" : ""}`}
              onClick={() => (isDir ? onToggle(entry) : onSelect(entry))}
            >
              {isDir ? (
                <span className={`tree-chevron ${isExpanded ? "tree-chevron--open" : ""}`}>›</span>
              ) : (
                <span className="tree-chevron tree-chevron--spacer" />
              )}
              <span className={`tree-icon ${isDir ? "tree-icon--dir" : "tree-icon--file"}`}>
                {isDir ? (isExpanded ? "📂" : "📁") : entry.kind === "note" ? "📄" : "📝"}
              </span>
              <span className="tree-name">{entry.name}</span>
            </button>
            {isExpanded && expanded[entry.path] && (
              <TreeList
                entries={expanded[entry.path]}
                expanded={expanded}
                selectedPath={selectedPath}
                onSelect={onSelect}
                onToggle={onToggle}
              />
            )}
          </li>
        );
      })}
    </ul>
  );
}
