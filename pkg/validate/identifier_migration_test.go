package validate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRunIdentifiersDetectsSequentialToDateTimeMigrationWithoutGit(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type EffortNote @node(paths: ["docs/efforts/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
}
`, map[string]string{
		"docs/efforts/2026-08-06-12-20-second.md": "---\nid: EFF-0002\naliases: [EFF-0002]\n---\n",
		"docs/efforts/2026-08-06-12-20-first.md":  "---\nid: EFF-0001\naliases: [EFF-0001]\n---\n",
	})
	runCtx := RunContext{
		VaultDef:     obsidian.VaultDefinition{Path: root},
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    20,
	}
	result := RunIdentifiersWithRuntime(context.Background(), runCtx, runtime)
	require.Empty(t, result.Error)
	require.Equal(t, 2, result.IssueCount)
	require.Len(t, result.Fixes, 1)
	require.Equal(t, "migrate_identifier_strategy", result.Fixes[0].Kind)
	require.Equal(t, FixSafetyConfirm, result.Fixes[0].Safety)
	require.Len(t, result.Fixes[0].IssueKeys, 2)
	require.Zero(t, result.identifierReconciliation.Diagnostics.Git.Commands)
	require.Zero(t, result.identifierReconciliation.Diagnostics.Timings.GitProvenance)
	require.NotNil(t, result.identifierRepair, result.Notes)

	want := map[string]string{
		"EFF-0001": "EFF-2026-08-06-12-20",
		"EFF-0002": "EFF-2026-08-06-12-20-2",
	}
	for _, issue := range result.Issues {
		require.Equal(t, issueCodeIdentifierStrategyMigrationRequired, issue.Code)
		var data IdentifierStrategyMigrationData
		require.NoError(t, json.Unmarshal(issue.Data, &data))
		require.Equal(t, want[data.OldIdentifier], data.NewIdentifier)
		require.True(t, data.Materializable)
		require.Equal(t, 2, data.MemberCount)
		require.NotEmpty(t, data.PlanFingerprint)
		require.Equal(t, &IssueVariant{Key: data.TargetPool, Label: "EFF- (SEQUENTIAL → DATETIME)"}, issue.Variant)
	}
}

func TestRunIdentifiersMigrationIgnoresIdentifierGatedFreeformSelectorCandidate(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type EffortNote @node(paths: ["docs/efforts/**/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
}
type AlternateEffortNote @node(paths: ["docs/efforts/**/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
}
`, map[string]string{
		"docs/efforts/2026-08-06-12-20-first.md": "---\ntype: EffortNote\nid: EFF-0001\naliases: [EFF-0001]\n---\n",
		"docs/efforts/legacy/plan.md":            "# Freeform plan\n",
	})
	runCtx := RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20}

	result := RunIdentifiersWithRuntime(context.Background(), runCtx, runtime)

	require.Empty(t, result.Error)
	require.Equal(t, 1, result.IssueCount)
	require.Len(t, result.Fixes, 1)
	require.Equal(t, "migrate_identifier_strategy", result.Fixes[0].Kind)
	require.Equal(t, FixSafetyConfirm, result.Fixes[0].Safety)
	require.NotNil(t, result.identifierRepair, result.Notes)
	var data IdentifierStrategyMigrationData
	require.NoError(t, json.Unmarshal(result.Issues[0].Data, &data))
	require.Equal(t, "EFF-0001", data.OldIdentifier)
	require.Equal(t, "EFF-2026-08-06-12-20", data.NewIdentifier)
}

