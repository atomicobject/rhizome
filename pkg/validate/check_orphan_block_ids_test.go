package validate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunOrphanBlockIDs covers SPEC-0023.US4 acceptance criteria: classify
// each anchor against inbound external references, treat broken-fragment
// near-matches as soft holds, and never auto-remove confirmed near-match
// candidates.
//
// Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us4]]
func TestRunOrphanBlockIDs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field
}

type Stories implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: Stories @contains(level: H2, heading: "Stories")
}
`,
		"specs/owner.md": `---
type: Spec
summary: Owner
aliases:
  - SPEC-0006
---

# Owner

## Stories

### Story Kept
id:: SPEC-1.US1
^story-kept

### Story Kept By Markdown
id:: SPEC-1.US1A
^story-kept-md

### Story Orphan
id:: SPEC-1.US2
^story-orphan

### Story Near Match
id:: SPEC-1.US3
^story-near

### Story Inline Property Anchor
id:: SPEC-1.US4
custom:: ^inline-property-anchor

### Story Kept By Alias
id:: SPEC-1.US5
^story-kept-alias
`,
		"refs/external.md": `---
type: Other
---
# External

- valid resolved ref: [[owner#^story-kept]]
- valid resolved markdown ref: [owner story](../specs/owner.md#^story-kept-md)
- valid display-alias ref: [[owner#^story-kept-alias|SPEC-0006.US5]]
- typo'd near-match refs: [[owner#^story-neaa]] and [[owner#^story-nea]]
`,
		"loose.md": `# Loose

^loose-orphan
`,
	}))

	store := openTestStore(t, root)
	t.Cleanup(func() { _ = store.Close() })

	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	runCtx := RunContext{
		VaultDef:     vaultDef,
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    100,
	}

	result := RunOrphanBlockIDs(context.Background(), runCtx)
	require.True(t, result.OK, "check itself should run cleanly: %+v", result)

	classByID := map[string]OrphanBlockIDClass{}
	nearMatchesByID := map[string][]string{}
	for _, issue := range result.Issues {
		assert.Equal(t, "orphan_block_id", issue.Code)
		var data OrphanBlockIDData
		require.NoError(t, json.Unmarshal(issue.Data, &data))
		classByID[data.BlockID] = data.Class
		if data.BlockID == "story-near" {
			var raw map[string]any
			require.NoError(t, json.Unmarshal(issue.Data, &raw))
			for _, candidate := range raw["nearMatchCandidates"].([]any) {
				nearMatchesByID[data.BlockID] = append(nearMatchesByID[data.BlockID], candidate.(string))
			}
		}
	}
	assert.NotContains(t, classByID, "story-kept", "anchors with resolved external references must not be flagged")
	assert.NotContains(t, classByID, "story-kept-md", "anchors with resolved external markdown references must not be flagged")
	assert.NotContains(t, classByID, "story-kept-alias", "anchors with display-alias references must not be flagged")
	assert.NotContains(t, classByID, "inline-property-anchor", "inline property anchors are not removable standalone anchors")
	assert.Equal(t, OrphanClassRemovable, classByID["story-orphan"], "anchors with no external reference must be removable")
	assert.Equal(t, OrphanClassFuzzyNearMatch, classByID["story-near"], "anchors with broken-fragment near-match inbound must be soft-hold")
	assert.Equal(t, []string{"story-nea", "story-neaa"}, nearMatchesByID["story-near"], "all near-match candidates must be sorted deterministically")

	// Fix actions: removable → safe; near-match → confirm; never both for the kept anchor.
	fixesByID := map[string]FixAction{}
	for _, fix := range result.Fixes {
		assert.Equal(t, FixKindRemoveBlockID, fix.Kind)
		blockID := fix.Edits[0].BlockID
		fixesByID[blockID] = fix
	}
	assert.Equal(t, FixSafetySafe, fixesByID["story-orphan"].Safety, "removable orphans get safe-apply fixes")
	assert.Equal(t, FixSafetyConfirm, fixesByID["story-near"].Safety, "near-match orphans must require confirmation, never auto-apply")
	assert.Equal(t, FixSafetySafe, fixesByID["loose-orphan"].Safety, "fallback-scanned anchors still get exact-line removal fixes")
	assert.NotContains(t, fixesByID, "story-kept", "kept anchors must not have a removal fix")
	assert.NotContains(t, fixesByID, "story-kept-md", "markdown-kept anchors must not have a removal fix")
	assert.NotContains(t, fixesByID, "story-kept-alias", "display-alias-kept anchors must not have a removal fix")
}

// TestRunOrphanBlockIDs_StripsDerivableIdentifierLines covers SPEC-0023.US8.AC5:
// orphan cleanup extends to authored `id::` lines on derivable-identity fields.
// Cited identifier-backed lines remain
// preserved; required-identity identifier-backed lines (e.g. on a top-level
// note) are still preserved unconditionally.
func TestRunOrphanBlockIDs_StripsDerivableIdentifierLines(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Milestone implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "MS", populate: ON_LINK)
}

type MilestonesSection implements Section {
  milestones: [Milestone!] @contains(level: H5)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US", populate: ON_LINK)
  milestones: MilestonesSection @contains(level: H4, heading: "Milestones")
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "SPEC")
  userStoriesSection: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`,
		"specs/owner.md": `---
type: Spec
id: SPEC-0042
summary: Owner
aliases:
  - SPEC-0042
---

# Owner

## User Stories

### Story Cited
id:: ^SPEC-0042-US1

#### Milestones

##### Milestone cited externally
id:: ^SPEC-0042-US1-MS1

##### Milestone uncited
id:: ^SPEC-0042-US1-MS2

### Story Uncited
id:: ^SPEC-0042-US2
`,
		"refs/external.md": `---
type: Other
---
- valid resolved milestone ref: [[owner#^SPEC-0042-US1-MS1]]
- valid resolved story ref: [[owner#^SPEC-0042-US1]]
`,
	}))

	store := openTestStore(t, root)
	t.Cleanup(func() { _ = store.Close() })

	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	runCtx := RunContext{
		VaultDef:     vaultDef,
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    100,
	}

	result := RunOrphanBlockIDs(context.Background(), runCtx)
	require.True(t, result.OK)

	classByID := map[string]OrphanBlockIDClass{}
	for _, issue := range result.Issues {
		var data OrphanBlockIDData
		require.NoError(t, json.Unmarshal(issue.Data, &data))
		classByID[data.BlockID] = data.Class
	}
	assert.NotContains(t, classByID, "SPEC-0042-US1", "cited story id is preserved")
	assert.NotContains(t, classByID, "SPEC-0042-US1-MS1", "cited milestone id is preserved")
	assert.Equal(t, OrphanClassRemovable, classByID["SPEC-0042-US1-MS2"], "uncited derivable-identity milestone line is removable")
	assert.Equal(t, OrphanClassRemovable, classByID["SPEC-0042-US2"], "uncited derivable-identity story line is removable")

	plan := &FixPlan{Actions: result.Fixes}
	_, err := ApplyFixPlan(context.Background(), runCtx, plan, Options{Fix: true, NonInteractive: true})
	require.NoError(t, err)

	updated, err := os.ReadFile(filepath.Join(root, "specs/owner.md"))
	require.NoError(t, err)
	got := string(updated)
	assert.Contains(t, got, "id:: ^SPEC-0042-US1\n", "cited story line preserved")
	assert.Contains(t, got, "id:: ^SPEC-0042-US1-MS1\n", "cited milestone line preserved")
	assert.NotContains(t, got, "id:: ^SPEC-0042-US1-MS2", "uncited milestone line stripped")
	assert.NotContains(t, got, "id:: ^SPEC-0042-US2", "uncited story line stripped")

	result2 := RunOrphanBlockIDs(context.Background(), runCtx)
	assert.Empty(t, result2.Fixes, "second cleanup run is idempotent")
}

// TestApplyRemoveBlockIDEdit_Idempotent guards that running the cleanup twice
// against the same anchor leaves source identical the second time.
func TestApplyRemoveBlockIDEdit_Idempotent(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field
}

type Stories implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: Stories @contains(level: H2, heading: "Stories")
}
`,
		"specs/owner.md": `---
