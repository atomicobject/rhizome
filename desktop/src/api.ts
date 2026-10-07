import { Channel, invoke } from "@tauri-apps/api/core";
import { open } from "@tauri-apps/plugin-dialog";

export interface Repository {
  id: string;
  root: string;
  name: string;
  primary?: string;
  executables?: Record<string, string>;
  acknowledged: string[];
  lastOpened?: number;
}
export interface Library {
  version: 2;
  repositories: Repository[];
  globalExecutable?: string;
  /** Counts library saves since the app launched; a higher revision is newer. */
  revision: number;
}
export interface Worktree {
  path: string;
  branch?: string;
  head?: string;
  main?: boolean;
  configured: boolean;
  hasDatabase: boolean;
  trusted: boolean;
  trustRequired: boolean;
  authority?: "managed" | "development" | "global" | "external";
  /** The local build a developer override selects. */
  executable?: string;
  error?: string;
}
export interface Discovery {
  id: string;
  root: string;
  name: string;
  git: boolean;
  defaultBranch?: string;
  /** The vault's folder below each Git worktree root, for a subfolder vault. */
  subpath?: string;
  defaultPrimary: string;
  primary: string;
  worktrees: Worktree[];
}
export interface GlobalInfo {
  path: string;
  managedPath: string;
  version: string;
  installed: boolean;
  canUpdate: boolean;
}
export interface Failure {
  code: string;
  message: string;
}
export interface WindowSession {
  sidebarCollapsed: boolean;
  repository?: string;
  worktree?: string;
}
export type Step =
  | "checking"
  | "trust"
  | "setup"
  | "seeding"
  | "starting"
  | "loading"
  | "ready"
  /** The user stopped this worktree's runtime; opening it again starts it. */
  | "sleeping"
  | "error"
  | "seed-error";
export interface OpenState {
  type: "open";
  generation: number;
  started: number;
  repository: string;
  worktree: string;
  step: Step;
  code?: string;
  message?: string;
  primary?: string;
  /** Set when the app, not the user, began this open. */
  reconnecting?: "stopped" | "moved";
}
export type RuntimeState = "running" | "starting" | "stopped";
export interface Runtime {
  state: RuntimeState;
  mode: "headless" | "attached" | null;
}
export interface Presence {
  type: "presence";
  repositories: Record<string, { info?: Discovery; error?: Failure }>;
  runtimes: Record<string, Runtime>;
}
export type Message =
  | OpenState
  | Presence
  | { type: "library"; library: Library }
  /** A worktree opened from the terminal, chosen in this window. */
  | { type: "select"; library: Library; repository: string; worktree: string }
  | ({ type: "alert" } & Failure)
  /** A brief confirmation, such as after Copy Page URL. */
  | { type: "notice"; message: string }
  | { type: "menu"; id: string }
  | { type: "command"; command: "add-repository" | "toggle-sidebar" | "settings" };
