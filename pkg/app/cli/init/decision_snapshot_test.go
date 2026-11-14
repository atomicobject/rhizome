package init

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecisionSnapshotTracksReviewedContent(t *testing.T) {
	makePlan := func() *preparedFiles {
		return &preparedFiles{changes: []*change{
			{key: "a", rel: "a", group: "a", state: stateEdited, current: []byte("local\r\n"), desired: []byte("new")},
			{key: "b", rel: "b", group: "a", state: stateUnknown, current: []byte("local\r\n"), desired: []byte("new")},
		}}
	}
	for _, test := range []struct {
		name   string
		change func(*preparedFiles)
	}{
		{"raw current bytes", func(p *preparedFiles) { p.changes[0].current = []byte("local\n") }},
		{"desired bytes", func(p *preparedFiles) { p.changes[0].desired = []byte("newer") }},
		{"source", func(p *preparedFiles) { p.changes[0].from = "legacy/a" }},
		{"path", func(p *preparedFiles) { p.changes[0].rel = "other" }},
		{"key", func(p *preparedFiles) { p.changes[0].key = "other" }},
		{"kind", func(p *preparedFiles) { p.changes[0].state = stateUnknown }},
		{"removal", func(p *preparedFiles) { p.changes[0].removal = true }},
		{"group split", func(p *preparedFiles) { p.changes[1].group = "b" }},
		{"member removed", func(p *preparedFiles) { p.changes = p.changes[:1] }},
		{"member added", func(p *preparedFiles) {
			p.changes = append(p.changes, &change{key: "c", group: "a", state: stateUnknown})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := makePlan()
			before := p.decisionSnapshot()
			test.change(p)
			require.NotEqual(t, before, p.decisionSnapshot())
		})
	}
	p := makePlan()
	before := p.decisionSnapshot()
	p.changes[0], p.changes[1] = p.changes[1], p.changes[0]
	for _, c := range p.changes {
		c.group = "new representative"
		c.asked, c.take = true, true
	}
	require.NoError(t, p.setChoices([]string{"a"}, nil))
	require.Equal(t, before, p.decisionSnapshot(), "ordering, representative, and choices do not change the review")
	empty := (&preparedFiles{}).decisionSnapshot()
	require.Len(t, empty, 64)
	require.NotEqual(t, before, empty)
	require.Equal(t, snapshotHash([]byte("[]")), empty)
}

func TestDecisionSnapshotRejectsEditAfterReview(t *testing.T) {
	root := legacyOwnershipRepo(t)
	clearLegacyEvidence(t, root)
	plan, err := Plan(RunOptions{Dir: root})
	require.NoError(t, err)
	require.NotEmpty(t, plan.Writes.DecisionSnapshot)
	writeTestFile(t, root, legacyEffortPath, legacyEffortSchema+"type Newer { name: String }\n")
	before := fixtureFiles(t, root)
	_, err = Apply(RunOptions{Dir: root, DecisionSnapshot: plan.Writes.DecisionSnapshot, Take: []string{moduleEffortPath}})
	require.ErrorIs(t, err, ErrDecisionsChanged)
	require.ErrorContains(t, err, "--check --json")
	require.Equal(t, before, fixtureFiles(t, root), "stale review preserves every byte including the record")
}

func TestEmptyDecisionSnapshotRejectsNewDecisionBeforeAutomaticWrites(t *testing.T) {
	root := legacyOwnershipRepo(t)
	plan, err := Plan(RunOptions{Dir: root})
	require.NoError(t, err)
	require.Empty(t, plan.Writes.Decisions)
	require.NotEmpty(t, plan.Writes.Files)
	require.NotEmpty(t, plan.Writes.DecisionSnapshot)
	writeTestFile(t, root, legacyEffortPath, legacyEffortSchema+"type Custom { name: String }\n")
	before := fixtureFiles(t, root)
	_, err = Apply(RunOptions{Dir: root, DecisionSnapshot: plan.Writes.DecisionSnapshot})
	require.ErrorIs(t, err, ErrDecisionsChanged)
	require.Equal(t, before, fixtureFiles(t, root))
}

