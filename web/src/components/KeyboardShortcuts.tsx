import { useEffect, useRef } from "react";

const SHORTCUTS: Array<[string, Array<[string, string]>]> = [
  [
    "Anywhere",
    [
      ["⌘K", "Search this project"],
      ["F6 / ⇧F6", "Next / previous area: tabs, rail, main, context"],
      ["?", "Show these shortcuts"],
    ],
  ],
  [
    "Note tabs",
    [
      ["← →", "Switch tabs"],
      ["Home End", "First / last tab"],
      ["Delete", "Close tab"],
    ],
  ],
  [
    "Note list",
    [
      ["↑ ↓  j k", "Move"],
      ["Enter", "Open"],
      ["O  X", "Open beside"],
      ["↓ in filter", "Enter the list"],
    ],
  ],
  [
    "Editing",
    [
      ["⌘⇧E", "Start editing"],
      ["⌘S", "Save staged changes"],
      ["⌘⇧Enter", "Review changes"],
    ],
  ],
  [
    "Tables",
    [
      ["↑ ↓", "Rows"],
      ["← →", "Cells"],
      ["Enter", "Open row"],
      ["F2", "Edit cell"],
      ["Esc", "Cancel edit, back to row"],
    ],
  ],
  [
    "Boards",
    [
      ["M", "Move card to a column"],
      ["Enter", "Open card"],
    ],
  ],
  [
    "Problems",
    [
      ["↑ ↓", "Move through issues"],
      ["Alt ↑ ↓", "Previous / next issue in detail"],
      ["Esc", "Back to the issue list"],
    ],
  ],
];

function typingIn(target: EventTarget | null) {
  return (
    target instanceof HTMLElement &&
    (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))
  );
}

/** A `?` key and header button open a modal list of the app's shortcuts. */
export function KeyboardShortcuts() {
  const dialogRef = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "?" || event.metaKey || event.ctrlKey || typingIn(event.target)) return;
      event.preventDefault();
      dialogRef.current?.showModal();
    };

    window.addEventListener("keydown", onKeyDown);

    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  return (
    <>
      <button
        type="button"
        className="app-shell__shortcuts-button"
        aria-label="Keyboard shortcuts"
        aria-keyshortcuts="Shift+?"
        title="Keyboard shortcuts (?)"
        onClick={() => dialogRef.current?.showModal()}
      >
        ?
      </button>
      <dialog
        ref={dialogRef}
        className="keyboard-shortcuts"
        aria-labelledby="keyboard-shortcuts-title"
        // A click on the backdrop lands on the dialog itself.
        onClick={(event) => {
          if (event.target === event.currentTarget) event.currentTarget.close();
        }}
      >
        <header>
          <h2 id="keyboard-shortcuts-title">Keyboard shortcuts</h2>
          <button type="button" onClick={() => dialogRef.current?.close()}>
            Close
          </button>
        </header>
        <div className="keyboard-shortcuts__groups">
          {SHORTCUTS.map(([group, rows]) => (
            <section key={group}>
              <h3>{group}</h3>
              <dl>
                {rows.map(([keys, action]) => (
                  <div key={action}>
                    <dt>
                      <kbd>{keys}</kbd>
                    </dt>
                    <dd>{action}</dd>
                  </div>
                ))}
              </dl>
            </section>
          ))}
        </div>
      </dialog>
    </>
  );
}
