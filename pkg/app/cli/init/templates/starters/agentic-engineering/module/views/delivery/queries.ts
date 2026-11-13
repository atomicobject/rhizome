import type { Coverage, Ref, SourceLink, Spec, Story, Criterion, Effort, Portfolio, ScopeMaterial, Occurrence } from './lib/index.ts';

export type FieldState = {
  name: string; present: boolean; values: string[] | null; sectionNodes: Ref[] | null;
  links: { value: string; resolved: boolean; ref: Ref | null; title: string | null }[] | null;
  issues: { code: string; message: string }[] | null;
};
export type LiveNode = {
  ref: Ref; title: string; resolvedType: string | null; content?: string; format?: string;
  sectionType?: string;
  locator: { sourceLocator: string; exists: boolean; requiresFix: boolean; linkTarget: { ref: Ref; exists: boolean; requiresFix: boolean } | null };
  workspace: { sourceRevision: { contentFingerprint: string } | null; fields: FieldState[]; collections?: { name: string; items: { ref: Ref }[] }[] } | null;
};
export type Detail<T> = T & { source: LiveNode };
export type LiveCriterion = Detail<Criterion>;
export type LiveStory = Omit<Detail<Story>, 'criteria'> & { criteria: LiveCriterion[] };
export type LiveSpec = Omit<Detail<Spec>, 'stories'> & { stories: LiveStory[] };
export type LiveEffort = Detail<Effort>;
export type RecordDetail = {
  node: LiveNode; record: LiveSpec | LiveEffort | null;
  components: LiveNode[]; selectedTargets: LiveNode[];
  governing: { coverage: Coverage; occurrences: Occurrence[] };
  entryMentions: { coverage: Coverage; occurrences: Occurrence[] };
  workLogSnapshot: ScopeMaterial | null;
  coverage: Coverage;
};
export type PortfolioRead = { portfolio: Portfolio; coverage: Coverage; reviewEvidence: 'deferred' | 'loaded' };

export const REF = 'ref notePath kind fragment nodeId structural typeName';
export const SUMMARY = `ref { ${REF} } title resolvedType locator { sourceLocator exists requiresFix linkTarget { ref { ${REF} } exists requiresFix } }`;
export const FIELDS = `workspace { sourceRevision { contentFingerprint } collections { name items { ref { ${REF} } } } fields { name present values sectionNodes { ${REF} } links { value resolved title ref { ${REF} } } issues { code message } } }`;
export const NODE_SUMMARY = `${SUMMARY} ${FIELDS}`;
export const SECTION_CHILDREN = `ref { ${REF} } title workspace { collections { name items { ref { ${REF} } } } fields { name sectionNodes { ${REF} } issues { code message } } }`;
export const STORY_SUMMARY = `${SUMMARY} workspace { collections { name items { ref { ${REF} } } } fields { name present values sectionNodes { ${REF} } issues { code message } } }`;
export const NODE_DETAIL = `${NODE_SUMMARY} ... on NoteNode { content format } ... on Section { content sectionType: __typename }`;
/** NODE_DETAIL for section types; it is also valid nested inside their typed parent fields. */
export const SECTION_DETAIL = `${NODE_SUMMARY} ... on Section { content sectionType: __typename }`;
/** A source section whose links are read but whose prose is not shown. */
export const REVISION = `ref { ${REF} } workspace { sourceRevision { contentFingerprint } }`;
export const SOURCE_PAGE = `query DeliverySource($ref: String!, $offset: Int!) { node(ref: $ref) { ref { ${REF} } workspace { sourceRevision { contentFingerprint } sourceLinkCount sourceLinks(first: 100, offset: $offset) { target authoredTarget text kind anchor embed targetKind resolved resolvedRef { ${REF} } title preview } } } }`;
export type SourcePage = { node: { ref: Ref; workspace: { sourceRevision: { contentFingerprint: string } | null; sourceLinkCount: number; sourceLinks: SourceLink[] } | null } | null };
export const SPEC_TREE_ALIAS = 'deliveryTree';
/** The section each nested tree field holds, read with the selection hydration would use for it. */
export const SPEC_TREE_CHILDREN = { stories: STORY_SUMMARY, acceptanceCriteria: SECTION_CHILDREN, criteria: SUMMARY } as const;
export const SPEC_DETAIL_CHILDREN = { stories: SECTION_DETAIL, acceptanceCriteria: SECTION_CHILDREN, criteria: SECTION_DETAIL } as const;
// Spec types disagree on whether userStories is nullable, so each gets its own alias.
const specTree = (children: { [K in keyof typeof SPEC_TREE_CHILDREN]: string }) => ['ProductSpec', 'TechnicalSpec', 'ExperienceSpec', 'OperationsSpec'].map(type =>
  `... on ${type} { ${SPEC_TREE_ALIAS}${type}: userStories { ${SECTION_CHILDREN} stories { ${children.stories} acceptanceCriteria { ${children.acceptanceCriteria} criteria { ${children.criteria} } } } } }`).join(' ');
export const SPEC_TREE = specTree(SPEC_TREE_CHILDREN);
export const SPEC_DETAIL_TREE = specTree(SPEC_DETAIL_CHILDREN);
/** Inventory pages read together; with the effort read they fill the browser's five free connections. */
export const SPEC_PAGE_SIZE = 35;
export const SPEC_PAGE_WINDOW = 4;
