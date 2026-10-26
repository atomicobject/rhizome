import { useRef, useState } from "react";

import { type Appearance, setAppearance, setTheme, useAppearance } from "../lib/appearance";
import { THEMES } from "../lib/themes";
import { usePopoverDismiss } from "./focusManagement";

const APPEARANCES: readonly { id: Appearance; label: string }[] = [
  { id: "auto", label: "Match system" },
  { id: "light", label: "Light" },
  { id: "dark", label: "Dark" },
];

/**
 * The browser's Appearance control: light, dark, or match the system, plus
 * the chrome theme. Rhizome Desktop has its own (in the app menu and
 * Settings), so the desktop page does not render this.
 */
export function AppearanceMenu() {
  const { appearance, theme } = useAppearance();
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  usePopoverDismiss(open, rootRef, () => setOpen(false));

  return (
    <div className="appearance-menu" ref={rootRef}>
      <button
        type="button"
        className="appearance-menu__trigger"
        aria-label="Appearance"
        title="Appearance"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <circle cx="12" cy="12" r="8.5" fill="none" stroke="currentColor" strokeWidth="1.6" />
          <path d="M12 3.5v17a8.5 8.5 0 0 0 0-17z" fill="currentColor" />
        </svg>
      </button>
      {open && (
        <div className="appearance-menu__panel" role="dialog" aria-label="Appearance">
          <h2 className="appearance-menu__heading" id="appearance-mode">
            Appearance
          </h2>
          <div
            className="segmented appearance-menu__modes"
            role="group"
            aria-labelledby="appearance-mode"
          >
            {APPEARANCES.map((option) => (
              <button
                key={option.id}
                type="button"
                aria-pressed={appearance === option.id}
                onClick={() => setAppearance(option.id)}
              >
                {option.label}
              </button>
            ))}
          </div>
          <h2 className="appearance-menu__heading" id="appearance-theme">
            Theme
          </h2>
          <div className="appearance-menu__themes" role="group" aria-labelledby="appearance-theme">
            {THEMES.map((option) => (
              <button
                key={option.id}
                type="button"
                className="appearance-menu__swatch"
                aria-pressed={theme === option.id}
                onClick={() => setTheme(option.id)}
              >
                <span
                  className="appearance-menu__preview"
                  style={{ backgroundColor: option.swatch }}
                  aria-hidden="true"
                >
                  <span />
                </span>
                {option.label}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
