package init

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLegacyCustomIdentifiersKeepUpgradeReviewable(t *testing.T) {
	for _, owned := range []bool{false, true} {
		for _, choice := range []string{"defer", "keep", "take"} {
			name := map[bool]string{false: "unknown", true: "owned"}[owned] + "/" + choice
			t.Run(name, func(t *testing.T) {
				root := legacyOwnershipRepo(t)
				clearLegacyEvidence(t, root)
				custom := legacyEffortSchema + "type Custom { id: String! @identifier(preferred: true, prefix: \"CUSTOM\") }\n"
				writeTestFile(t, root, legacyEffortPath, custom)
				if owned {
					record, _ := loadGeneratedFiles(root)
					record.Written[legacyEffortPath] = ontologyFingerprint([]byte(custom))
					require.NoError(t, record.save(root))
				}
				writeTestFile(t, root, ".gitignore", "local-only\n")
				plan, err := Plan(RunOptions{Dir: root})
				require.NoError(t, err)
				decision := findSetupDecision(t, plan.Writes.Decisions, moduleEffortPath)
				require.Equal(t, "edited", decision.Kind)
				require.Equal(t, legacyEffortPath, decision.From)
				require.Contains(t, decision.Diff, "-type Custom")
				corePath := ".rhizome/modules/core/ontology/core.graphql"
				require.Contains(t, plan.Writes.Files, corePath)
				opts := RunOptions{Dir: root}
				switch choice {
				case "keep":
					opts.Keep = []string{moduleEffortPath}
				case "take":
					opts.Take = []string{moduleEffortPath}
				}
				before := fixtureFiles(t, root)
				_, err = runInit(t, opts)
				if choice == "take" {
					require.ErrorContains(t, err, "Custom.id identifier allocation contract")
					require.Equal(t, before, fixtureFiles(t, root))
					return
				}
				require.NoError(t, err)
				require.FileExists(t, filepath.Join(root, corePath))
				require.Equal(t, custom, readTestFile(t, root, legacyEffortPath))
				require.NoFileExists(t, filepath.Join(root, moduleEffortPath))
				plan, err = Plan(RunOptions{Dir: root})
				require.NoError(t, err)
				if choice == "keep" {
					require.Empty(t, plan.Writes.Decisions)
					record, _ := loadGeneratedFiles(root)
					require.NotEmpty(t, record.Declined[moduleEffortPath])
				} else {
					findSetupDecision(t, plan.Writes.Decisions, moduleEffortPath)
				}
			})
		}
	}
}