/** Choices a setup sheet passes to `rzm init`; see SPEC-0118. */
export interface SetupChoices {
  workflow: string;
  addons: string[];
  agents: string[];
  search: string;
  skip: string[];
  keepIndexed: string[];
  includeIgnored: string[];
}
export interface ScopeRule {
  /** default is the built-in list. */
  layer: "default" | "gitignore" | "rhizome" | "config";
  source?: string;
  line?: number;
  pattern: string;
  /** The folder a nested .gitignore applies to. */
  folder?: string;
  /** Why init suggested skipping the path. */
  reason?: string;
  included?: boolean;
  /** A rule setup will write. */
  planned?: boolean;
}
export interface Scope {
  builtInApplies: boolean;
  rules: ScopeRule[];
  keepIndexed: string[];
}
export interface ScopeEdits {
  skip: string[];
  removeRules: string[];
  includeIgnored: string[];
}
export interface SearchProvider {
  id: string;
  label: string;
  ready: boolean;
  key?: string;
  keyLabel?: string;
  teamKey?: boolean;
  hint?: string;
}
/** `rzm init --check --json`: what setup would do, and every choice it offers. */
export interface SetupPlan {
  root: string;
  name: string;
  findings: { docs: string; code: string; skip?: string };
  workflow: string;
  workflows: { id: string; label: string; description: string; recommended: boolean }[];
  addons: {
    id: string;
    label: string;
    description: string;
    defaultFor: string[];
    enabled: boolean;
  }[];
  agents: { id: string; label: string; enabled: boolean; detected?: "marker" | "installed" }[];
  search: { provider: string; ready: boolean; hint?: string; providers: SearchProvider[] };
  ignoredRepositories: { path: string; included: boolean; source?: string; line?: number }[];
  pin: string;
  scope: Scope;
  writes: { summary: string[]; files: string[] };
}
/** `rzm init --json`: what setup wrote. */
export interface SetupResult {
  root: string;
  summary: string[];
  commit: string[];
  warnings: string[];
  kept: string[];
  savedKey?: string;
  searchHint?: string;
  pin: { version?: string; error?: string };
}
export interface MenuEntry {
  id?: string;
  label?: string;
  checked?: boolean;
  disabled?: boolean;
  separator?: boolean;
  items?: MenuEntry[];
}
type Results = {
  list: Library;
  discover: Discovery;
  add: { library: Library; id: string };
  remove: Library;
  "set-primary": Library;
  "set-executable": Library;
  "set-global-executable": Library;
  "global-status": GlobalInfo;
  "global-install": GlobalInfo;
  "global-update": GlobalInfo;
  trust: unknown;
  "setup-report": SetupPlan;
  initialize: { result: SetupResult; folder: unknown };
  scope: Scope & { root: string };
  "scope-edit": Scope & { root: string };
  open: { generation: number; library: Library };
  deselect: null;
  restart: null;
  stop: null;
  reveal: null;
  "open-in-browser": null;
  reorder: Library;
  browse: null;
  session: null;
  layout: null;
  menu: null;
};
type Args = {
  list: Record<string, never>;
  discover: { id: string };
  add: { path: string };
  remove: { id: string };
  "set-primary": { id: string; worktree: string | null };
  "set-executable": { id: string; worktree: string; executable: string | null };
  "set-global-executable": { executable: string | null };
  "global-status": Record<string, never>;
  "global-install": Record<string, never>;
  "global-update": Record<string, never>;
  trust: { id: string; worktree: string };
  /** Without choices, the report uses Rhizome's recommendations. */
  "setup-report": { id: string; worktree: string; choices?: SetupChoices };
  /** A key is sent only here, never with a report. */
  initialize: { id: string; worktree: string; choices: SetupChoices; key?: string };
  scope: { id: string; worktree: string };
  "scope-edit": { id: string; worktree: string; edits: ScopeEdits };
  /** `selection` numbers the user's choices in this window, oldest first. */
  open: { id: string; worktree: string; skipSeed?: boolean; selection: number };
  deselect: { selection: number };
  restart: { id: string; worktree: string };
  stop: { worktree: string };
  reveal: { worktree: string };
  "open-in-browser": { id: string; worktree: string };
  /** Repository ids in the order the sidebar lists them. */
  reorder: { ids: string[] };
  browse: { to: "back" | "forward" | "reload" };
  session: { repository: string | null; worktree: string | null; sidebarCollapsed: boolean };
  layout: { x: number; y: number; width: number; height: number; covered: boolean };
  menu: { x: number; y: number; items: MenuEntry[] };
};
export function request<K extends keyof Results>(operation: K, args: Args[K]): Promise<Results[K]> {
  return invoke("desktop_request", { action: { operation, ...args } });
}
export function attach(onMessage: (message: Message) => void): Promise<WindowSession> {
  const channel = new Channel<Message>();
  channel.onmessage = onMessage;
  return invoke("desktop_attach", { channel });
}
export async function pickPath(directory: boolean): Promise<string | null> {
  const value = await open({
    directory,
    multiple: false,
    title: directory ? "Add a repository" : "Choose a Rhizome executable",
  });
  return typeof value === "string" ? value : null;
}
export function failure(error: unknown): Failure {
  if (typeof error === "object" && error !== null && "message" in error) {
    return {
      code: "code" in error && typeof error.code === "string" ? error.code : "desktop_error",
      message: String(error.message),
    };
  }
  return { code: "desktop_error", message: String(error) };
}