func TestRunIdentifiersDetectsDateTimeToSequentialDenseMigration(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type EffortNote @node(paths: ["docs/efforts/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: SEQUENTIAL, prefix: "EFF", pad: 4)
  aliases: [String!] @field
}
`, map[string]string{
		"docs/efforts/2026-08-06-12-21-third.md":  "---\nid: EFF-2026-08-06-12-21\naliases: [EFF-2026-08-06-12-21]\n---\n",
		"docs/efforts/2026-08-06-12-20-second.md": "---\nid: EFF-2026-08-06-12-20-2\naliases: [EFF-2026-08-06-12-20-2]\n---\n",
		"docs/efforts/2026-08-06-12-20-first.md":  "---\nid: EFF-2026-08-06-12-20\naliases: [EFF-2026-08-06-12-20]\n---\n",
	})
	runCtx := RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20}

	result := RunIdentifiersWithRuntime(context.Background(), runCtx, runtime)
	require.Empty(t, result.Error)
	require.Equal(t, 3, result.IssueCount)
	require.Len(t, result.Fixes, 1)
	require.Equal(t, FixSafetyConfirm, result.Fixes[0].Safety)
	require.NotNil(t, result.identifierRepair, result.Notes)

	want := map[string]string{
		"EFF-2026-08-06-12-20":   "EFF-0001",
		"EFF-2026-08-06-12-20-2": "EFF-0002",
		"EFF-2026-08-06-12-21":   "EFF-0003",
	}
	for _, issue := range result.Issues {
		var data IdentifierStrategyMigrationData
		require.NoError(t, json.Unmarshal(issue.Data, &data))
		require.Equal(t, want[data.OldIdentifier], data.NewIdentifier)
	}
}

func TestRunIdentifiersBlocksExactMigrationTargetClaimedByHistoricalAlias(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type EffortNote @node(paths: ["docs/efforts/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
}
`, map[string]string{
		"docs/efforts/2026-08-06-12-20-first.md": "---\nid: EFF-0001\naliases: [EFF-0001]\n---\n",
		"docs/efforts/2026-08-06-12-21-other.md": "---\nid: EFF-0002\naliases: [EFF-0002, EFF-2026-08-06-12-20]\n---\n",
	})
	runCtx := RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20}

	result := RunIdentifiersWithRuntime(context.Background(), runCtx, runtime)
	require.Empty(t, result.Error)
	require.NotZero(t, result.IssueCount)
	var blocked *FixAction
	for index := range result.Fixes {
		if result.Fixes[index].Kind == "review_identifier_strategy_migration" {
			blocked = &result.Fixes[index]
		}
	}
	require.NotNil(t, blocked)
	require.Equal(t, FixSafetyAgent, blocked.Safety)
	require.Nil(t, result.identifierRepair)
}

