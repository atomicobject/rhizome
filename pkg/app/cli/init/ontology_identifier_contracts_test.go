package init

import (
	"bytes"
	"context"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/idalloc"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreserveAdoptedIdentifierStrategiesRequiresSeparateMigrationAuthority(t *testing.T) {
	existing := []byte(`
type EffortNote {
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "EFF")
}
type ExplicitNote {
  id: String! @identifier(preferred: true, strategy: SEQUENTIAL, prefix: "EXP")
}
`)
	upstream := []byte(`
type EffortNote {
  id: String! @field(source: "id") @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  summary: String @field(source: "summary")
}
type ExplicitNote {
  id: String! @identifier(preferred: true, strategy: DATETIME, prefix: "EXP")
}
`)

	got, preserved, err := preserveAdoptedIdentifierStrategies(existing, upstream)
	require.NoError(t, err)
	require.Equal(t, []string{"EffortNote.id", "ExplicitNote.id"}, preserved)
	require.Contains(t, string(got), `EffortNote {`)
	require.Contains(t, string(got), `strategy: SEQUENTIAL, prefix: "EFF"`)
	require.Contains(t, string(got), `strategy: SEQUENTIAL, prefix: "EXP"`)
	require.Contains(t, string(got), `separator: "-", pad: 4`)
	require.Contains(t, string(got), `summary: String`)
}

func TestPreserveAdoptedIdentifierStrategiesPreservesPreferredSelector(t *testing.T) {
	existing := []byte(`type EffortNote { id: String! @identifier(preferred: true, prefix: "EFF") }`)
	upstream := []byte(`type EffortNote { id: String! @identifier(preferred: false, strategy: DATETIME, prefix: "EFF") }`)

	got, preserved, err := preserveAdoptedIdentifierStrategies(existing, upstream)
	require.NoError(t, err)
	require.Equal(t, []string{"EffortNote.id"}, preserved)
	require.Contains(t, string(got), `preferred: true`)
	require.Contains(t, string(got), `strategy: SEQUENTIAL`)
}

func TestPreserveAdoptedIdentifierStrategiesIgnoresNonPreferredNamespaces(t *testing.T) {
	existing := []byte(`
type EffortNote { id: String! @identifier(preferred: true, strategy: SEQUENTIAL, prefix: "EFF") }
type LegacyNote { id: String! @identifier(preferred: false, strategy: DATETIME, prefix: "EFF") }
`)
	upstream := []byte(`
type EffortNote { id: String! @identifier(preferred: true, strategy: DATETIME, prefix: "EFF") }
type LegacyNote { id: String! @identifier(preferred: false, strategy: DATETIME, prefix: "EFF") }
`)

	got, preserved, err := preserveAdoptedIdentifierStrategies(existing, upstream)
	require.NoError(t, err)
	require.Equal(t, []string{"EffortNote.id"}, preserved)
	require.Contains(t, string(got), `EffortNote { id: String! @identifier(preferred: true, strategy: SEQUENTIAL`)
}

func TestPreserveAdoptedIdentifierStrategiesPreservesCoherentSequentialContract(t *testing.T) {
	existing := []byte(`
type EffortNote {
  id: String! @identifier(preferred: true, strategy: SEQUENTIAL, prefix: "OLD", separator: "_", pad: 6)
}
`)
	upstream := []byte(`
"""Unicode before the directive proves rune offsets stay aligned: café 🌱.
A description can mention @identifier(strategy: DATETIME) without becoming a contract.
"""
type EffortNote {
  id: String! @identifier(preferred: true, derivedSuffix: "U\u0053", strategy: SEQUENTIAL, prefix: "NEW", separator: "-", pad: 4)
  summary: String
}
`)

	got, preserved, err := preserveAdoptedIdentifierStrategies(existing, upstream)
	require.NoError(t, err)
	require.Equal(t, []string{"EffortNote.id"}, preserved)
	require.Contains(t, string(got), `@identifier(derivedSuffix: "U\u0053", preferred: true, strategy: SEQUENTIAL, prefix: "OLD", separator: "_", pad: 6)`)
	require.Contains(t, string(got), "café 🌱")
	require.Contains(t, string(got), "summary: String")

	contracts, err := parseIdentifierAllocationContracts("result", got)
	require.NoError(t, err)
	require.Equal(t, "SEQUENTIAL", contracts["EffortNote.id"].allocation.strategy)
	require.Equal(t, "OLD", contracts["EffortNote.id"].allocation.prefix)
	require.Equal(t, "_", contracts["EffortNote.id"].allocation.separator)
	require.Equal(t, 6, contracts["EffortNote.id"].allocation.pad)
}