type: Spec
summary: Owner
---

# Owner

## Stories

### Story Orphan
id:: SPEC-1.US1
^story-orphan
`,
	}))

	store := openTestStore(t, root)
	t.Cleanup(func() { _ = store.Close() })

	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	runCtx := RunContext{
		VaultDef:     vaultDef,
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    100,
	}

	result := RunOrphanBlockIDs(context.Background(), runCtx)
	require.NotEmpty(t, result.Fixes)
	plan := &FixPlan{Actions: result.Fixes}
	_, err := ApplyFixPlan(context.Background(), runCtx, plan, Options{Fix: true, NonInteractive: true})
	require.NoError(t, err)

	updated, err := os.ReadFile(filepath.Join(root, "specs/owner.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(updated), "^story-orphan", "first apply must remove the anchor")

	// Idempotence at the workflow level: re-running the check after apply
	// produces no fix actions. (Re-applying a stale plan against drifted
	// source is intentionally an error condition; callers should re-run the
	// check rather than replay stale plans.)
	result2 := RunOrphanBlockIDs(context.Background(), runCtx)
	assert.Empty(t, result2.Fixes, "second cleanup run must produce no fixes for already-cleaned anchors")
}

func TestRemoveExactBlockIDLine_DoesNotRemovePrefixMatch(t *testing.T) {
	content := "id:: SPEC-1.US1\n^spec-1-us1-ac1\n"

	updated, changed := removeExactBlockIDLine(content, "spec-1-us1")

	assert.False(t, changed)
	assert.Equal(t, content, updated)
}

func TestRemoveExactBlockIDLine_CollapsesOneSurroundingBlankLine(t *testing.T) {
	content := "status:: ready\n\n^spec-1-us1\n\n#### Acceptance Criteria\n"

	updated, changed := removeExactBlockIDLine(content, "spec-1-us1")

	require.True(t, changed)
	assert.Equal(t, "status:: ready\n\n#### Acceptance Criteria\n", updated)
}
