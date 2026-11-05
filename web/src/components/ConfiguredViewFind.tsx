import { Search } from "lucide-react";
import { useEffect, useRef } from "react";

export function ConfiguredViewFind({
  active,
  value,
  onChange,
}: {
  active: boolean;
  value: string;
  onChange: (value: string) => void;
}) {
  const searchRef = useRef<HTMLInputElement | null>(null);

  // ⌘F (Ctrl+F elsewhere) finds in the view while it shows.
  useEffect(() => {
    if (!active) return;

    const onKeyDown = (event: KeyboardEvent) => {
      const search = searchRef.current;

      if (
        event.key.toLowerCase() !== "f" ||
        !(event.metaKey || event.ctrlKey) ||
        event.shiftKey ||
        event.altKey ||
        !search?.isConnected
      )
        return;
      event.preventDefault();
      search.focus();
      search.select();
    };

    window.addEventListener("keydown", onKeyDown);

    return () => window.removeEventListener("keydown", onKeyDown);
  }, [active]);

  return (
    <div className="configured-view__find-wrap">
      <button
        type="button"
        className="configured-view__tool-button configured-view__find-toggle"
        aria-label="Find in view"
        onClick={() => searchRef.current?.focus()}
      >
        <Search size={16} strokeWidth={1.5} absoluteStrokeWidth aria-hidden />
      </button>
      <label className="configured-view__find">
        <Search size={16} strokeWidth={1.5} absoluteStrokeWidth aria-hidden />
        <input
          ref={searchRef}
          className="configured-view__search"
          type="search"
          aria-label="Search view"
          aria-keyshortcuts="Meta+F Control+F"
          placeholder="Find in view"
          value={value}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Escape") event.currentTarget.blur();
          }}
        />
        {!value && <kbd aria-hidden="true">⌘F</kbd>}
      </label>
    </div>
  );
}