func TestPreserveAdoptedIdentifierStrategiesPreservesDatetimeWithoutSequentialPad(t *testing.T) {
	existing := []byte(`
type EffortNote {
  id: String! @identifier(preferred: true, strategy: DATETIME, prefix: "EFF", separator: ":")
}
`)
	upstream := []byte(`
type EffortNote {
  id: String! @identifier(preferred: true, strategy: SEQUENTIAL, prefix: "EFF", separator: "-", pad: 6)
}
`)

	got, preserved, err := preserveAdoptedIdentifierStrategies(existing, upstream)
	require.NoError(t, err)
	require.Equal(t, []string{"EffortNote.id"}, preserved)
	require.Contains(t, string(got), `@identifier(preferred: true, strategy: DATETIME, prefix: "EFF", separator: ":")`)
	require.NotContains(t, string(got), "pad:")
}

func TestPreserveAdoptedIdentifierStrategiesSupportsTypeExtensions(t *testing.T) {
	existing := []byte(`
type EffortNote { summary: String }
extend type EffortNote {
  id: String! @identifier(preferred: true, prefix: "EFF", pad: 6)
}
`)
	upstream := []byte(`
type EffortNote { summary: String }
extend type EffortNote {
  id: String! @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
}
`)

	got, preserved, err := preserveAdoptedIdentifierStrategies(existing, upstream)
	require.NoError(t, err)
	require.Equal(t, []string{"EffortNote.id"}, preserved)
	require.Contains(t, string(got), `strategy: SEQUENTIAL, prefix: "EFF", separator: "-", pad: 6`)
}

func TestPreserveAdoptedIdentifierStrategiesFailsClosedOnUnsafeRefresh(t *testing.T) {
	existing := []byte(`type EffortNote { id: String! @identifier(preferred: true, prefix: "EFF") }`)

	t.Run("missing matching directive", func(t *testing.T) {
		_, _, err := preserveAdoptedIdentifierStrategies(existing, []byte(`type EffortNote { id: String! }`))
		require.ErrorContains(t, err, "no longer declares the matching @identifier field")
	})

	t.Run("comment inside edited directive", func(t *testing.T) {
		_, _, err := preserveAdoptedIdentifierStrategies(existing, []byte(`
type EffortNote {
  id: String! @identifier(
    preferred: true
    # Keep this explanation attached to the argument.
    strategy: DATETIME
    prefix: "EFF"
  )
}
`))
		require.ErrorContains(t, err, "comments inside @identifier")
	})

	t.Run("preserved contract conflicts with new namespace owner", func(t *testing.T) {
		_, _, err := preserveAdoptedIdentifierStrategies(existing, []byte(`
type EffortNote {
  id: String! @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
}
type NewEffortNote {
  id: String! @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
}
`))
		require.ErrorContains(t, err, `namespace "EFF-" conflicts`)
	})
}

func TestPreserveAdoptedIdentifierStrategiesLeavesUnchangedContractsAlone(t *testing.T) {
	content := []byte(`type EffortNote { id: String! @identifier(strategy: SEQUENTIAL, prefix: "EFF") }`)

	got, preserved, err := preserveAdoptedIdentifierStrategies(content, content)
	require.NoError(t, err)
	require.Empty(t, preserved)
	require.Equal(t, content, got)
}

func TestPreserveAdoptedIdentifierStrategiesRejectsMalformedSchema(t *testing.T) {
	_, _, err := preserveAdoptedIdentifierStrategies(
		[]byte(`type EffortNote { id: String! @identifier(prefix: "EFF") }`),
		[]byte(`type EffortNote { id: String! @identifier(`),
	)
	require.Error(t, err)
}

// applyScaffoldForTest plans and applies only the starter scaffold.
func applyScaffoldForTest(t *testing.T, root string, ui ownershipUI) syncReport {
	t.Helper()
	record, _ := loadGeneratedFiles(root)
	plan := &filePlan{}
	require.NoError(t, planTemplateScaffold(root, []string{templateAgenticEngineering}, false, plan))
	report, err := applyFilePlan(root, plan, record, ui)
	require.NoError(t, err)
	return report
}

