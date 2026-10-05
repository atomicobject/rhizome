# Component test ownership

- Preview timing belongs to `NoteLinkPreview` with real pointer/focus events and deferred HTTP responses; keep geometry arithmetic in `previewPosition` tests.
- Markdown link resolution belongs to `useRhizomeMarkdownLinks` through the real search client and fake HTTP, including ambiguous names and explicit unindexed paths.
- Assert unified diff numbering in rendered rows and gutters, not through an exported parser.
- HTML viewer request replay tests must settle the first prompt before resending its ID, then prove a fresh ID still opens. Keep browser isolation and download proof in Playwright.
- Keep source and narrative editor staging contracts at their separate owners; each must carry its own source witness.
- Keep graph renderer continuity, Problems labels, and click identity at the mounted Sigma boundary; browser tests own actual graph and atlas geometry.
- Board read-only tests must use a working stage callback in both editable and read-only states. Browser move tests must observe the card in its new column.
- Read the note outline through `onContextChange` and assert the exact scroll target. Graph-backed pane fixtures need projected body blocks when exercising BodyWalker.
- Test tab identity, ordering and close neighbors through the public `useNoteTabs` hook; test URL fragment encoding through `notesRoute` parser/writer tables with literal URLs on both sides, never a round trip alone.
- Pane lifecycle tests use only the `usePaneStack` methods `NoteTab` calls (`openRootPane`, `focusOrPushNodeFromIndex`, `loadPane`, `closePaneAt`); do not re-export the private setter or manufacture `PaneState` fixtures.
- Assert Problems kind and variant counts (from the grouped read) and server file order in rendered `NotesIssuesHome`, with a non-alphabetical server order.
- Settle a deferred stale reply inside `act` before asserting its absence; a negative `waitFor` can pass before the reply is processed.
- Give each negative guard its own fixture: supply the value while omitting the field, or the reverse, so either guard's removal fails.
- Default GraphQL documents are validated with the other operations in `api/graphql/operations.test.ts`; applied CSS layout (flex, hit areas, overflow) belongs in Playwright, not stylesheet source reads.
