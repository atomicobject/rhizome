import { type ReactNode, useId } from "react";

/** An info mark whose explanation shows on hover or keyboard focus. */
export function InfoTip({ label, children }: { label: string; children: ReactNode }) {
  const id = useId();

  return (
    <span className="field-editor__info">
      <button
        type="button"
        className="field-editor__info-button"
        aria-label={label}
        aria-describedby={id}
      >
        <svg viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor">
          <circle cx="8" cy="8" r="6.25" strokeWidth="1.3" />
          <path d="M8 7.25v4" strokeWidth="1.5" strokeLinecap="round" />
          <circle cx="8" cy="4.9" r="0.4" fill="currentColor" strokeWidth="0.9" />
        </svg>
      </button>
      <span id={id} role="tooltip" className="field-editor__info-tip">
        {children}
      </span>
    </span>
  );
}
