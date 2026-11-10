/** Adapter inputs are facts from public reads, never inferred link membership. */
export type Ref = {
  notePath: string;
  kind: string;
  fragment?: string | null;
  nodeId?: string | null;
  structuralFingerprint?: string | null;
  typeName?: string | null;
  startByte?: number | null;
  endByte?: number | null;
  parentId?: string | null;
};

export function identity(ref: Ref): string {
  return JSON.stringify([ref.notePath, ref.fragment ?? '', ref.nodeId ?? '', ref.kind,
    ref.structuralFingerprint ?? '', ref.typeName ?? '']);
}

export type Coverage = {
  complete: boolean;
  errors: readonly string[];
  truncated: boolean;
  sourceRevision: string | null;
  graphGeneration: string | number | null;
};

export type RecordSummary = {
  ref: Ref;
  title: string;
  /** Canonical path plus a public durable fragment, supplied by the adapter. */
  locator: string | null;
  coverage: Coverage;
};
export type Story = RecordSummary & {
  status: 'draft' | 'ready' | 'satisfied' | null;
  criteria: readonly Criterion[];
  criteriaCoverage: Coverage;
};
export type Criterion = RecordSummary & { story: Ref };
export type Spec = RecordSummary & {
  kind: 'spec';
  status: 'proposed' | 'active' | 'superseded' | 'archived' | null;
  stories: readonly Story[];
  storiesCoverage: Coverage;
};
export type Checklist =
  | { state: 'missing' | 'unreadable' }
  | { state: 'empty' | 'recorded'; checked: number; total: number };
export type Effort = RecordSummary & {
  kind: 'effort';
  form: 'EffortNote' | 'EffortWorkspace';
  status: 'planned' | 'active' | 'complete' | 'archived' | null;
  checklist: Checklist;
  scope: FrozenScope;
};

export type SourceLink = {
  target: string;
  authoredTarget: string;
  text: string | null;
  kind: string;
  anchor: string | null;
  embed: boolean;
  targetKind: string;
  resolved: boolean;
  resolvedRef: Ref | null;
  title: string | null;
  preview: string | null;
};
export type Occurrence = {
  link: SourceLink;
  source: Ref;
  /** Position in the focused public source-link projection, before hydration. */
  ordinal: number;
  authored: string;
  target: Ref | null;
  targetKind: 'spec' | 'story' | 'criterion' | 'unsupported' | 'unresolved';
};
export type ScopeSection = {
  state: 'present' | 'missing' | 'duplicate' | 'unreadable' | 'unsupported';
  coverage: Coverage;
  occurrences: readonly Occurrence[];
};
export type ScopeMaterial = {
  ref: Ref;
  supported: boolean;
  coverage: Coverage;
  specs: ScopeSection;
  stories: ScopeSection;
};
export type ScopeSelector = {
  state: 'selected' | 'missing' | 'broken' | 'duplicate' | 'unsupported' | 'unreadable';
  authored: readonly string[];
  coverage: Coverage;
  material: ScopeMaterial | null;
};
export type FrozenScope = {
  state: 'known' | 'unknown' | 'incomplete';
  source: Ref | null;
  authored: readonly string[];
  specs: readonly Occurrence[];
  selections: readonly Occurrence[];
  coverage: Coverage;
  reasons: readonly string[];
};
export type Portfolio = {
  specs: readonly Spec[];
  efforts: readonly Effort[];
  specCoverage: Coverage;
  effortCoverage: Coverage;
};
