// The composer's floating panels (SPEC-0140): the model and reasoning picker
// and the permission mode menu, placed above the composer.
import {
  type CSSProperties,
  type KeyboardEvent,
  type RefObject,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";

import {
  agentGroups,
  type ChatChoice,
  choiceForModel,
  effortFor,
  effortRows,
  HARNESS_LABELS,
  modelFor,
  type ModelRow,
  PERMISSION_MODES,
  readiness,
  statusOf,
} from "./chatAgent";
import type { ChatAgent } from "./ChatAgentControls";

export const IS_MAC =
  typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

export const commandKey = (event: KeyboardEvent | globalThis.KeyboardEvent) =>
  IS_MAC ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;

/**
 * Places a floating panel above the composer, from its trigger's left edge
 * but never past the composer's right edge, so in the narrow right pane it
 * floats over the page.
 */
function placement(trigger: HTMLElement | null, width: number): CSSProperties {
  if (!trigger) return { visibility: "hidden" };

  const frame = (trigger.closest(".agent-composer__frame") ?? trigger).getBoundingClientRect();
  const fits = Math.min(width, window.innerWidth - 16);
  const left = Math.min(trigger.getBoundingClientRect().left, frame.right - fits);

  return {
    left: Math.max(8, Math.min(left, window.innerWidth - 8 - fits)),
    bottom: window.innerHeight - frame.top + 8,
    width: fits,
    // A short window or a tall draft leaves less room above the composer.
    maxHeight: Math.max(0, frame.top - 16),
  };
}

// Placed on the first render, since a hidden panel cannot take focus.
function usePlacement(anchor: RefObject<HTMLElement | null>, width: number) {
  const [style, setStyle] = useState(() => placement(anchor.current, width));

  useEffect(() => {
    const place = () => setStyle(placement(anchor.current, width));

    window.addEventListener("resize", place);

    return () => window.removeEventListener("resize", place);
  }, [anchor, width]);

  return style;
}

/** A press outside the panel and its trigger closes it, leaving focus where the press put it. */
function useOutsidePress(
  panel: RefObject<HTMLElement | null>,
  anchor: RefObject<HTMLElement | null>,
  onClose: (restoreFocus: boolean) => void,
) {
  const close = useRef(onClose);

  close.current = onClose;

  useEffect(() => {
    const onPointerDown = (event: MouseEvent) => {
      const target = event.target instanceof Node ? event.target : null;

      if (!panel.current?.contains(target) && !anchor.current?.contains(target))
        close.current(false);
    };

    document.addEventListener("mousedown", onPointerDown);

    return () => document.removeEventListener("mousedown", onPointerDown);
  }, [panel, anchor]);
}

const STATE_NOTES: Partial<Record<ReturnType<typeof readiness>, string>> = {
  "signed-out": "signed out",
  "not-installed": "not installed",
};

type PanelProps = {
  agent: ChatAgent;
  choice: ChatChoice;
  anchor: RefObject<HTMLButtonElement | null>;
  onClose: (restoreFocus: boolean) => void;
};

export function ModelPicker({
  agent,
  choice,
  anchor,
  reasoning,
  onClose,
}: PanelProps & { reasoning: boolean }) {
  const { response } = agent;
  const [query, setQuery] = useState("");
  const groups = agentGroups(response, query);
  const rows = groups.flatMap((group) => group.rows);
  const isCurrent = (row: ModelRow) => row.harness === choice.harness && row.id === choice.model;
  const [active, setActive] = useState(() => Math.max(0, rows.findIndex(isCurrent)));
  const panel = useRef<HTMLDivElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const style = usePlacement(anchor, 430);
  const id = useId();
  const optionID = (index: number) => `${id}-option-${index}`;

  useOutsidePress(panel, anchor, onClose);

  const model = modelFor(statusOf(response, choice.harness), choice.model);
  const efforts = effortRows(model);
  const effort = effortFor(model, choice.effort);

  const focusEfforts = () =>
    panel.current?.querySelector<HTMLElement>('[role="radio"][aria-checked="true"]')?.focus();

  useLayoutEffect(() => {
    if (reasoning && efforts.length) focusEfforts();
    else search.current?.focus();
    // Only where the picker opens; later moves are the person's.
  }, [reasoning]);

  useEffect(() => {
    document.getElementById(optionID(active))?.scrollIntoView?.({ block: "nearest" });
  }, [active]);

  const pick = (row: ModelRow | undefined) => {
    if (!row) return;
    agent.choose(choiceForModel(response, choice, row));
    onClose(true);
  };

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      onClose(true);
    } else if (commandKey(event) && /^[1-9]$/.test(event.key)) {
      event.preventDefault();
      event.stopPropagation();
      pick(rows[Number(event.key) - 1]);
    }
  };

  const onSearchKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    const field = event.currentTarget;

    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const step = event.key === "ArrowDown" ? 1 : -1;
      setActive((index) => Math.max(0, Math.min(rows.length - 1, index + step)));
    } else if (event.key === "Enter") {
      event.preventDefault();
      pick(rows[active]);
    } else if (
      event.key === "ArrowRight" &&
      efforts.length &&
      field.selectionStart === field.value.length
    ) {
      event.preventDefault();
      focusEfforts();
    }
  };

  const onEffortKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    const radios = [...(panel.current?.querySelectorAll<HTMLElement>('[role="radio"]') ?? [])];
    const at = radios.indexOf(event.currentTarget);

    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      radios[
        Math.max(0, Math.min(radios.length - 1, at + (event.key === "ArrowDown" ? 1 : -1)))
      ]?.focus();
    } else if (event.key === "ArrowLeft") {
      event.preventDefault();
      search.current?.focus();
    }
  };

  let index = -1;

  return createPortal(
    <div
      ref={panel}
      className="chat-agent-popover chat-model-picker"
      role="dialog"
      aria-label="Choose a model"
      style={style}
      onKeyDown={onKeyDown}
    >
      <div className="chat-model-picker__search">
        <svg aria-hidden="true" viewBox="0 0 16 16">
          <circle cx="7" cy="7" r="4.5" fill="none" stroke="currentColor" strokeWidth="1.5" />
          <path d="m10.5 10.5 3 3" stroke="currentColor" strokeWidth="1.5" />
        </svg>
        <input
          ref={search}
          type="text"
          role="combobox"
          aria-label="Search models"
          aria-expanded
          aria-controls={`${id}-models`}
          aria-activedescendant={rows[active] ? optionID(active) : undefined}
          placeholder="Search models"
          autoComplete="off"
          spellCheck={false}
          value={query}
          onChange={(event) => {
            setQuery(event.target.value);
            setActive(0);
          }}
          onKeyDown={onSearchKeyDown}
        />
      </div>
      <div className="chat-model-picker__columns">
        <div
          className="chat-model-picker__list"
          id={`${id}-models`}
          role="listbox"
          aria-label="Models"
        >
          {groups.length === 0 ? <p className="chat-model-picker__empty">No models match</p> : null}
          {groups.map((group) => (
            <div key={group.harness} role="group" aria-label={HARNESS_LABELS[group.harness]}>
              <div className="chat-model-picker__heading" aria-hidden="true">
                {HARNESS_LABELS[group.harness]}
                {STATE_NOTES[group.readiness] ? (
                  <span className="chat-model-picker__state">· {STATE_NOTES[group.readiness]}</span>
                ) : null}
              </div>
              {group.rows.map((row) => {
                index += 1;
                const at = index;

                return (
                  <div
                    key={`${row.harness}:${row.id}`}
                    id={optionID(at)}
                    role="option"
                    aria-selected={isCurrent(row)}
                    className={`chat-model-picker__row${at === active ? " is-active" : ""}`}
                    onMouseMove={() => setActive(at)}
                    onClick={() => pick(row)}
                  >
                    <span className="chat-model-picker__check" aria-hidden="true">
                      {isCurrent(row) ? "✓" : ""}
                    </span>
                    <span className="chat-model-picker__name">{row.label}</span>
                    {at < 9 ? (
                      <span className="chat-model-picker__key" aria-hidden="true">
                        {IS_MAC ? "⌘" : "Ctrl+"}
                        {at + 1}
                      </span>
                    ) : null}
                  </div>
                );
              })}
            </div>
          ))}
        </div>
        {efforts.length ? (
          <div className="chat-model-picker__efforts" role="radiogroup" aria-label="Reasoning">
            <div className="chat-model-picker__heading" aria-hidden="true">
              Reasoning
            </div>
            {efforts.map((row) => {
              const checked = row.value === effort;

              return (
                <button
                  key={row.value}
                  type="button"
                  role="radio"
                  aria-checked={checked}
                  tabIndex={checked ? 0 : -1}
                  className="chat-model-picker__effort"
                  onKeyDown={onEffortKeyDown}
                  onClick={() => agent.choose({ ...choice, effort: row.value })}
                >
                  <span>{row.label}</span>
                  {checked ? (
                    <span aria-hidden="true">✓</span>
                  ) : row.isDefault && row.value ? (
                    <span className="chat-model-picker__default">Default</span>
                  ) : null}
                </button>
              );
            })}
          </div>
        ) : null}
      </div>
    </div>,
    document.body,
  );
}

