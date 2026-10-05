import { useCallback, useEffect, useId, useRef, useState } from "react";

import type { OntologyEditFieldValue } from "../../api/types";

import {
  clearPersistedEditorDraft,
  readPersistedEditorDraft,
  type OntologyEditExpected,
  writePersistedEditorDraft,
} from "../editing/draftStorage";

type Props = {
  /** Current ISO-ish value (YYYY-MM-DD or RFC3339). */
  value: string;
  /** True when the surrounding session is in edit mode. */
  editing: boolean;
  /** Native input/editing mode. Defaults to date-only for existing callers. */
  kind?: "date" | "datetime";
  /** True when this field has unsaved changes in the active edit session. */
  dirty?: boolean;
  /** Prevent an empty value from being staged. */
  required?: boolean;
  /** Vault owner for the raw draft store. */
  vaultKey?: string | null;
  /** Stable field operation target for the raw draft store. */
  operationTarget?: string;
  /** Stage a setField op with the new ISO date or RFC3339 datetime string. */
  onStage?: (next: string) => boolean | void;
  /** Optional aria-label override. */
  ariaLabel?: string;
  /** Capture the source and field witness when a draft first changes. */
  onDraft?: (
    value: OntologyEditFieldValue,
    preserveWhenOriginal?: boolean,
  ) => OntologyEditExpected | undefined;
};

const MS_PER_DAY = 1000 * 60 * 60 * 24;

/**
 * Compact date display. The on-disk literal stays the canonical value; the
 * relative phrase ("3d ago") is a tooltip, not the display, so users can copy
 * the literal with a click. Edit mode swaps in a native date input.
 */