func TestDecisionSnapshotRejectsChangedMirrorGroups(t *testing.T) {
	for _, change := range []string{"split", "growth"} {
		t.Run(change, func(t *testing.T) {
			root := machineRunRepo(t)
			_, err := Apply(RunOptions{Dir: root, Agents: "claude,codex", Workflow: "none", Search: "off"})
			require.NoError(t, err)
			const first = ".agents/skills/rhizome/SKILL.md"
			const second = ".claude/skills/rhizome/SKILL.md"
			record, ok := loadGeneratedFiles(root)
			require.True(t, ok)
			writeTestFile(t, root, first, "local")
			record.Written[first] = "old"
			if change == "split" {
				writeTestFile(t, root, second, "local")
				record.Written[second] = "old"
			}
			require.NoError(t, record.save(root))
			plan, err := Plan(RunOptions{Dir: root})
			require.NoError(t, err)
			decision := findSetupDecision(t, plan.Writes.Decisions, first)
			if change == "split" {
				require.Contains(t, decision.Copies, second)
				writeTestFile(t, root, second, "different edit")
			} else {
				require.NotContains(t, decision.Copies, second)
				writeTestFile(t, root, second, "local")
				record.Written[second] = "old"
				require.NoError(t, record.save(root))
			}
			before := fixtureFiles(t, root)
			_, err = Apply(RunOptions{Dir: root, DecisionSnapshot: plan.Writes.DecisionSnapshot, Take: []string{first}})
			require.ErrorIs(t, err, ErrDecisionsChanged)
			require.Equal(t, before, fixtureFiles(t, root))
		})
	}
}

func TestDecisionSnapshotRequiresExistingSetup(t *testing.T) {
	root := machineRunRepo(t)
	_, err := Apply(RunOptions{Dir: root, DecisionSnapshot: "stale"})
	require.ErrorContains(t, err, "require an existing Rhizome setup")
	require.NoDirExists(t, filepath.Join(root, ".rhizome"))
}

func TestDecisionSnapshotDoesNotMigrateLegacyConfigBeforeValidation(t *testing.T) {
	root := machineRunRepo(t)
	writeTestFile(t, root, ".rhizome/config.yml", "workflowTemplates: [agentic-engineering]\n")
	before := fixtureFiles(t, root)
	_, err := Apply(RunOptions{Dir: root, DecisionSnapshot: "stale"})
	require.ErrorIs(t, err, ErrDecisionsChanged)
	require.Equal(t, before, fixtureFiles(t, root))
}

func TestDecisionSnapshotRoundTrip(t *testing.T) {
	for _, take := range []bool{true, false} {
		t.Run(map[bool]string{true: "take", false: "keep"}[take], func(t *testing.T) {
			root := legacyOwnershipRepo(t)
			clearLegacyEvidence(t, root)
			plan, err := Plan(RunOptions{Dir: root})
			require.NoError(t, err)
			data, err := json.Marshal(plan)
			require.NoError(t, err)
			var decoded SetupPlan
			require.NoError(t, json.Unmarshal(data, &decoded))
			opts := RunOptions{Dir: root, DecisionSnapshot: decoded.Writes.DecisionSnapshot}
			if take {
				opts.Take = []string{moduleEffortPath}
			} else {
				opts.Keep = []string{moduleEffortPath}
			}
			result, err := Apply(opts)
			require.NoError(t, err)
			require.Empty(t, result.Decisions)
			if take {
				require.NoFileExists(t, filepath.Join(root, legacyEffortPath))
			} else {
				require.FileExists(t, filepath.Join(root, legacyEffortPath))
			}
		})
	}
}

func TestDecisionSnapshotAllowsAutomaticUpdatesWithoutChoices(t *testing.T) {
	root := legacyOwnershipRepo(t)
	plan, err := Plan(RunOptions{Dir: root})
	require.NoError(t, err)
	require.Empty(t, plan.Writes.Decisions)
	result, err := Apply(RunOptions{Dir: root, DecisionSnapshot: plan.Writes.DecisionSnapshot})
	require.NoError(t, err)
	require.Contains(t, result.Created, moduleEffortPath)
	_, err = os.Stat(filepath.Join(root, moduleEffortPath))
	require.NoError(t, err)
}