export function ModeMenu({ agent, choice, anchor, onClose }: PanelProps) {
  const offered = statusOf(agent.response, choice.harness)?.capabilities.permissionModes ?? [];

  const modes = PERMISSION_MODES.filter(
    (item) => offered.includes(item.mode) || item.mode === choice.permissionMode,
  );

  const panel = useRef<HTMLDivElement>(null);
  const style = usePlacement(anchor, 300);

  useOutsidePress(panel, anchor, onClose);

  useLayoutEffect(() => {
    panel.current?.querySelector<HTMLElement>('[aria-checked="true"]')?.focus();
  }, []);

  const onKeyDown = (event: KeyboardEvent) => {
    const items = [
      ...(panel.current?.querySelectorAll<HTMLElement>('[role="menuitemradio"]') ?? []),
    ];

    const at = items.findIndex((item) => item === document.activeElement);

    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      onClose(true);
    } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const step = event.key === "ArrowDown" ? 1 : -1;
      items[(at + step + items.length) % items.length]?.focus();
    }
  };

  return createPortal(
    <div
      ref={panel}
      className="chat-agent-popover chat-mode-menu"
      role="menu"
      aria-label="Permission mode"
      style={style}
      onKeyDown={onKeyDown}
    >
      {modes.map((item) => {
        const checked = item.mode === choice.permissionMode;

        return (
          <button
            key={item.mode}
            type="button"
            role="menuitemradio"
            aria-checked={checked}
            tabIndex={-1}
            className="chat-mode-menu__item"
            onClick={() => {
              agent.choose({ ...choice, permissionMode: item.mode });
              onClose(true);
            }}
          >
            <span className="chat-mode-menu__label">{item.label}</span>
            <span className="chat-mode-menu__description">{item.description}</span>
            <span className="chat-mode-menu__check" aria-hidden="true">
              {checked ? "✓" : ""}
            </span>
          </button>
        );
      })}
    </div>,
    document.body,
  );
}
