package validate

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// frozenScopeDriftSchemaSDL mirrors the spec-driven starter's structural shape:
// a SpecLike interface + one concrete spec type + an EffortNote whose
// `Spec Set (Frozen)` H2 section captures outbound wikilinks as `frozenSpecs`.
const frozenScopeDriftSchemaSDL = `
enum SpecStatus { proposed active superseded archived }
enum EffortStatus { planned active complete archived }

interface SpecLike {
  id: String!
  specStatus: SpecStatus!
}

type Spec implements SpecLike @node(paths: ["specs/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "SPEC")
  specStatus: SpecStatus! @field(source: "spec-status")
  summary: String!
  lastUpdated: Date @field(source: "last-updated")
}

type FrozenSpecSetSection implements Section {
  frozenSpecs: [SpecLike!] @neighbors(direction: OUTBOUND, type: "SpecLike", scope: SUBTREE)
}

type EffortNote @node(paths: ["efforts/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "EFF")
  status: EffortStatus!
  createdAt: DateTime! @field(source: "created-at")
  summary: String!
  specSetFrozen: FrozenSpecSetSection
    @contains(level: H2, heading: "Spec Set (Frozen)", required: true)
}
`

// frozenScopeDriftFixture stitches together a typical effort + spec pair.
// effortStatus, effortCreatedAt, and specLastUpdated drive the per-test
// scenario; everything else is boilerplate the parser needs.
func frozenScopeDriftFixture(effortStatus, effortCreatedAt, specLastUpdated string) map[string]string {
	return map[string]string{
		".rhizome/ontology/schema.graphql": frozenScopeDriftSchemaSDL,
		"specs/alpha.md":                   "---\ntype: Spec\nid: SPEC-0001\nspec-status: active\nsummary: Alpha\nlast-updated: " + specLastUpdated + "\naliases:\n  - SPEC-0001\n---\n# Alpha\n",
		"efforts/eff.md":                   "---\ntype: EffortNote\nid: EFF-0001\nstatus: " + effortStatus + "\ncreated-at: " + effortCreatedAt + "\nsummary: Eff\naliases:\n  - EFF-0001\n---\n# Eff\n\n## Spec Set (Frozen)\n\n- [[specs/alpha.md|SPEC-0001]]\n",
	}
}