export function DateWidget({
  value,
  editing,
  kind = "date",
  dirty = false,
  required = false,
  vaultKey = null,
  operationTarget = "",
  onStage,
  ariaLabel,
  onDraft,
}: Props) {
  const trimmed = value.trim();
  const inputValue = kind === "datetime" ? toDateTimeLocal(trimmed) : toIsoDate(trimmed);
  const relative = inputValue ? formatRelative(inputValue.slice(0, 10)) : "";

  const [draft, setDraft] = useState(
    () => readPersistedEditorDraft(vaultKey, operationTarget) ?? (inputValue || ""),
  );

  const [requiredError, setRequiredError] = useState(false);
  const [validationError, setValidationError] = useState("");
  const inputRef = useRef<HTMLInputElement | null>(null);
  const lastStaged = useRef<string | null>(null);
  const suppressBlur = useRef(false);
  const errorID = `date-widget-error-${useId()}`;

  useEffect(() => {
    if (document.activeElement !== inputRef.current) {
      setDraft(readPersistedEditorDraft(vaultKey, operationTarget) ?? (inputValue || ""));
    }
  }, [inputValue, operationTarget, vaultKey]);
  useEffect(() => {
    if (lastStaged.current === inputValue) lastStaged.current = null;
  }, [inputValue]);
  useEffect(() => {
    const discard = () => {
      clearPersistedEditorDraft(vaultKey, operationTarget);
      setDraft(inputValue || "");
      setRequiredError(false);
      setValidationError("");
    };

    window.addEventListener("rhizome:discard-editor-drafts", discard);

    return () => window.removeEventListener("rhizome:discard-editor-drafts", discard);
  }, [inputValue, operationTarget, vaultKey]);

  const commit = useCallback(() => {
    const next = draft.trim();

    if (!next) {
      if (required) setRequiredError(true);

      if (required || (next === (inputValue || "") && lastStaged.current === null)) return;
      lastStaged.current = next;

      if (onStage?.("") !== false) clearPersistedEditorDraft(vaultKey, operationTarget);

      return;
    }

    const valid = kind === "datetime" ? isValidDateTimeLocal(next) : isValidDate(next);

    if (!valid) {
      setValidationError(`Enter a valid ${kind === "datetime" ? "date and time" : "date"}.`);

      return;
    }

    if (next === (inputValue || "") && lastStaged.current === null) {
      clearPersistedEditorDraft(vaultKey, operationTarget);

      return;
    }

    setRequiredError(false);
    setValidationError("");
    lastStaged.current = next;

    if (onStage?.(kind === "datetime" ? toRFC3339(next, trimmed) : next) !== false) {
      clearPersistedEditorDraft(vaultKey, operationTarget);
    }
  }, [draft, inputValue, kind, onStage, operationTarget, required, trimmed, vaultKey]);

  if (!editing) {
    if (!trimmed) {
      return <span className="widget-date widget-date--empty">(empty)</span>;
    }

    return (
      <span
        className={`widget-date${dirty ? " is-dirty" : ""}`}
        title={relative ? `${trimmed} · ${relative}` : trimmed}
      >
        {trimmed}
      </span>
    );
  }

  const invalidValue = trimmed !== "" && inputValue === "";
  const offset = kind === "datetime" ? datetimeOffset(trimmed) || localDateTimeOffset(draft) : "";

  const error = invalidValue
    ? `Current value: ${trimmed}`
    : validationError ||
      (requiredError && !draft.trim() ? `${ariaLabel || "This field"} is required.` : "");

  return (
    <span className="widget-date-editor">
      <input
        ref={inputRef}
        type={kind === "datetime" ? "datetime-local" : "date"}
        step={kind === "datetime" ? datetimeStep(trimmed) : undefined}
        className={`widget-date widget-date--input${dirty ? " is-dirty" : ""}`}
        value={draft}
        aria-label={ariaLabel}
        aria-invalid={Boolean(error)}
        aria-describedby={error ? errorID : undefined}
        required={required}
        onChange={(event) => {
          const next = event.target.value;
          setDraft(next);

          const nextValue = next
            ? {
                kind: "scalar" as const,
                scalar: kind === "datetime" ? toRFC3339(next, trimmed) : next,
              }
            : { kind: "unset" as const };

          writePersistedEditorDraft(
            vaultKey,
            operationTarget,
            next,
            onDraft?.(nextValue, next === inputValue && lastStaged.current !== null),
          );
          setRequiredError(false);
          setValidationError("");
        }}
        onBlur={() => {
          if (suppressBlur.current) {
            suppressBlur.current = false;

            return;
          }

          commit();
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            event.preventDefault();
            suppressBlur.current = true;
            setDraft(lastStaged.current ?? inputValue ?? "");
            clearPersistedEditorDraft(vaultKey, operationTarget);
            setRequiredError(false);
            setValidationError("");
            inputRef.current?.blur();
          } else if (event.key === "Enter") {
            event.preventDefault();
            suppressBlur.current = true;
            commit();
            inputRef.current?.blur();
          }
        }}
      />
      {kind === "datetime" ? <small className="widget-date-editor__offset">{offset}</small> : null}
      {error ? (
        <small id={errorID} className="widget-date-editor__invalid" role="alert">
          {error}
        </small>
      ) : null}
    </span>
  );
}

/**
 * Coerce an arbitrary date-ish string into a YYYY-MM-DD literal so the native
 * date input accepts it. Returns "" for unrecognized values rather than
 * throwing — the caller falls back to the raw display.
 */
function toIsoDate(input: string): string {
  if (!input) return "";
  // Already in YYYY-MM-DD form (possibly with a time suffix we don't need).
  const direct = /^(\d{4}-\d{2}-\d{2})/.exec(input);

  if (direct) return isValidDate(direct[1]) ? direct[1] : "";
  const parsed = new Date(input);

  if (Number.isNaN(parsed.getTime())) return "";

  return parsed.toISOString().slice(0, 10);
}

function toDateTimeLocal(input: string): string {
  if (!input) return "";
  const match = dateTimeParts(input);

  if (match) return isValidDateTimeLocal(match.local) ? nativeDateTimeLocal(match.local) : "";
  const parsed = new Date(input);

  if (Number.isNaN(parsed.getTime())) return "";

  return parsed.toISOString().slice(0, 16);
}