func TestScaffoldUpdatePreservesAdoptedIdentifierStrategy(t *testing.T) {
	root := t.TempDir()
	// An unedited installation predates the shared interface and workspace type.
	installedStrategy := "SEQUENTIAL"
	installed := []byte(`type EffortNote {
 id: String! @identifier(strategy: SEQUENTIAL, preferred: true, prefix: "EFF")
 }
 # older shipped content
 `)
	rel := ".rhizome/ontology/spec-driven.graphql"
	target := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, installed, 0o644))
	record, _ := loadGeneratedFiles(root)
	record.Written[rel] = ontologyFingerprint(installed)
	require.NoError(t, record.save(root))

	report := applyScaffoldForTest(t, root, nil)

	require.Contains(t, report.Updated, rel)
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	contracts, err := parseIdentifierAllocationContracts("updated starter ontology", got)
	require.NoError(t, err)
	require.Equal(t, installedStrategy, contracts["EffortNote.id"].allocation.strategy)
	require.Equal(t, installedStrategy, contracts["EffortWorkspace.id"].allocation.strategy)
	require.NotContains(t, contracts, "Effort.id")
	require.Equal(t, "EFF", contracts["EffortNote.id"].allocation.prefix)
	require.NotContains(t, string(got), "older shipped content")
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	for _, typeName := range []string{"EffortNote", "EffortWorkspace"} {
		allocation, err := idalloc.Allocate(context.Background(), schema, historicalEffortIdentifierStore{}, typeName)
		require.NoError(t, err)
		require.Equal(t, "EFF-0043", allocation.Next)
	}
}

func TestIdentifierMigrationDoesNotCountAsAnOntologyEdit(t *testing.T) {
	root := t.TempDir()
	shipped := specDrivenOntologyTemplate(t)
	migrated := bytes.ReplaceAll(
		shipped,
		[]byte(`@identifier(strategy: DATETIME, preferred: true, prefix: "EFF")`),
		[]byte(`@identifier(strategy: SEQUENTIAL, preferred: true, prefix: "EFF")`),
	)
	require.Contains(t, string(migrated), `strategy: SEQUENTIAL`)
	require.Equal(t, ontologyFingerprint(shipped), ontologyFingerprint(migrated))
	require.NotEqual(t, ontologyFingerprint(shipped), ontologyFingerprint(append(append([]byte(nil), shipped...), "\ntype Extra { id: ID! }\n"...)))

	rel := ".rhizome/ontology/spec-driven.graphql"
	target := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, migrated, 0o644))
	record, _ := loadGeneratedFiles(root)
	record.Written[rel] = ontologyFingerprint(shipped)
	require.NoError(t, record.save(root))

	report := applyScaffoldForTest(t, root, failUI{t})

	require.NotContains(t, report.changedPaths(), rel)
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, migrated, got, "init never rekeys an identifier pool")
}

func TestEditedOntologyIsKeptWithoutADecision(t *testing.T) {
	root := t.TempDir()
	installed := append(specDrivenOntologyTemplate(t), []byte("\n# team addition\n")...)
	rel := ".rhizome/ontology/spec-driven.graphql"
	target := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, installed, 0o644))
	record, _ := loadGeneratedFiles(root)
	record.Written[rel] = "0000000000000000"
	require.NoError(t, record.save(root))

	report := applyScaffoldForTest(t, root, nil)

	require.Contains(t, report.Kept, rel)
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, installed, got)
}

func specDrivenOntologyTemplate(t *testing.T) []byte {
	t.Helper()
	files, err := loadStarterOntologyTemplates(templateAgenticEngineering)
	require.NoError(t, err)
	require.Len(t, files, 1)
	return append([]byte(nil), files[0].Content...)
}

func TestPreserveNewInterfaceSiblingIdentifierContract(t *testing.T) {
	upstream := []byte(`
 interface Effort { id: String! }
 type EffortNote implements Effort { id: String! @identifier(preferred: true, strategy: DATETIME, prefix: "EFF") }
 type EffortWorkspace implements Effort { id: String! @identifier(preferred: true, strategy: DATETIME, prefix: "EFF") }
 `)
	for _, directive := range []string{
		`@identifier(preferred: true, strategy: SEQUENTIAL, prefix: "EFF", pad: 6)`,
		`@identifier(preferred: true, strategy: DATETIME, prefix: "CUSTOM", separator: "_")`,
	} {
		t.Run(directive, func(t *testing.T) {
			existing := []byte("type EffortNote { id: String! " + directive + " }")
			got, _, err := preserveAdoptedIdentifierStrategies(existing, upstream)
			require.NoError(t, err)
			contracts, err := parseIdentifierAllocationContracts("updated", got)
			require.NoError(t, err)
			original, err := parseIdentifierAllocationContracts("installed", existing)
			require.NoError(t, err)
			require.True(t, contracts["EffortNote.id"].allocation.equal(original["EffortNote.id"].allocation))
			require.True(t, contracts["EffortWorkspace.id"].allocation.equal(original["EffortNote.id"].allocation))
		})
	}
	t.Run("conflicting existing siblings", func(t *testing.T) {
		existing := []byte(`
   type EffortNote { id: String! @identifier(preferred: true, strategy: SEQUENTIAL, prefix: "EFF") }
   type OtherEffort { id: String! @identifier(preferred: true, strategy: DATETIME, prefix: "EFF") }
  `)
		withOther := append(append([]byte(nil), upstream...), []byte(`
   type OtherEffort implements Effort { id: String! @identifier(preferred: true, strategy: DATETIME, prefix: "EFF") }
  `)...)
		_, _, err := preserveAdoptedIdentifierStrategies(existing, withOther)
		require.ErrorContains(t, err, "conflicting allocation contracts")
	})
}