func runFrozenScopeDriftFixture(t *testing.T, files map[string]string) (CheckResult, *ontology.Runtime) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, files))

	store := openTestStore(t, root)
	vaultDef := obsidian.VaultDefinition{Root: root, Path: root, Links: obsidian.LinkTypeBoth, Includes: []string{"**/*.md", "**/*.html"}}
	_, err := testNoteMetadata(t).EnsureIndexed(context.Background(), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	runtime, err := ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadata(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	runCtx := RunContext{
		VaultDef:     vaultDef,
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    100,
	}
	return RunFrozenScopeDriftWithRuntime(context.Background(), runCtx, runtime), runtime
}

func TestRunFrozenScopeDrift_SpecEditedAfterFreezeFlagsIssue(t *testing.T) {
	files := frozenScopeDriftFixture("active", "2026-04-01T10:00:00Z", "2026-04-15")
	result, _ := runFrozenScopeDriftFixture(t, files)

	require.Equal(t, 1, result.IssueCount, "expected one drift issue")
	require.Len(t, result.Issues, 1)
	issue := result.Issues[0]
	require.Equal(t, "frozen_scope_drift", issue.Code)
	require.Equal(t, "efforts/eff.md", issue.Path)
	require.Equal(t, "specs/alpha.md", issue.Target)
	require.Equal(t, "SPEC-0001", issue.Source)

	var data FrozenScopeDriftData
	require.NoError(t, json.Unmarshal(issue.Data, &data))
	assert.Equal(t, "EFF-0001", data.EffortID)
	assert.Equal(t, "active", data.EffortStatus)
	assert.Equal(t, "SPEC-0001", data.SpecID)
}

func TestRunFrozenScopeDrift_SpecEditedBeforeFreezeIsClean(t *testing.T) {
	// Spec last-updated 2026-03-15, effort created 2026-04-01 → no drift.
	files := frozenScopeDriftFixture("active", "2026-04-01T10:00:00Z", "2026-03-15")
	result, _ := runFrozenScopeDriftFixture(t, files)

	require.Equal(t, 0, result.IssueCount)
	require.True(t, result.OK)
}

func TestRunFrozenScopeDrift_SkipsCompleteEfforts(t *testing.T) {
	// Spec edited after the effort was created, but the effort is already
	// complete → SPEC-0051 immutability says drift no longer applies.
	files := frozenScopeDriftFixture("complete", "2026-04-01T10:00:00Z", "2026-04-15")
	result, _ := runFrozenScopeDriftFixture(t, files)

	require.Equal(t, 0, result.IssueCount, "complete efforts must be skipped")
	require.True(t, result.OK)
}

func TestRunFrozenScopeDrift_SkipsArchivedEfforts(t *testing.T) {
	files := frozenScopeDriftFixture("archived", "2026-04-01T10:00:00Z", "2026-04-15")
	result, _ := runFrozenScopeDriftFixture(t, files)

	require.Equal(t, 0, result.IssueCount, "archived efforts must be skipped")
}

func TestRunFrozenScopeDrift_NoEffortsIsSkippedSummary(t *testing.T) {
	files := map[string]string{
		".rhizome/ontology/schema.graphql": frozenScopeDriftSchemaSDL,
		"specs/alpha.md":                   "---\ntype: Spec\nid: SPEC-0001\nspec-status: active\nsummary: Alpha\nlast-updated: 2026-04-15\naliases:\n  - SPEC-0001\n---\n# Alpha\n",
	}
	result, _ := runFrozenScopeDriftFixture(t, files)

	require.Equal(t, 0, result.IssueCount)
	require.Contains(t, result.Summary, "no efforts to check")
}

func TestRunFrozenScopeDrift_NilRuntimeSkips(t *testing.T) {
	runCtx := RunContext{MaxIssues: 100}
	result := RunFrozenScopeDriftWithRuntime(context.Background(), runCtx, nil)
	require.True(t, result.Skipped)
	require.Contains(t, result.Summary, "no ontology schema")
}

func TestRunFrozenScopeDrift_UnparseableTimestampsBecomeNotes(t *testing.T) {
	// Created-at that the loose parser cannot read should NOT crash; it
	// should land in result.Notes and be skipped quietly.
	files := frozenScopeDriftFixture("active", "not-a-real-timestamp", "2026-04-15")
	result, _ := runFrozenScopeDriftFixture(t, files)

	require.Equal(t, 0, result.IssueCount)
	require.NotEmpty(t, result.Notes)
}

func TestRunFrozenScopeDrift_AcknowledgmentInDeviationsSkipsPair(t *testing.T) {
	// Drift would normally fire (active effort, spec edited after createdAt),
	// but the effort body's Deviations section contains both the marker and
	// the spec id, so the check skips the pair.
	files := frozenScopeDriftFixture("active", "2026-04-01T10:00:00Z", "2026-04-15")
	files["efforts/eff.md"] = `---
type: EffortNote
id: EFF-0001
status: active
created-at: 2026-04-01T10:00:00Z
summary: Eff
aliases:
  - EFF-0001
---
# Eff

## Spec Set (Frozen)

- [[specs/alpha.md|SPEC-0001]]

## Deviations

- (frozen-scope-drift acknowledged via EFF-0099) SPEC-0001 was refreshed; no change to delivery contract.
`
	result, _ := runFrozenScopeDriftFixture(t, files)

	require.Equal(t, 0, result.IssueCount, "acknowledgement marker + spec id should suppress the pair")
	require.True(t, result.OK)
}

func TestRunFrozenScopeDrift_AcknowledgmentRequiresBothMarkerAndSpecID(t *testing.T) {
	// Marker present but spec id absent → still flagged.
	files := frozenScopeDriftFixture("active", "2026-04-01T10:00:00Z", "2026-04-15")
	files["efforts/eff.md"] = `---
type: EffortNote
id: EFF-0001
status: active
created-at: 2026-04-01T10:00:00Z
summary: Eff
aliases:
  - EFF-0001
---
# Eff

## Spec Set (Frozen)

- [[specs/alpha.md|SPEC-0001]]

## Deviations

- (frozen-scope-drift acknowledged via EFF-0099) some other spec was refreshed.
`
	result, _ := runFrozenScopeDriftFixture(t, files)

	require.Equal(t, 1, result.IssueCount, "marker without the actual spec id must NOT skip")
}

func TestRunFrozenScopeDrift_AcknowledgmentMarkerIsCaseInsensitive(t *testing.T) {
	files := frozenScopeDriftFixture("active", "2026-04-01T10:00:00Z", "2026-04-15")
	files["efforts/eff.md"] = `---
type: EffortNote
id: EFF-0001
status: active
created-at: 2026-04-01T10:00:00Z
summary: Eff
aliases:
  - EFF-0001
---
# Eff

## Spec Set (Frozen)

- [[specs/alpha.md|SPEC-0001]]

## Deviations

- (FROZEN-SCOPE-DRIFT ACKNOWLEDGED via EFF-0099) SPEC-0001 was refreshed.
`
	result, _ := runFrozenScopeDriftFixture(t, files)

	require.Equal(t, 0, result.IssueCount)
}

// The acknowledgement counts only when the marker and the drifting spec's
// exact id both sit inside the Deviations section.
func TestRunFrozenScopeDrift_AcknowledgmentScope(t *testing.T) {
	const ack = "- (frozen-scope-drift acknowledged via EFF-0099) SPEC-0001 was refreshed.\n"
	tests := []struct {
		name      string
		specID    string
		body      string
		wantIssue int
	}{
		{"inside deviations", "SPEC-0001", "## Deviations\n\n" + ack, 0},
		{"before deviations", "SPEC-0001", "## Scope\n\n" + ack + "\n## Deviations\n\n- none\n", 1},
		{"below the next H2", "SPEC-0001", "## Deviations\n\n- none\n\n## Compounding Follow-ups\n\n" + ack, 1},
		{"another spec id", "SPEC-0001", "## Deviations\n\n- (frozen-scope-drift acknowledged via EFF-0099) SPEC-0099 was refreshed.\n", 1},
		{"spec id without marker", "SPEC-0001", "## Deviations\n\n- SPEC-0001 was refreshed.\n", 1},
		{"drifting spec has no id", "", "## Deviations\n\n- (frozen-scope-drift acknowledged via EFF-0099) refreshed.\n", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := frozenScopeDriftFixture("active", "2026-04-01T10:00:00Z", "2026-04-15")
			if tt.specID == "" {
				files["specs/alpha.md"] = "---\ntype: Spec\nspec-status: active\nsummary: Alpha\nlast-updated: 2026-04-15\n---\n# Alpha\n"
			}
			files["efforts/eff.md"] = "---\ntype: EffortNote\nid: EFF-0001\nstatus: active\ncreated-at: 2026-04-01T10:00:00Z\nsummary: Eff\naliases:\n  - EFF-0001\n---\n# Eff\n\n## Spec Set (Frozen)\n\n- [[specs/alpha.md|SPEC-0001]]\n\n" + tt.body
			result, _ := runFrozenScopeDriftFixture(t, files)

			require.Empty(t, result.Error)
			require.Equal(t, tt.wantIssue, result.IssueCount)
			if tt.wantIssue > 0 {
				require.Equal(t, tt.specID, result.Issues[0].Source)
			}
		})
	}
}

