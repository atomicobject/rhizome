package qualityeval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

type corpusSourceRename struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Locator string `json:"locator"`
}

type corpusRejudgment struct {
	CaseID string                    `json:"caseId"`
	Source string                    `json:"source"`
	Before map[string]any            `json:"before"`
	After  map[string]any            `json:"after"`
	Added  map[string]map[string]any `json:"added"`
}

type corpusMigration struct {
	Renames     []corpusSourceRename `json:"renames"`
	Rejudgments struct {
		Reviewer  string             `json:"reviewer"`
		Changes   []corpusRejudgment `json:"changes"`
		Witnesses map[string]string  `json:"witnesses"`
	} `json:"rejudgments"`
	RetiredCases []struct {
		ID       string         `json:"id"`
		Reason   string         `json:"reason"`
		Evidence string         `json:"evidence"`
		Case     map[string]any `json:"case"`
	} `json:"retiredCases"`
}

func readCorpusArtifact(t *testing.T, name string, target any) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, target))
	return data
}

func TestRefactorCorpusPreservesHistoricalDataAndSupportedJudgments(t *testing.T) {
	for name, checksum := range map[string]string{
		"corpus-v1.json": "bdf04b6e6218608da223a13da6bc1a3b639e4776ee5775eea2b2d329e1155225",
		"corpus-v2.json": "40315979b2e8ddcb5b0346586552b897cafe71bcf79c29a7607466dae4ed5ac8",
	} {
		data, err := os.ReadFile("testdata/" + name)
		require.NoError(t, err)
		digest := sha256.Sum256(data)
		require.Equal(t, checksum, hex.EncodeToString(digest[:]), "historical corpus bytes changed: %s", name)
	}

	var before, after struct {
		Cases []map[string]any `json:"cases"`
	}
	readCorpusArtifact(t, "corpus-v2.json", &before)
	readCorpusArtifact(t, "corpus-v3.json", &after)
	var migration corpusMigration
	readCorpusArtifact(t, "corpus-v3-provenance.json", &migration)
	require.Len(t, before.Cases, 265)
	require.Len(t, after.Cases, 263)
	require.Len(t, migration.RetiredCases, 2)
	require.ElementsMatch(t, []string{"RHIZOME_REPOSITORY-CONTROL-07", "RHIZOME_REPOSITORY-CONTROL-08"}, []string{migration.RetiredCases[0].ID, migration.RetiredCases[1].ID})

	renamed := make(map[string]corpusSourceRename)
	for _, rename := range migration.Renames {
		require.NotEmpty(t, rename.From)
		require.NotEmpty(t, rename.To)
		require.NotEmpty(t, rename.Locator)
		require.NotContains(t, renamed, rename.From)
		renamed[rename.From] = rename
	}
	// The only grade changes approved for this migration are the two explicit,
	// independently reviewed shared-policy cases. The ledger retains the full
	// before/after judgments and source witnesses; all other case fields below
	// must remain identical to the frozen historical workload.
	require.Len(t, migration.Rejudgments.Changes, 2)
	require.NotEmpty(t, migration.Rejudgments.Reviewer)
	require.Len(t, migration.Rejudgments.Witnesses, 3)
	rejudged := make(map[string]corpusRejudgment)
	for _, change := range migration.Rejudgments.Changes {
		require.Contains(t, []string{"RHIZOME_REPOSITORY-CONTROL-11", "RHIZOME_REPOSITORY-CONTROL-12"}, change.CaseID)
		require.NotContains(t, rejudged, change.CaseID)
		require.Equal(t, "pkg/embedindex/syncer.go", change.Source)
		require.Equal(t, float64(2), change.Before["grade"])
		require.Equal(t, float64(1), change.After["grade"])
		require.Equal(t, "agent_source_inspection_rank_blind_refactor_2026-10-17", change.After["method"])
		require.Len(t, change.Added, 1)
		require.Contains(t, change.Added, "pkg/embedindex/syncer_plan.go")
		require.Equal(t, float64(2), change.Added["pkg/embedindex/syncer_plan.go"]["grade"])
		rejudged[change.CaseID] = change
	}
	retired := make(map[string]map[string]any)
	for _, item := range migration.RetiredCases {
		require.NotEmpty(t, item.Reason)
		require.NotEmpty(t, item.Evidence)
		retired[item.ID] = item.Case
	}
	current := make(map[string]map[string]any)
	for _, item := range after.Cases {
		id := item["id"].(string)
		require.NotContains(t, current, id)
		current[id] = item
	}
	for _, original := range before.Cases {
		id := original["id"].(string)
		if preserved, ok := retired[id]; ok {
			require.Equal(t, original, preserved, "retired case must retain all original queries and judgments")
			require.NotContains(t, current, id)
			continue
		}
		judgments := original["judgments"].(map[string]any)
		for source, value := range judgments {
			if rename, ok := renamed[source]; ok {
				delete(judgments, source)
				judgment := value.(map[string]any)
				judgment["locator"] = rename.Locator
				judgments[rename.To] = judgment
			}
		}
		if change, ok := rejudged[id]; ok {
			require.Equal(t, judgments[change.Source], change.Before)
			judgments[change.Source] = change.After
			for source, judgment := range change.Added {
				require.NotContains(t, judgments, source)
				judgments[source] = judgment
			}
		}
		require.Equal(t, original, current[id], "only verified source mappings and the two policy rejudgments may change: %s", id)
	}
}
