import type {
  AgentHarnessKind,
  AgentHarnessStatus,
  AgentSettings,
  AgentSettingsResponse,
} from "../../api/types";
import { humanizeName } from "../../lib/labels";
import { DisclosureChevron } from "../DisclosureChevron";

const defaultSettings: AgentSettings = {
  harnesses: {
    codex: { permissionMode: "approval-required" },
    claude: { permissionMode: "approval-required" },
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
  resolvedHarness?: AgentHarnessKind;
  resolutionReason: string;
  statuses: StatusEntry[];
  saving: boolean;
  onChange: (settings: AgentSettings) => void;
};

const labels: Record<AgentHarnessKind, string> = { codex: "Codex", claude: "Claude" };

function harnessKind(value: string): AgentHarnessKind {
  return value === "claude" ? "claude" : "codex";
}

function fallbackHarness(settings: AgentSettings, resolved?: AgentHarnessKind): AgentHarnessKind {
  return settings.harness || resolved || "codex";
}

export function AgentSettingsPanel({
  settings,
  resolvedHarness,
  resolutionReason,
  statuses,
  saving,
  onChange,
}: Props) {
  const harness = fallbackHarness(settings, resolvedHarness);
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

  const updateHarnessSettings = (
    kind: AgentHarnessKind,
    change: Partial<AgentSettings["harnesses"][AgentHarnessKind]>,
  ) => {
    onChange({
      ...settings,
      harness: kind,
      harnesses: {
        ...settings.harnesses,
        [kind]: { ...settings.harnesses[kind], ...change },
      },
    });
  };

  return (
    <section className="agent-settings" aria-label="Harness settings">
      {!settings.harness ? <p className="agent-muted">Resolved: {resolutionReason}</p> : null}
      <div className="agent-model-controls">
        <label>
          Harness
          <select
            aria-label="Harness"
            value={harness}
            disabled={saving}
            onChange={(event) => {
              const kind = harnessKind(event.target.value);
              updateHarnessSettings(kind, {});
            }}
          >
            {(["codex", "claude"] as const).map((kind) => (
              <option key={kind} value={kind}>
                {labels[kind]}
              </option>
            ))}
          </select>
        </label>
        <label>
          Model
          <select
            aria-label="Model"
            value={selectedModel}
            disabled={saving || models.length === 0}
            onChange={(event) => {
              const nextModel = models.find((item) => item.id === event.target.value);
              const nextEfforts = nextModel?.efforts ?? [];
              updateHarnessSettings(harness, {
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
        <summary aria-label={`Session settings: ${humanizeName(configured.permissionMode)}`}>
          <DisclosureChevron />
          <span>Settings</span>
          <span className="agent-session-settings__active">
            {humanizeName(configured.permissionMode)}
          </span>
        </summary>
        <div className="agent-model-controls">
          <label>
            Reasoning effort
            <select
              aria-label="Reasoning effort"
              value={efforts.includes(configured.effort || "") ? configured.effort : ""}
              disabled={saving || efforts.length === 0}
              onChange={(event) => updateHarnessSettings(harness, { effort: event.target.value })}
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
                updateHarnessSettings(harness, {
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
