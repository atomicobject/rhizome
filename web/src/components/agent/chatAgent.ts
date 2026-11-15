// What chat's composer offers (SPEC-0140): the coding agents that are on,
// their models and efforts, the permission modes, and what keeps a message
// from sending. Pure, so the picker and both composers read one model.
import type {
  AgentHarnessKind,
  AgentHarnessStatus,
  AgentModelOption,
  AgentPermissionMode,
  AgentSettings,
  AgentSettingsResponse,
} from "../../api/types";

export const HARNESS_LABELS: Record<AgentHarnessKind, string> = {
  claude: "Claude Code",
  codex: "Codex",
};

export const INSTALL_DOCS: Record<AgentHarnessKind, string> = {
  claude: "https://docs.anthropic.com/en/docs/claude-code/setup",
  codex: "https://github.com/openai/codex",
};

/** The picker lists Claude Code first. */
const HARNESS_ORDER: AgentHarnessKind[] = ["claude", "codex"];

export const emptySettingsResponse: AgentSettingsResponse = {
  settings: {
    harnesses: { codex: { permissionMode: "auto" }, claude: { permissionMode: "auto" } },
  },
  resolved: { reason: "none available" },
  harnesses: [],
};

export type Readiness = "ready" | "signed-out" | "unchecked" | "not-installed";

export function readiness(status: AgentHarnessStatus | undefined): Readiness {
  if (!status?.installed) return "not-installed";

  if (status.loggedIn) return "ready";

  // A probe that failed cannot tell, and the server still runs the agent.
  return status.lastError ? "unchecked" : "signed-out";
}

/** What chat runs: an agent, its model and effort (empty for the agent's default), and its permission mode. */
export type ChatChoice = {
  harness: AgentHarnessKind;
  model: string;
  effort: string;
  permissionMode: AgentPermissionMode;
};

export const statusOf = (response: AgentSettingsResponse, kind: AgentHarnessKind) =>
  response.harnesses.find((item) => item.kind === kind)?.status;

/** The agents the server drives that Settings has not turned off, in picker order. */
function enabledAgents(response: AgentSettingsResponse) {
  return HARNESS_ORDER.filter(
    (kind) =>
      !response.settings.harnesses[kind]?.disabled &&
      response.harnesses.some((item) => item.kind === kind),
  );
}

/** The model a saved id names, else the agent's default. */
export function modelFor(status: AgentHarnessStatus | undefined, id: string) {
  const models = status?.models ?? [];

  return (
    models.find((model) => model.id === id) ?? models.find((model) => model.default) ?? models[0]
  );
}

/**
 * What the next new chat runs, by the server's rule: the saved agent while it
 * is on, else the first one ready; with none ready, the first installed one,
 * so the notice can say how to sign in.
 */
export function currentChoice(response: AgentSettingsResponse): ChatChoice | undefined {
  const agents = enabledAgents(response);
  const saved = response.settings.harness;
  const resolved = response.resolved.harness;

  const harness =
    (saved && agents.includes(saved) ? saved : undefined) ??
    (resolved && agents.includes(resolved) ? resolved : undefined) ??
    agents.find((kind) => statusOf(response, kind)?.installed);

  if (!harness) return undefined;

  const settings = response.settings.harnesses[harness];
  const model = modelFor(statusOf(response, harness), settings?.model ?? "");

  return {
    harness,
    model: model?.id ?? settings?.model ?? "",
    effort: settings?.effort ?? "",
    permissionMode: settings?.permissionMode ?? "auto",
  };
}

/** The effort a model runs: the chosen one when it offers it, else its default (empty when unnamed). */
export function effortFor(model: AgentModelOption | undefined, effort: string) {
  const efforts = model?.efforts ?? [];

  if (effort && (efforts.length === 0 || efforts.includes(effort))) return effort;

  return model?.defaultEffort ?? "";
}

const EFFORT_LABELS = new Map([
  ["none", "None"],
  ["minimal", "Minimal"],
  ["low", "Low"],
  ["medium", "Medium"],
  ["high", "High"],
  ["xhigh", "Extra high"],
  ["max", "Max"],
  ["ultra", "Ultra"],
]);

export const effortLabel = (effort: string) =>
  effort
    ? (EFFORT_LABELS.get(effort) ?? effort.charAt(0).toUpperCase() + effort.slice(1))
    : "Default";

export type EffortRow = { value: string; label: string; isDefault: boolean };