// frozenScopeDriftWorkspaceSDL adds the starter's HTML effort form: frozen
// specs come from `governing-specs` metadata and Deviations live in the
// linked Markdown work log.
const frozenScopeDriftWorkspaceSDL = frozenScopeDriftSchemaSDL + `
type EffortMaterial @node(paths: ["efforts/*/work-log.md"]) { summary: String }

type EffortWorkspace @node(paths: ["efforts/*/*-effort.html"]) {
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "EFF")
  status: EffortStatus!
  createdAt: DateTime! @field(source: "created-at")
  summary: String!
  workLog: EffortMaterial! @link(source: "work-log", includeBodyLinks: false, includeBacklinks: false)
  governingSpecs: [SpecLike!]! @link(source: "governing-specs", includeBodyLinks: false, includeBacklinks: false)
}
`

func frozenScopeDriftWorkspaceFixture(workLogBody string) map[string]string {
	return map[string]string{
		".rhizome/config.yml":                    "notes:\n  includes: [\"**/*.md\", \"**/*.html\"]\n",
		".rhizome/ontology/schema.graphql":       frozenScopeDriftWorkspaceSDL,
		"specs/alpha.md":                         "---\ntype: Spec\nid: SPEC-0001\nspec-status: active\nsummary: Alpha\nlast-updated: 2026-04-15\naliases:\n  - SPEC-0001\n---\n# Alpha\n",
		"efforts/w/2026-04-01-10-00-effort.html": `<html><head><title>W</title><script id="rhizome-metadata" type="application/json">{"id":"EFF-0002","aliases":["EFF-0002"],"summary":"W","status":"active","created-at":"2026-04-01T10:00:00Z","work-log":"efforts/w/work-log.md","governing-specs":["specs/alpha.md"]}</script></head><body>W</body></html>`,
		"efforts/w/work-log.md":                  "# Work log\n\n" + workLogBody,
	}
}

func TestRunFrozenScopeDrift_WorkspaceGoverningSpecEditedAfterFreezeFlagsIssue(t *testing.T) {
	result, _ := runFrozenScopeDriftFixture(t, frozenScopeDriftWorkspaceFixture("## Deviations\n\n- none\n"))

	require.Empty(t, result.Error)
	require.Equal(t, 1, result.IssueCount)
	require.Equal(t, "efforts/w/2026-04-01-10-00-effort.html", result.Issues[0].Path)
	require.Equal(t, "specs/alpha.md", result.Issues[0].Target)
}

func TestRunFrozenScopeDrift_WorkspaceAcknowledgmentInWorkLogSkipsPair(t *testing.T) {
	ack := "## Deviations\n\n- (frozen-scope-drift acknowledged via EFF-0099) SPEC-0001 was refreshed.\n"
	result, _ := runFrozenScopeDriftFixture(t, frozenScopeDriftWorkspaceFixture(ack))

	require.Empty(t, result.Error)
	require.Equal(t, 0, result.IssueCount)
}

func TestRunFrozenScopeDrift_SameDayDateOnlyEditIsNoteNotIssue(t *testing.T) {
	// A date-only last-updated on the creation day cannot be ordered against
	// the creation time, so it is reported as a note without failing.
	result, _ := runFrozenScopeDriftFixture(t, frozenScopeDriftFixture("active", "2026-04-15T10:00:00Z", "2026-04-15"))

	require.Equal(t, 0, result.IssueCount)
	require.Len(t, result.Notes, 1)
	require.Contains(t, result.Notes[0], "creation day")
}