func TestIdentifierStrategyMigrationAppliesWholePoolAndConverges(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		files  map[string]string
		want   map[string]string
		extra  map[string]string
		exact  map[string]string
	}{
		{
			name: "sequential to datetime",
			schema: `
type EffortNote @node(paths: ["docs/efforts/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
  stories: Stories @contains(level: H2, heading: "Stories")
}
type Stories implements Section {
  items: [Story!] @contains(level: H3)
}
type Story implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
}
type Consumer @node(paths: ["refs/*.md"], propertyCase: AS_DEFINED) {
  effort: EffortNote @link
}
`,
			files: map[string]string{
				"docs/efforts/2026-08-06-12-20-first.md":  "---\nid: EFF-0001\naliases: [EFF-0001]\n---\n## Stories\n### One\nid:: ^EFF-0001-US1\n",
				"docs/efforts/2026-08-06-12-20-second.md": "---\nid: EFF-0002\naliases: [EFF-0002]\n---\n",
				"refs/inbound.md":                         "---\neffort: EFF-0001\n---\n",
			},
			want: map[string]string{
				"docs/efforts/2026-08-06-12-20-first.md":  "EFF-2026-08-06-12-20",
				"docs/efforts/2026-08-06-12-20-second.md": "EFF-2026-08-06-12-20-2",
			},
			extra: map[string]string{
				"refs/inbound.md": "effort: EFF-2026-08-06-12-20",
			},
			// The authored caret child locator is matched against its semantic
			// projection and rewritten in place; every other byte is unchanged.
			exact: map[string]string{
				"docs/efforts/2026-08-06-12-20-first.md": "---\nid: EFF-2026-08-06-12-20\naliases: [EFF-2026-08-06-12-20]\n---\n## Stories\n### One\nid:: ^EFF-2026-08-06-12-20-US1\n",
			},
		},
		{
			name: "datetime to sequential",
			schema: `
type EffortNote @node(paths: ["docs/efforts/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: SEQUENTIAL, prefix: "EFF", pad: 4)
  aliases: [String!] @field
}
`,
			files: map[string]string{
				"docs/efforts/2026-08-06-12-20-first.md":  "---\nid: EFF-2026-08-06-12-20\naliases: [EFF-2026-08-06-12-20]\n---\n",
				"docs/efforts/2026-08-06-12-20-second.md": "---\nid: EFF-2026-08-06-12-20-2\naliases: [EFF-2026-08-06-12-20-2]\n---\n",
			},
			want: map[string]string{
				"docs/efforts/2026-08-06-12-20-first.md":  "EFF-0001",
				"docs/efforts/2026-08-06-12-20-second.md": "EFF-0002",
			},
		},
		{
			name: "mixed sequential and datetime converges",
			schema: `
type EffortNote @node(paths: ["docs/efforts/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
}
`,
			files: map[string]string{
				"docs/efforts/2026-08-06-12-20-first.md":  "---\nid: EFF-0001\naliases: [EFF-0001]\n---\n",
				"docs/efforts/2026-08-06-12-21-second.md": "---\nid: EFF-2026-08-06-12-21\naliases: [EFF-2026-08-06-12-21]\n---\n",
			},
			want: map[string]string{
				"docs/efforts/2026-08-06-12-20-first.md":  "EFF-2026-08-06-12-20",
				"docs/efforts/2026-08-06-12-21-second.md": "EFF-2026-08-06-12-21",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, runtime := identifierCheckRuntime(t, test.schema, test.files)
			runCtx := RunContext{
				VaultDef:     obsidian.VaultDefinition{Path: root},
				VaultPath:    root,
				NoteReader:   &obsidian.Note{},
				NoteMetadata: testNoteMetadata(t),
				MaxIssues:    20,
			}
			result := RunIdentifiersWithRuntime(context.Background(), runCtx, runtime)
			require.Empty(t, result.Error)
			require.NotNil(t, result.identifierRepair, result.Notes)
			plan, err := BuildRepairPlan(context.Background(), runCtx, []CheckResult{result})
			require.NoError(t, err)
			require.NotNil(t, plan)
			execution, err := ApplyIdentifierRepairPlan(context.Background(), runCtx, plan, result.identifierRepair.Assembly, result.identifierRepair.Bindings, Options{
				Fix: true, Confirm: func(string) (bool, error) { return true, nil },
				PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: runCtx},
			})
			require.NoError(t, err)
			require.NotNil(t, execution)
			require.NotEmpty(t, execution.Applied, execution)
			for path, identifier := range test.want {
				contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
				require.NoError(t, err)
				require.Contains(t, string(contents), "id: "+identifier)
				require.Contains(t, string(contents), "aliases: ["+identifier+"]")
			}
			for path, expected := range test.extra {
				contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
				require.NoError(t, err)
				require.Contains(t, string(contents), expected)
			}
			for path, expected := range test.exact {
				contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
				require.NoError(t, err)
				require.Equal(t, expected, string(contents))
			}
			refreshed, cleanup, err := ontology.EnsureFreshRuntime(context.Background(), testNoteMetadata(t), runCtx.VaultDef, runCtx.NoteReader)
			if cleanup != nil {
				defer cleanup()
			}
			require.NoError(t, err)
			postcheck := RunIdentifiersWithRuntime(context.Background(), runCtx, refreshed)
			require.Empty(t, postcheck.Error)
			require.Zero(t, postcheck.IssueCount, postcheck.Issues)
		})
	}
}