/** The Reasoning column. Claude Code does not name its default effort, so "Default" leaves it to the agent. */
export function effortRows(model: AgentModelOption | undefined): EffortRow[] {
  const efforts = model?.efforts ?? [];

  if (efforts.length === 0) return [];

  const rows = efforts.map((value) => ({
    value,
    label: effortLabel(value),
    isDefault: value === model?.defaultEffort,
  }));

  return model?.defaultEffort ? rows : [{ value: "", label: "Default", isDefault: true }, ...rows];
}

/** The composer's label for a choice: its model and its effort, when the model has efforts. */
export function choiceLabel(response: AgentSettingsResponse, choice: ChatChoice) {
  const model = modelFor(statusOf(response, choice.harness), choice.model);

  return {
    model: model ? model.displayName || model.id : HARNESS_LABELS[choice.harness],
    effort: model?.efforts?.length ? effortLabel(effortFor(model, choice.effort)) : "",
  };
}

export type ModelRow = { harness: AgentHarnessKind; id: string; label: string };

export type AgentGroup = { harness: AgentHarnessKind; readiness: Readiness; rows: ModelRow[] };

/** The picker's list: each agent that is on, with its models matching the search. */
export function agentGroups(response: AgentSettingsResponse, query = ""): AgentGroup[] {
  const needle = query.trim().toLowerCase();

  return enabledAgents(response).flatMap((harness) => {
    const status = statusOf(response, harness);
    const models = status?.models ?? [];

    // An agent that reported no models, such as one not installed, still
    // offers itself, on its default model.
    const rows = models.length
      ? models.map((model) => ({ harness, id: model.id, label: model.displayName || model.id }))
      : [{ harness, id: "", label: HARNESS_LABELS[harness] }];

    const agentMatches = HARNESS_LABELS[harness].toLowerCase().includes(needle);
    const shown = rows.filter((row) => agentMatches || row.label.toLowerCase().includes(needle));

    return shown.length ? [{ harness, readiness: readiness(status), rows: shown }] : [];
  });
}

/** Choosing a model keeps the effort and permission mode it can, from the current choice or the agent's saved settings. */
export function choiceForModel(
  response: AgentSettingsResponse,
  current: ChatChoice | undefined,
  row: ModelRow,
): ChatChoice {
  const same = current?.harness === row.harness ? current : undefined;
  const saved = response.settings.harnesses[row.harness];
  const efforts = modelFor(statusOf(response, row.harness), row.id)?.efforts ?? [];
  const effort = same ? same.effort : (saved?.effort ?? "");

  return {
    harness: row.harness,
    model: row.id,
    effort: effort && efforts.includes(effort) ? effort : "",
    permissionMode: same?.permissionMode ?? saved?.permissionMode ?? "auto",
  };
}

/** The settings a choice saves: the agent chat uses, and that agent's model, effort, and mode. */
export function withChoice(settings: AgentSettings, choice: ChatChoice): AgentSettings {
  return {
    ...settings,
    harness: choice.harness,
    harnesses: {
      ...settings.harnesses,
      [choice.harness]: {
        ...settings.harnesses[choice.harness],
        model: choice.model,
        effort: choice.effort,
        permissionMode: choice.permissionMode,
      },
    },
  };
}

export const PERMISSION_MODES: {
  mode: AgentPermissionMode;
  label: string;
  description: string;
}[] = [
  {
    mode: "approval-required",
    label: "Ask first",
    description: "Asks before every command and edit.",
  },
  {
    mode: "auto",
    label: "Auto",
    description: "A reviewer allows routine actions and asks you about risky ones.",
  },
  {
    mode: "auto-accept-edits",
    label: "Accept edits",
    description: "Edits files without asking, and asks before commands.",
  },
  { mode: "full-access", label: "Full access", description: "Runs every action without asking." },
];

/** What keeps a message from sending, which the notice above the composer explains. */
export type Blocker =
  | { kind: "signed-out"; harness: AgentHarnessKind; command?: string }
  | { kind: "not-installed"; harness: AgentHarnessKind }
  | { kind: "none-installed" }
  | { kind: "all-off" };

export function blockerFor(
  response: AgentSettingsResponse,
  choice: ChatChoice | undefined,
): Blocker | undefined {
  if (!choice)
    return enabledAgents(response).length ? { kind: "none-installed" } : { kind: "all-off" };

  const status = statusOf(response, choice.harness);

  switch (readiness(status)) {
    case "signed-out":
      return { kind: "signed-out", harness: choice.harness, command: status?.loginHint };
    case "not-installed":
      return { kind: "not-installed", harness: choice.harness };
    default:
      return undefined;
  }
}
