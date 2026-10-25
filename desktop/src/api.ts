import { Channel, invoke } from "@tauri-apps/api/core";
import { open } from "@tauri-apps/plugin-dialog";
import type { ThemeName } from "../../web/src/lib/appearance";

declare global {
  interface Window {
    /** Marks the web UI's content view; read by the shared appearance module. */
    __RHIZOME_DESKTOP__?: boolean;
    /** Set by the finder overlay's initialization script. */
    __RHIZOME_FINDER__?: boolean;
  }
}

export interface Repository {
  id: string;
  root: string;
  name: string;
  primary?: string;
  executables?: Record<string, string>;
  acknowledged: string[];
  lastOpened?: number;
  /** The app version whose pin offer was dismissed; a newer app offers again. */
  pinOfferDismissed?: string;
}
export interface Library {
  version: 3;
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
  /** Present when a managed pin differs from the app's Rhizome. */
  pinOffer?: PinOffer;
  error?: string;
}
export interface PinOffer {
  /** `advance`: the pin is older than the app; `app-older`: the app is older. */
  state: "advance" | "app-older";
  pinned: string;
  available: string;
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
export interface CredentialSource {
  account: string;
  environment: string;
  keys: string[];
}
export interface CredentialStatus {
  source?: CredentialSource;
  available: boolean;
  ready: string[];
}
export interface GlobalInfo {
  path: string;
  version: string;
  /** True when the global executable is the one inside this app. */
  bundled: boolean;
}
export type LinkState =
  | "absent"
  | "installed"
  | "other-bundle"
  | "dangling"
  | "foreign"
  | "unsupported";
export interface LinkInfo {
  linkPath: string;
  state: LinkState;
  target?: string;
  /** What the user's login shell runs for `rzm`. */
  shell: { path?: string; version?: string; isApp: boolean; error?: string };
}
export type UpdateStatus =
  | { state: "disabled" | "idle" | "checking" }
  | { state: "current" | "downloading" | "ready"; version: string }
  | { state: "failed"; message: string };
/** Whether new content views record render timings; `always` in Rhizome Dev. */
export interface RenderProfiling {
  enabled: boolean;
  always: boolean;
}
export interface Failure {
  code: string;
  message: string;
}
/** A failure about a worktree may name it, so the banner can act on it. */
export interface Alert extends Failure {
  repository?: string;
  worktree?: string;
}
export interface WindowSession {
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
  | ({ type: "alert" } & Alert)
  /** A brief confirmation, such as after Copy Page URL. */
  | { type: "notice"; message: string }
  | { type: "menu"; id: string }
  | { type: "update"; status: UpdateStatus }
  | {
      type: "command";
      command: "add-repository" | "settings" | "focus-search" | "repositories" | "worktrees";
    }
  /** The finder's choice, relayed by the app. */
  | ({ type: "finder" } & FinderChoice)
  | { type: "settings"; settings: AppSettings }
  /** The workspace page's reported state, or null when no page has reported. */
  | { type: "page"; state: PageState | null };
/** The app-wide appearance; `auto` follows the operating system. */
export type AppearanceChoice = "auto" | "light" | "dark";
export interface AppSettings {
  theme: ThemeName;
  appearance: AppearanceChoice;
}
export type FinderMode = "repositories" | "worktrees";
/** One finder row: a repository, or one worktree of the current repository. */
export interface FinderRow {
  repository: string;
  /** A worktree row's path; a repository row opens its primary worktree. */
  worktree?: string;
  title: string;
  detail: string;
  runtime: "running" | "starting" | "stopped" | "sleeping";
  /** A worktree the user has not opened yet. */
  fresh?: boolean;
  primary?: boolean;
  /** What this window shows. */
  current?: boolean;
}
/** What the shell asks the finder to show. `opened` changes for each new
 * opening, so the finder starts a fresh query then but not on live updates. */
export interface FinderModel {
  mode: FinderMode;
  opened: number;
  theme: ThemeName;
  rows: FinderRow[];
}
export type FinderChoice =
  | { choice: "close" }
  | { choice: "add" }
  | { choice: "open"; repository: string; worktree: string | null; newWindow: boolean }
  | { choice: "menu"; repository: string; x: number; y: number };
export type PageSection = "notes" | "ontology" | "explorer" | "agent" | "graphql";
/** What the toolbar's page controls show; see SPEC-0119. */
export interface PageState {
  section: PageSection;
  search: string;
  /** Present on Notes once validation has run. */
  issues?: number;
  health?:
    | "never_checked"
    | "running"
    | "current_clean"
    | "current_issues"
    | "incomplete"
    | "failed"
    | "stale";
}
export type PageCommand =
  | { command: "section"; section: PageSection }
  | { command: "search"; query: string }
  | { command: "issues" }
  | { command: "shortcuts" }
  | { command: "find" };
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
  "credential-status": CredentialStatus;
  "credential-import": CredentialStatus;
  "credential-cancel": null;
  list: Library;
  discover: Discovery;
  add: { library: Library; id: string };
  remove: Library;
  "set-primary": Library;
  "set-executable": Library;
  "set-global-executable": Library;
  "global-status": GlobalInfo;
  "update-status": UpdateStatus;
  "check-for-updates": UpdateStatus;
  "install-update": null;
  "render-profiling": RenderProfiling;
  "set-render-profiling": RenderProfiling;
  "cli-link-status": LinkInfo;
  "cli-link-install": LinkInfo;
  "cli-link-remove": LinkInfo;
  "pin-advance": unknown;
  "dismiss-pin-offer": Library;
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
  browse: null;
  page: null;
  session: null;
  layout: null;
  menu: null;
  finder: null;
  "finder-choice": null;
  settings: AppSettings;
  "set-settings": AppSettings;
};
type Args = {
  "credential-status": Record<string, never>;
  "credential-import": { source: CredentialSource };
  "credential-cancel": Record<string, never>;
  list: Record<string, never>;
  discover: { id: string };
  add: { path: string };
  remove: { id: string };
  "set-primary": { id: string; worktree: string | null };
  "set-executable": { id: string; worktree: string; executable: string | null };
  "set-global-executable": { executable: string | null };
  "global-status": Record<string, never>;
  "update-status": Record<string, never>;
  "check-for-updates": Record<string, never>;
  "install-update": Record<string, never>;
  "render-profiling": Record<string, never>;
  "set-render-profiling": { enabled: boolean };
  "cli-link-status": Record<string, never>;
  "cli-link-install": Record<string, never>;
  "cli-link-remove": Record<string, never>;
  "pin-advance": { id: string; worktree: string };
  "dismiss-pin-offer": { id: string; version: string };
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
  browse: { to: "back" | "forward" | "reload" };
  page: PageCommand;
  session: { repository: string | null; worktree: string | null };
  layout: { x: number; y: number; width: number; height: number; covered: boolean };
  menu: { x: number; y: number; items: MenuEntry[] };
  /** Shows the finder with `model`, or hides it with null. */
  finder: { model: FinderModel | null; returnToContent?: boolean };
  "finder-choice": FinderChoice;
  settings: Record<string, never>;
  "set-settings": Partial<AppSettings>;
};
export function request<K extends keyof Results>(operation: K, args: Args[K]): Promise<Results[K]> {
  return invoke("desktop_request", { action: { operation, ...args } });
}
export function attach(onMessage: (message: Message) => void): Promise<WindowSession> {
  const channel = new Channel<Message>();
  channel.onmessage = onMessage;
  return invoke("desktop_attach", { channel });
}
/** Connects the finder overlay to the models its window's shell sends. */
export function attachFinder(onModel: (model: FinderModel) => void): Promise<null> {
  const channel = new Channel<FinderModel>();
  channel.onmessage = onModel;
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