function toRFC3339(input: string, previous: string): string {
  if (!input) return "";
  const authored = dateTimeParts(previous);
  const next = dateTimeParts(input);

  if (!next) return input;
  let local = next.local;

  if (!next.seconds) {
    local += authored?.seconds === "00" && authored.fraction ? `:00${authored.fraction}` : ":00";
  } else if (authored?.fraction && !next.fraction) {
    local += authored.fraction;
  } else if (
    authored?.fraction &&
    next.fraction &&
    next.fraction.length < authored.fraction.length &&
    authored.fraction.startsWith(next.fraction)
  ) {
    local = `${local.slice(0, -next.fraction.length)}${authored.fraction}`;
  }

  return `${local}${authored?.offset || localDateTimeOffset(input)}`;
}

function nativeDateTimeLocal(input: string): string {
  const match = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2})(?::(\d{2})(\.\d+)?)?$/.exec(input);

  if (!match || !match[2] || !match[3] || match[3].length <= 4) return input;

  return `${match[1]}:${match[2]}${match[3].slice(0, 4)}`;
}

function datetimeOffset(input: string): string {
  return dateTimeParts(input)?.offset || "";
}

function localDateTimeOffset(input: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(input);

  if (!match) return "local";

  const instant = new Date(
    Number(match[1]),
    Number(match[2]) - 1,
    Number(match[3]),
    Number(match[4]),
    Number(match[5]),
  );

  const minutes = -instant.getTimezoneOffset();
  const sign = minutes >= 0 ? "+" : "-";
  const absolute = Math.abs(minutes);

  return `${sign}${String(Math.floor(absolute / 60)).padStart(2, "0")}:${String(absolute % 60).padStart(2, "0")}`;
}

function datetimeStep(input: string): string {
  const fraction = dateTimeParts(input)?.fraction;
  const seconds = dateTimeParts(input)?.seconds;

  if (!seconds) return "60";

  if (!fraction) return "1";

  return String(10 ** -(fraction.length - 1));
}

function dateTimeParts(input: string): {
  local: string;
  seconds: string;
  fraction: string;
  offset: string;
} | null {
  const match = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2})(?::(\d{2})(\.\d+)?)?(Z|[+-]\d{2}:\d{2})?$/.exec(
    input.trim(),
  );

  if (!match) return null;

  return {
    local: `${match[1]}${match[2] ? `:${match[2]}${match[3] || ""}` : ""}`,
    seconds: match[2] || "",
    fraction: match[3] || "",
    offset: match[4] || "",
  };
}

function isValidDate(input: string): boolean {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(input);

  if (!match) return false;
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const date = new Date(Date.UTC(year, month - 1, day));

  return (
    date.getUTCFullYear() === year && date.getUTCMonth() === month - 1 && date.getUTCDate() === day
  );
}

function isValidDateTimeLocal(input: string): boolean {
  const match = /^(\d{4}-\d{2}-\d{2})T(\d{2}):(\d{2})(?::(\d{2})(\.\d+)?)?$/.exec(input);

  if (!match || !isValidDate(match[1])) return false;
  const hour = Number(match[2]);
  const minute = Number(match[3]);
  const second = match[4] ? Number(match[4]) : 0;

  return hour <= 23 && minute <= 59 && second <= 59 && (!match[5] || /^\.\d+$/.test(match[5]));
}

/**
 * Render a human-readable relative phrase for tooltip use. Not localized;
 * Intl.RelativeTimeFormat is overkill for "3d ago" and we want the phrasing
 * to stay terse for hover.
 */
function formatRelative(iso: string): string {
  const date = new Date(`${iso}T00:00:00Z`);

  if (Number.isNaN(date.getTime())) return "";
  const now = Date.now();
  const diffDays = Math.round((date.getTime() - now) / MS_PER_DAY);

  if (diffDays === 0) return "today";

  if (diffDays === -1) return "yesterday";

  if (diffDays === 1) return "tomorrow";

  if (diffDays < -1 && diffDays > -30) return `${-diffDays}d ago`;

  if (diffDays > 1 && diffDays < 30) return `in ${diffDays}d`;
  const diffMonths = Math.round(diffDays / 30);

  if (diffMonths < 0) return `${-diffMonths}mo ago`;

  if (diffMonths > 0) return `in ${diffMonths}mo`;

  return "";
}