type historicalEffortIdentifierStore struct{}

func (historicalEffortIdentifierStore) OntologyNodesByType(context.Context, string) ([]codeanchor.IntelOntologyNode, error) {
	return nil, nil
}
func (historicalEffortIdentifierStore) OntologyNodeFieldValuesByNodeIDs(context.Context, []string, []string) ([]codeanchor.IntelOntologyNodeFieldValue, error) {
	return nil, nil
}

func (historicalEffortIdentifierStore) OntologyPathsByType(_ context.Context, typeName string, _ int) ([]string, error) {
	if typeName == "EffortNote" {
		return []string{"historical.md"}, nil
	}
	return nil, nil
}
func (historicalEffortIdentifierStore) CurrentNotePropertyValues(_ context.Context, paths []string, _ []string, _ semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	return []semdb.NotePropertyValueRow{{NotePath: "historical.md", PropertyName: "id", ValueText: "EFF-0042"}}, nil
}

func TestPreserveNewInterfaceSiblingIdentifierContractAcrossExtensions(t *testing.T) {
	for _, declarations := range []string{
		`type EffortNote implements Effort { title: String! }
		 extend type EffortNote { id: String! %s }`,
		`type EffortNote { id: String! %s }
		 extend type EffortNote implements Effort { title: String! }`,
	} {
		t.Run(declarations, func(t *testing.T) {
			installed := []byte(fmt.Sprintf(declarations, `@identifier(preferred: true, strategy: SEQUENTIAL, prefix: "EFF", pad: 6)`))
			upstream := []byte(fmt.Sprintf(declarations, `@identifier(preferred: true, strategy: DATETIME, prefix: "EFF")`) + `
			interface Effort { id: String! }
			type EffortWorkspace implements Effort { title: String! }
			extend type EffortWorkspace { id: String! @identifier(preferred: true, strategy: DATETIME, prefix: "EFF") }
			`)
			got, preserved, err := preserveAdoptedIdentifierStrategies(installed, upstream)
			require.NoError(t, err)
			require.Equal(t, []string{"EffortNote.id", "EffortWorkspace.id"}, preserved)
			contracts, err := parseIdentifierAllocationContracts("updated", got)
			require.NoError(t, err)
			for _, key := range []string{"EffortNote.id", "EffortWorkspace.id"} {
				require.Contains(t, contracts[key].interfaces, "Effort")
				require.Equal(t, "SEQUENTIAL", contracts[key].allocation.strategy)
				require.Equal(t, 6, contracts[key].allocation.pad)
			}
		})
	}
}

func TestPreserveNewSiblingRequiresInterfaceIdentifierField(t *testing.T) {
	for _, tc := range []struct {
		name        string
		declaration string
		inherits    bool
	}{
		{"broad interface", `interface Named { title: String! }`, false},
		{"identifier interface", `interface Named { title: String! id: String! }`, true},
		{"identifier interface extension", `interface Named { title: String! } extend interface Named { id: String! }`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installed := []byte(`type Existing { id: String! @identifier(preferred: true, strategy: SEQUENTIAL, prefix: "CUSTOM", pad: 6) }`)
			upstream := []byte(tc.declaration + `
    type Existing implements Named { title: String! id: String! @identifier(preferred: true, strategy: DATETIME, prefix: "NEW") }
    type Addition implements Named { title: String! id: String! @identifier(preferred: true, strategy: DATETIME, prefix: "NEW") }
   `)
			got, preserved, err := preserveAdoptedIdentifierStrategies(installed, upstream)
			require.NoError(t, err)
			contracts, err := parseIdentifierAllocationContracts("updated", got)
			require.NoError(t, err)
			require.Equal(t, "CUSTOM", contracts["Existing.id"].allocation.prefix)
			if tc.inherits {
				require.Equal(t, []string{"Addition.id", "Existing.id"}, preserved)
				require.True(t, contracts["Addition.id"].allocation.equal(contracts["Existing.id"].allocation))
			} else {
				require.Equal(t, []string{"Existing.id"}, preserved)
				require.Equal(t, "NEW", contracts["Addition.id"].allocation.prefix)
				require.Equal(t, "DATETIME", contracts["Addition.id"].allocation.strategy)
			}
		})
	}
}
