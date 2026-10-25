import { useEffect, useRef, useState } from "react";

import { useDesktopCommands } from "./desktopHost";

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

/** Elements whose keys belong to the reader's text, even when focus sits in a child. */
const EDITABLE =
  'input, textarea, select, [contenteditable]:not([contenteditable="false"]), [role="textbox"], [role="searchbox"], [role="combobox"]';

function typingIn(target: EventTarget | null) {
  return (
    target instanceof Element &&
    ((target instanceof HTMLElement && target.isContentEditable) ||
      target.closest(EDITABLE) !== null)
  );
}

/** WCAG 2.1.4: a reader can turn off the one global single-character shortcut. */
const SINGLE_KEY_STORAGE = "rhizome:shortcuts:single-key";

function readSingleKey() {
  try {
    return window.localStorage.getItem(SINGLE_KEY_STORAGE) !== "off";
  } catch {
    return true;
  }
}

function writeSingleKey(enabled: boolean) {
  try {
    if (enabled) window.localStorage.removeItem(SINGLE_KEY_STORAGE);
    else window.localStorage.setItem(SINGLE_KEY_STORAGE, "off");
  } catch {
    // Browser storage is optional; the choice lasts until reload.
  }
}

/** A `?` key and header button open a modal list of the app's shortcuts.
 * Inside Rhizome Desktop the button lives in the native toolbar instead. */
export function KeyboardShortcuts({ trigger = true }: { trigger?: boolean }) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const [singleKey, setSingleKey] = useState(readSingleKey);

  useDesktopCommands((command) => {
    if (command.command === "shortcuts") dialogRef.current?.showModal();
  });

  useEffect(() => {
    if (!singleKey) return;

    const onKeyDown = (event: KeyboardEvent) => {
      if (
        event.key !== "?" ||
        event.metaKey ||
        event.ctrlKey ||
        event.altKey ||
        event.defaultPrevented ||
        typingIn(event.target)
      )
        return;
      event.preventDefault();
      dialogRef.current?.showModal();
    };

    window.addEventListener("keydown", onKeyDown);

    return () => window.removeEventListener("keydown", onKeyDown);
  }, [singleKey]);

  return (
    <>
      {trigger && (
        <button
          type="button"
          className="app-shell__shortcuts-button"
          aria-label="Keyboard shortcuts"
          aria-keyshortcuts={singleKey ? "Shift+?" : undefined}
          title={singleKey ? "Keyboard shortcuts (?)" : "Keyboard shortcuts"}
          onClick={() => dialogRef.current?.showModal()}
        >
          ?
        </button>
      )}
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
        <label className="keyboard-shortcuts__setting">
          <input
            type="checkbox"
            checked={singleKey}
            onChange={(event) => {
              setSingleKey(event.target.checked);
              writeSingleKey(event.target.checked);
            }}
          />
          Open this list with the ? key
        </label>
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
