import type {
  AgentHarnessKind,
  AgentHarnessStatus,
  AgentSettings,
  AgentSettingsResponse,
} from "../../api/types";
import { humanizeName } from "../../lib/labels";
import { DisclosureChevron } from "../DisclosureChevron";
import { HARNESS_LABELS } from "./AgentSetup";

const defaultSettings: AgentSettings = {
  harnesses: {
    codex: { permissionMode: "auto" },
    claude: { permissionMode: "auto" },
  },
};

export const emptySettingsResponse: AgentSettingsResponse = {
  settings: defaultSettings,
  resolved: { reason: "none available" },
  harnesses: [],
};

type StatusEntry = { kind: AgentHarnessKind; status: AgentHarnessStatus };

type Props = {
  settings: AgentSettings;
  statuses: StatusEntry[];
  saving: boolean;
  onChange: (settings: AgentSettings) => void;
};

function harnessKind(value: string): AgentHarnessKind {
  return value === "claude" ? "claude" : "codex";
}

export function AgentSettingsPanel({ settings, statuses, saving, onChange }: Props) {
  const harness = settings.harness;

  const choose = (kind: AgentHarnessKind) => onChange({ ...settings, harness: kind });

  const picker = (
    <label>
      Harness
      <select
        aria-label="Harness"
        value={harness ?? ""}
        disabled={saving}
        onChange={(event) => choose(harnessKind(event.target.value))}
      >
        {harness ? null : (
          <option value="" disabled>
            Choose…
          </option>
        )}
        {(["codex", "claude"] as const).map((kind) => (
          <option key={kind} value={kind}>
            {HARNESS_LABELS[kind]}
          </option>
        ))}
      </select>
    </label>
  );

  // Nothing reads as chosen until the person picks a harness.
  if (!harness) {
    return (
      <section className="agent-settings" aria-label="Harness settings">
        <div className="agent-model-controls">{picker}</div>
      </section>
    );
  }

  const configured = settings.harnesses[harness];
  const status = statuses.find((item) => item.kind === harness)?.status;
  const models = status?.models ?? [];

  const selectedModel =
    configured.model || models.find((model) => model.default)?.id || models[0]?.id || "";

  const model = models.find((option) => option.id === selectedModel);
  const efforts = model?.efforts ?? [];
  const permissionModes = status?.capabilities.permissionModes ?? [];
  // A harness that reports no modes still shows the configured one, not an empty box.
  const modeOptions = permissionModes.length ? permissionModes : [configured.permissionMode];

  const updateHarnessSettings = (change: Partial<AgentSettings["harnesses"][AgentHarnessKind]>) => {
    onChange({
      ...settings,
      harnesses: {
        ...settings.harnesses,
        [harness]: { ...configured, ...change },
      },
    });
  };

  return (
    <section className="agent-settings" aria-label="Harness settings">
      <div className="agent-model-controls">
        {picker}
        <label>
          Model
          <select
            aria-label="Model"
            value={selectedModel}
            disabled={saving || models.length === 0}
            onChange={(event) => {
              const nextModel = models.find((item) => item.id === event.target.value);
              const nextEfforts = nextModel?.efforts ?? [];
              updateHarnessSettings({
                model: event.target.value,
                effort: nextEfforts.includes(configured.effort || "") ? configured.effort : "",
              });
            }}
          >
            {models.length === 0 ? <option value="">None reported</option> : null}
            {models.map((option) => (
              <option key={option.id} value={option.id}>
                {option.displayName || option.id}
                {option.default ? " (default)" : ""}
              </option>
            ))}
          </select>
        </label>
      </div>
      {/* Effort and permission are per-session tuning: behind one disclosure
          whose summary still names the active permission. */}
      <details className="agent-session-settings">
        <summary
          aria-label={`Settings: ${configured.permissionMode === "approval-required" ? "Tool approval: ask first" : `Tool approval: ${humanizeName(configured.permissionMode)}`}`}
        >
          <DisclosureChevron />
          <span>Settings</span>
          <span className="agent-session-settings__active">
            {configured.permissionMode === "approval-required"
              ? "Tool approval: ask first"
              : `Tool approval: ${humanizeName(configured.permissionMode)}`}
          </span>
        </summary>
        <div className="agent-model-controls">
          <label>
            Reasoning effort
            <select
              aria-label="Reasoning effort"
              value={efforts.includes(configured.effort || "") ? configured.effort : ""}
              disabled={saving || efforts.length === 0}
              onChange={(event) => updateHarnessSettings({ effort: event.target.value })}
            >
              <option value="">Default</option>
              {efforts.map((effort) => (
                <option key={effort} value={effort}>
                  {humanizeName(effort)}
                </option>
              ))}
            </select>
          </label>
          <label>
            Permission mode
            <select
              aria-label="Permission mode"
              value={configured.permissionMode}
              disabled={saving || permissionModes.length === 0}
              onChange={(event) =>
                updateHarnessSettings({
                  permissionMode:
                    permissionModes.find((mode) => mode === event.target.value) ||
                    configured.permissionMode,
                })
              }
            >
              {modeOptions.map((mode) => (
                <option key={mode} value={mode}>
                  {humanizeName(mode)}
                </option>
              ))}
            </select>
          </label>
        </div>
      </details>
    </section>
  );
}
