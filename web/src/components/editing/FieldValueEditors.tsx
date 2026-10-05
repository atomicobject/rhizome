import {
  type ChangeEvent,
  type KeyboardEvent,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";

import type { OntologyEditFieldValue } from "../../api/types";
import {
  clearPersistedEditorDraft,
  readPersistedEditorDraft,
  type OntologyEditExpected,
  writePersistedEditorDraft,
} from "./draftStorage";
import { fieldValueFromScalar } from "./RelationEditor";

type EditorProps = {
  editing: boolean;
  dirty: boolean;
  required: boolean;
  vaultKey: string | null;
  operationTarget: string;
  ariaLabel: string;
  onDraft?: (
    value: OntologyEditFieldValue,
    preserveWhenOriginal?: boolean,
  ) => OntologyEditExpected | undefined;
  onStage: (value: OntologyEditFieldValue) => boolean | void;
};

function normalizedBooleanValue(value: string): string {
  const normalized = value.trim().toLowerCase();

  return normalized === "true" || normalized === "false" ? normalized : "";
}

export function BooleanEditor({
  value,
  editing,
  dirty,
  required,
  vaultKey,
  operationTarget,
  ariaLabel,
  onDraft,
  onStage,
}: EditorProps & { value: string }) {
  const currentInvalid = value.trim() !== "" && !/^(true|false)$/i.test(value.trim());
  const errorID = `boolean-editor-error-${useId()}`;

  const [draft, setDraft] = useState(() => {
    const persisted = readPersistedEditorDraft(vaultKey, operationTarget);

    return persisted ?? normalizedBooleanValue(value);
  });

  useEffect(() => {
    if (document.activeElement === null || document.activeElement.tagName !== "SELECT") {
      setDraft(
        readPersistedEditorDraft(vaultKey, operationTarget) ?? normalizedBooleanValue(value),
      );
    }
  }, [operationTarget, value, vaultKey]);
  useEffect(() => {
    const discard = () => {
      clearPersistedEditorDraft(vaultKey, operationTarget);
      setDraft(normalizedBooleanValue(value));
    };

    window.addEventListener("rhizome:discard-editor-drafts", discard);

    return () => window.removeEventListener("rhizome:discard-editor-drafts", discard);
  }, [operationTarget, value, vaultKey]);

  if (!editing) {
    return (
      <span className={dirty ? "field-editor is-dirty" : "field-editor"}>
        {value || <span className="field-editor__empty">Empty</span>}
      </span>
    );
  }

  return (
    <span className="field-editor__control">
      <select
        className={dirty ? "field-editor__select is-dirty" : "field-editor__select"}
        value={draft}
        aria-label={ariaLabel}
        aria-invalid={currentInvalid}
        aria-describedby={currentInvalid ? errorID : undefined}
        required={required}
        onChange={(event) => {
          const next = event.target.value;
          setDraft(next);
          const nextValue = fieldValueFromScalar(next);
          const expected = onDraft?.(nextValue);

          if (next === value) clearPersistedEditorDraft(vaultKey, operationTarget);
          else writePersistedEditorDraft(vaultKey, operationTarget, next, expected);

          if (onStage(fieldValueFromScalar(next)) !== false) {
            clearPersistedEditorDraft(vaultKey, operationTarget);
          }
        }}
      >
        {!required ? <option value="">Empty</option> : null}
        <option value="true">True</option>
        <option value="false">False</option>
      </select>
      {currentInvalid ? (
        <small id={errorID} className="field-editor__reason" role="alert">
          Current value: {value}. Choose True or False.
        </small>
      ) : null}
    </span>
  );
}

export function NumberEditor({
  value,
  kind,
  editing,
  dirty,
  required,
  vaultKey,
  operationTarget,
  ariaLabel,
  onDraft,
  onStage,
}: EditorProps & { value: string; kind: "int" | "float" | "number" }) {
  const currentInvalid = value.trim() !== "" && !validNumberDraft(value, kind, false);

  const [draft, setDraft] = useState(() => {
    const persisted = readPersistedEditorDraft(vaultKey, operationTarget);

    return persisted ?? (currentInvalid ? "" : value);
  });

  const [error, setError] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const draftRef = useRef(draft);
  const valueRef = useRef(value);
  const stageRef = useRef(onStage);
  const lastStaged = useRef<string | null>(null);
  const cancelBlur = useRef(false);
  const composing = useRef(false);
  const errorID = `number-editor-error-${useId()}`;
  useEffect(() => {
    draftRef.current = draft;
    valueRef.current = value;
    stageRef.current = onStage;
  }, [draft, onStage, value]);
  useEffect(() => {
    if (document.activeElement !== inputRef.current) {
      setDraft(
        readPersistedEditorDraft(vaultKey, operationTarget) ?? (currentInvalid ? "" : value),
      );
    }
  }, [currentInvalid, operationTarget, value, vaultKey]);
  useEffect(() => {
    if (lastStaged.current === value) lastStaged.current = null;
  }, [value]);
  useEffect(() => {
    const discard = () => {
      clearPersistedEditorDraft(vaultKey, operationTarget);
      draftRef.current = currentInvalid ? "" : value;
      setDraft(draftRef.current);
      setError("");
    };

    window.addEventListener("rhizome:discard-editor-drafts", discard);

    return () => window.removeEventListener("rhizome:discard-editor-drafts", discard);
  }, [currentInvalid, operationTarget, value, vaultKey]);
  useEffect(
    () => () => {
      const next = draftRef.current;

      if (
        readPersistedEditorDraft(vaultKey, operationTarget) === null ||
        composing.current ||
        (next === valueRef.current && lastStaged.current === null) ||
        lastStaged.current === next ||
        !validNumberDraft(next, kind, required)
      ) {
        return;
      }

      if (stageRef.current(fieldValueFromScalar(next)) !== false) {
        clearPersistedEditorDraft(vaultKey, operationTarget);
      }
    },
    [kind, operationTarget, required, vaultKey],
  );

  const accept = useCallback(() => {
    if (cancelBlur.current) {
      cancelBlur.current = false;

      return;
    }

    if (draft === value && lastStaged.current === null) {
      if (required && draft.trim() === "") setError(`${ariaLabel} is required.`);

      if (currentInvalid) setError(`Current value: ${value}. Enter a valid number.`);

      return;
    }

    if (currentInvalid && draft.trim() === "") {
      setError(`Current value: ${value}. Enter a valid number.`);

      return;
    }

    if (!validNumberDraft(draft, kind, required) || !inputRef.current?.checkValidity()) {
      setError(
        required && draft.trim() === "" ? `${ariaLabel} is required.` : "Enter a valid number.",
      );

      return;
    }

    setError("");
    lastStaged.current = draft;

    if (onStage(fieldValueFromScalar(draft)) !== false) {
      clearPersistedEditorDraft(vaultKey, operationTarget);
    }
  }, [ariaLabel, currentInvalid, draft, kind, onStage, operationTarget, required, value, vaultKey]);

  if (!editing) {
    return (
      <span className={dirty ? "field-editor is-dirty" : "field-editor"}>
        {value || <span className="field-editor__empty">Empty</span>}
      </span>
    );
  }

  return (
    <span className="field-editor__control">
      <input
        ref={inputRef}
        type="number"
        step={kind === "int" ? "1" : "any"}
        value={draft}
        className={dirty ? "field-editor__number is-dirty" : "field-editor__number"}
        aria-label={ariaLabel}
        aria-invalid={Boolean(error) || currentInvalid}
        aria-describedby={error || currentInvalid ? errorID : undefined}
        required={required}
        onChange={(event) => {
          const next = event.target.value;
          draftRef.current = next;
          setDraft(next);

          const expected = onDraft?.(
            fieldValueFromScalar(next),
            next === value && lastStaged.current !== null,
          );

          if (next === value && lastStaged.current === null)
            clearPersistedEditorDraft(vaultKey, operationTarget);
          else writePersistedEditorDraft(vaultKey, operationTarget, next, expected);

          if (event.target.value.trim() !== "") setError("");
        }}
        onCompositionStart={() => (composing.current = true)}
        onCompositionEnd={() => (composing.current = false)}
        onBlur={accept}
        onKeyDown={(event) => {
          if (composing.current || event.nativeEvent.isComposing) return;

          if (event.key === "Escape") {
            event.preventDefault();
            cancelBlur.current = true;
            const next = lastStaged.current ?? value;
            draftRef.current = next;
            setDraft(next);
            clearPersistedEditorDraft(vaultKey, operationTarget);
            setError("");
            inputRef.current?.blur();
          } else if (event.key === "Enter") {
            event.preventDefault();
            accept();
            cancelBlur.current = true;
            inputRef.current?.blur();
          }
        }}
      />
      {error ? (
        <small id={errorID} className="field-editor__reason" role="alert">
          {error}
        </small>
      ) : null}
      {currentInvalid && !error ? (
        <small id={errorID} className="field-editor__reason" role="alert">
          Current value: {value}. Enter a valid number.
        </small>
      ) : null}
    </span>
  );
}

function validNumberDraft(draft: string, kind: "int" | "float" | "number", required: boolean) {
  const next = draft.trim();

  if (!next) return !required;
  const number = Number(next);

  if (!Number.isFinite(number)) return false;

  return kind !== "int" || Number.isInteger(number);
}

function splitListInput(value: string): string[] {
  return value
    .split(/\r?\n/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function parsePersistedList(value: string, fallback: string[]): string[] {
  try {
    const parsed: unknown = JSON.parse(value);

    if (Array.isArray(parsed) && parsed.every((item) => item === String(item))) return parsed;
  } catch {
    // Text list drafts are not JSON, so keep the authored values for enum controls.
  }

  return fallback;
}

export function ListEditor({
  values,
  options,
  editing,
  dirty,
  required,
  vaultKey,
  operationTarget,
  ariaLabel,
  onDraft,
  onStage,
}: EditorProps & { values: string[]; options?: string[] }) {
  const [draft, setDraft] = useState(
    () => readPersistedEditorDraft(vaultKey, operationTarget) ?? values.join("\n"),
  );

  const [selectedValues, setSelectedValues] = useState(() => {
    const persisted = readPersistedEditorDraft(vaultKey, operationTarget);

    if (!options || persisted === null) return values;

    return parsePersistedList(persisted, values);
  });

  const [error, setError] = useState("");
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const draftRef = useRef(draft);
  const valuesRef = useRef(values);
  const stageRef = useRef(onStage);
  const lastStaged = useRef<string | null>(null);
  const cancelBlur = useRef(false);
  const composing = useRef(false);
  const errorID = `list-editor-error-${useId()}`;
  useEffect(() => {
    draftRef.current = draft;
    valuesRef.current = values;
    stageRef.current = onStage;
  }, [draft, onStage, values]);
  useEffect(() => {
    if (document.activeElement !== inputRef.current) {
      setDraft(readPersistedEditorDraft(vaultKey, operationTarget) ?? values.join("\n"));
    }

    const persisted = readPersistedEditorDraft(vaultKey, operationTarget);

    if (persisted === null) setSelectedValues(values);
    else if (options) setSelectedValues(parsePersistedList(persisted, values));
  }, [operationTarget, options, values, vaultKey]);
  useEffect(() => {
    if (lastStaged.current === values.join("\n")) lastStaged.current = null;
  }, [values]);
  useEffect(() => {
    const discard = () => {
      clearPersistedEditorDraft(vaultKey, operationTarget);
      draftRef.current = values.join("\n");
      setDraft(draftRef.current);
      setSelectedValues(values);
      setError("");
    };

    window.addEventListener("rhizome:discard-editor-drafts", discard);

    return () => window.removeEventListener("rhizome:discard-editor-drafts", discard);
  }, [operationTarget, values, vaultKey]);
  useEffect(
    () => () => {
      if (
        readPersistedEditorDraft(vaultKey, operationTarget) === null ||
        composing.current ||
        lastStaged.current === draftRef.current
      )
        return;
      const next = splitListInput(draftRef.current);

      if (required && next.length === 0) return;

      if (
        JSON.stringify(next) !== JSON.stringify(valuesRef.current) ||
        lastStaged.current !== null
      ) {
        if (stageRef.current({ kind: "list", items: next }) !== false) {
          clearPersistedEditorDraft(vaultKey, operationTarget);
        }
      }
    },
    [operationTarget, required, vaultKey],
  );

  const accept = useCallback(() => {
    if (cancelBlur.current) {
      cancelBlur.current = false;

      return;
    }

    const next = splitListInput(draft);

    if (JSON.stringify(next) === JSON.stringify(values) && lastStaged.current === null) {
      if (required && next.length === 0) setError(`${ariaLabel} is required.`);

      return;
    }

    if (required && next.length === 0) {
      setError(`${ariaLabel} is required.`);

      return;
    }

    setError("");
    lastStaged.current = draft;

    if (onStage({ kind: "list", items: next }) !== false) {
      clearPersistedEditorDraft(vaultKey, operationTarget);
    }
  }, [ariaLabel, draft, onStage, operationTarget, required, values, vaultKey]);

  if (!editing) {
    return (
      <span className={dirty ? "field-editor__chips is-dirty" : "field-editor__chips"}>
        {values.length ? (
          values.map((item) => <span key={item}>{item}</span>)
        ) : (
          <span className="field-editor__empty">Empty</span>
        )}
      </span>
    );
  }

  if (options) {
    return (
      <fieldset className="field-editor__multi-enum">
        <legend>{ariaLabel}</legend>
        {options.map((option) => (
          <label key={option}>
            <input
              type="checkbox"
              checked={selectedValues.includes(option)}
              onChange={(event) => {
                const next = event.target.checked
                  ? [...selectedValues, option]
                  : selectedValues.filter((value) => value !== option);

                setSelectedValues(next);
                const nextValue = { kind: "list" as const, items: next };
                const expected = onDraft?.(nextValue);

                if (JSON.stringify(next) === JSON.stringify(values))
                  clearPersistedEditorDraft(vaultKey, operationTarget);
                else
                  writePersistedEditorDraft(
                    vaultKey,
                    operationTarget,
                    JSON.stringify(next),
                    expected,
                  );

                if (onStage({ kind: "list", items: next }) !== false) {
                  clearPersistedEditorDraft(vaultKey, operationTarget);
                }
              }}
            />
            {option}
          </label>
        ))}
      </fieldset>
    );
  }

  return (
    <span className="field-editor__control">
      <textarea
        ref={inputRef}
        rows={Math.min(4, Math.max(1, values.length || 1))}
        value={draft}
        className={dirty ? "field-editor__list is-dirty" : "field-editor__list"}
        aria-label={ariaLabel}
        aria-invalid={Boolean(error)}
        aria-describedby={error ? errorID : undefined}
        placeholder="One value per line"
        required={required}
        onChange={(event: ChangeEvent<HTMLTextAreaElement>) => {
          const next = event.target.value;
          draftRef.current = next;
          setDraft(next);
          const nextValues = splitListInput(next);
          const returnsToOriginal = JSON.stringify(nextValues) === JSON.stringify(values);

          const expected = onDraft?.(
            { kind: "list", items: nextValues },
            returnsToOriginal && lastStaged.current !== null,
          );

          if (returnsToOriginal && lastStaged.current === null)
            clearPersistedEditorDraft(vaultKey, operationTarget);
          else writePersistedEditorDraft(vaultKey, operationTarget, next, expected);

          if (event.target.value.trim() !== "") setError("");
        }}
        onCompositionStart={() => (composing.current = true)}
        onCompositionEnd={() => (composing.current = false)}
        onBlur={accept}
        onKeyDown={(event: KeyboardEvent<HTMLTextAreaElement>) => {
          if (composing.current || event.nativeEvent.isComposing) return;

          if (event.key === "Escape") {
            event.preventDefault();
            cancelBlur.current = true;
            const next = lastStaged.current ?? values.join("\n");
            draftRef.current = next;
            setDraft(next);
            clearPersistedEditorDraft(vaultKey, operationTarget);
            setError("");
            inputRef.current?.blur();
          } else if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
            event.preventDefault();
            accept();
            cancelBlur.current = true;
            inputRef.current?.blur();
          }
        }}
      />
      {error ? (
        <small id={errorID} className="field-editor__reason" role="alert">
          {error}
        </small>
      ) : null}
    </span>
  );
}