func TestIdentifierStrategyMigrationFailsClosedAtRuntimeRecoveryBoundary(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		files  map[string]string
	}{
		{
			name: "missing preferred member",
			schema: `
type EffortNote @node(paths: ["docs/efforts/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
}
`,
			files: map[string]string{
				"docs/efforts/2026-08-06-12-20-first.md":  "---\nid: EFF-0001\naliases: [EFF-0001]\n---\n",
				"docs/efforts/2026-08-06-12-21-second.md": "---\ntype: EffortNote\naliases: []\n---\n",
			},
		},
		{
			name: "ambiguous selector candidates",
			schema: `
type EffortNote @node(paths: ["docs/efforts/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
}
type AlternateEffortNote @node(paths: ["docs/efforts/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
}
`,
			files: map[string]string{
				"docs/efforts/2026-08-06-12-20-first.md": "---\nid: EFF-0001\naliases: [EFF-0001]\n---\n",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, runtime := identifierCheckRuntime(t, test.schema, test.files)
			result := RunIdentifiersWithRuntime(context.Background(), RunContext{
				VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20,
			}, runtime)
			require.Empty(t, result.Error)
			require.Nil(t, result.identifierRepair)
			var blocked bool
			for _, issue := range result.Issues {
				blocked = blocked || issue.Code == issueCodeIdentifierStrategyMigrationBlocked
			}
			require.True(t, blocked, result.Issues)
		})
	}
}

func TestIdentifierStrategyMigrationKeepsCompleteActionAuthorityBeyondMaxIssues(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type EffortNote @node(paths: ["docs/efforts/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
}
`, map[string]string{
		"docs/efforts/2026-08-06-12-20-first.md":  "---\nid: EFF-0001\naliases: [EFF-0001]\n---\n",
		"docs/efforts/2026-08-06-12-21-second.md": "---\nid: EFF-0002\naliases: [EFF-0002]\n---\n",
		"docs/efforts/2026-08-06-12-22-third.md":  "---\nid: EFF-0003\naliases: [EFF-0003]\n---\n",
	})
	runCtx := RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 1}
	result := RunIdentifiersWithRuntime(context.Background(), runCtx, runtime)
	require.Equal(t, 3, result.IssueCount)
	require.Len(t, result.Issues, 3, "the check preserves complete evidence until centralized finalization")
	require.Len(t, result.Fixes, 1)
	require.Len(t, result.Fixes[0].IssueKeys, 3)
	require.NotNil(t, result.identifierRepair)

	finalized := finalizeCheckResult(CheckIdentifiers, result, time.Millisecond, 1)
	require.Equal(t, 3, finalized.IssueCount)
	require.Len(t, finalized.Issues, 1)
	require.Len(t, finalized.Fixes[0].IssueKeys, 3)
	require.NotNil(t, finalized.identifierRepair)
}

func TestIdentifierStrategyMigrationRequiresHistoricalAuthorityForCompleteEffort(t *testing.T) {
	const path = "docs/efforts/2026-08-06-12-20-complete.md"
	root, runtime := identifierCheckRuntime(t, `
enum EffortStatus { active complete archived }
type EffortNote @node(paths: ["docs/efforts/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  aliases: [String!] @field
  status: EffortStatus! @field
}
	`, map[string]string{
		path: "---\ntype: EffortNote\nid: EFF-0001\naliases: [EFF-0001]\nstatus: complete\n---\n",
	})
	runCtx := RunContext{
		VaultDef:     obsidian.VaultDefinition{Path: root},
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    20,
	}
	result := RunIdentifiersWithRuntime(context.Background(), runCtx, runtime)
	require.NotNil(t, result.identifierRepair, result.Notes)
	plan, err := BuildRepairPlan(context.Background(), runCtx, []CheckResult{result})
	require.NoError(t, err)

	skipped, err := ApplyIdentifierRepairPlan(context.Background(), runCtx, plan, result.identifierRepair.Assembly, result.identifierRepair.Bindings, Options{
		Fix: true, Confirm: func(string) (bool, error) { return true, nil },
		PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: runCtx},
	})
	require.NoError(t, err)
	require.Len(t, skipped.Transactions, 1)
	require.Equal(t, "skipped_lifecycle", skipped.Transactions[0].Status)
	contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	require.Contains(t, string(contents), "id: EFF-0001")

	execution, err := ApplyIdentifierRepairPlan(context.Background(), runCtx, plan, result.identifierRepair.Assembly, result.identifierRepair.Bindings, Options{
		Fix: true, AllowHistorical: true, Confirm: func(string) (bool, error) { return true, nil },
		PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: runCtx},
	})
	require.NoError(t, err)
	require.NotEmpty(t, execution.Applied, execution)
	contents, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	require.Contains(t, string(contents), "id: EFF-2026-08-06-12-20")
}
