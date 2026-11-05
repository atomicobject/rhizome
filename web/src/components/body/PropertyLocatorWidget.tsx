import { type KeyboardEvent, useCallback, useEffect, useRef, useState } from "react";

function isValidLocator(value: string): boolean {
  return /^[A-Za-z0-9_-]+$/.test(value.trim());
}

export function LocatorWidget({
  value,
  editing,
  dirty,
  present,
  onStage,
  ariaLabel,
}: {
  value: string;
  editing: boolean;
  dirty: boolean;
  present: boolean;
  onStage: (value: string) => void;
  ariaLabel: string;
}) {
  const [draft, setDraft] = useState(value);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const valid = isValidLocator(draft);

  useEffect(() => {
    if (document.activeElement !== inputRef.current) {
      setDraft(value);
    }
  }, [value]);

  const commit = useCallback(() => {
    const next = draft.trim();

    if (next === value || !isValidLocator(next)) return;
    onStage(next);
  }, [draft, onStage, value]);

  const handleKeyDown = useCallback(
    (event: KeyboardEvent<HTMLInputElement>) => {
      if (event.key === "Enter") {
        event.preventDefault();
        commit();
        inputRef.current?.blur();
      } else if (event.key === "Escape") {
        event.preventDefault();
        setDraft(value);
        inputRef.current?.blur();
      }
    },
    [commit, value],
  );

  if (!editing) {
    if (!present || value.trim() === "") {
      return <span className="widget-text widget-text--empty">Empty</span>;
    }

    return <span className={`widget-text${dirty ? " is-dirty" : ""}`}>^{value}</span>;
  }

  return (
    <span className="ontology-properties__locator-edit">
      <span aria-hidden="true">^</span>
      <input
        ref={inputRef}
        type="text"
        className={`widget-text widget-text--input${dirty ? " is-dirty" : ""}`}
        value={draft}
        aria-label={ariaLabel}
        aria-invalid={!valid}
        pattern="[A-Za-z0-9_-]+"
        onChange={(event) => setDraft(event.target.value.replace(/^\^+/, ""))}
        onBlur={commit}
        onKeyDown={handleKeyDown}
      />
    </span>
  );
}
