package ontology

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestEditSessionPackedInlineFieldsPreserveUntouchedSource(t *testing.T) {
	type operation struct {
		node  int
		field string
		value string
		unset bool
	}
	cases := []struct {
		name, source, expected string
		ops                    []operation
	}{
		{
			name:     "single field with lineage",
			source:   "### Story A\nid:: ^SPEC-001-US1 summary:: Keep this text. status:: ready\n",
			expected: "### Story A\nid:: ^SPEC-001-US1 summary:: Keep this text. status:: done\n",
			ops:      []operation{{field: "status", value: "done"}},
		},
		{
			name:     "separate nodes in one batch",
			source:   "### Story A\nid:: ^SPEC-001-US1 summary:: First. status:: ready\n\n### Story B\nid:: ^SPEC-001-US2 summary:: Second. status:: ready\n",
			expected: "### Story A\nid:: ^SPEC-001-US1 summary:: First. status:: done\n\n### Story B\nid:: ^SPEC-001-US2 summary:: Second. status:: done\n",
			ops:      []operation{{field: "status", value: "done"}, {node: 1, field: "status", value: "done"}},
		},
		{
			name:     "two fields on the same line",
			source:   "### Story A\nid:: ^SPEC-001-US1 summary:: Old summary. status:: ready\n",
			expected: "### Story A\nid:: ^SPEC-001-US1 summary:: A longer summary. status:: done\n",
			ops:      []operation{{field: "status", value: "done"}, {field: "summary", value: "A longer summary."}},
		},
		{
			name:     "duplicate field beside another field",
			source:   "### Story A\nid:: ^SPEC-001-US1 status:: ready\nsummary:: Keep this text. status:: old\n",
			expected: "### Story A\nid:: ^SPEC-001-US1 status:: done\nsummary:: Keep this text. \n",
			ops:      []operation{{field: "status", value: "done"}},
		},
		{
			name:     "duplicate fields on the same line",
			source:   "### Story A\nid:: ^SPEC-001-US1 status:: ready summary:: Keep this text. status:: old\n",
			expected: "### Story A\nid:: ^SPEC-001-US1 status:: done summary:: Keep this text. \n",
			ops:      []operation{{field: "status", value: "done"}},
		},
		{
			name:     "remove duplicate fields without removing siblings",
			source:   "### Story A\nid:: ^SPEC-001-US1 status:: ready\nsummary:: Keep this text. status:: old\n",
			expected: "### Story A\nid:: ^SPEC-001-US1 \nsummary:: Keep this text. \n",
			ops:      []operation{{field: "status", unset: true}},
		},
		{
			name:     "undeclared property after known field",
			source:   "### Story A\nid:: ^SPEC-001-US1\nstatus:: ready unknown:: Keep this text\n",
			expected: "### Story A\nid:: ^SPEC-001-US1\nstatus:: done unknown:: Keep this text\n",
			ops:      []operation{{field: "status", value: "done"}},
		},
		{
			name:     "undeclared property before known field",
			source:   "### Story A\nid:: ^SPEC-001-US1\nunknown:: Keep this text status:: ready\n",
			expected: "### Story A\nid:: ^SPEC-001-US1\nunknown:: Keep this text status:: done\n",
			ops:      []operation{{field: "status", value: "done"}},
		},
		{
			name:     "narrative before known field",
			source:   "### Story A\nid:: ^SPEC-001-US1\nKeep this prose. status:: ready\n",
			expected: "### Story A\nid:: ^SPEC-001-US1\nKeep this prose. status:: done\n",
			ops:      []operation{{field: "status", value: "done"}},
		},
		{
			name:     "raw source neighbors across batched nodes",
			source:   "### Story A\nid:: ^SPEC-001-US1\nstatus:: ready unknown:: First\n\n### Story B\nid:: ^SPEC-001-US2\nunknown:: Second status:: ready\n\n### Story C\nid:: ^SPEC-001-US3\nKeep this prose. status:: ready\n",
			expected: "### Story A\nid:: ^SPEC-001-US1\nstatus:: done unknown:: First\n\n### Story B\nid:: ^SPEC-001-US2\nunknown:: Second status:: done\n\n### Story C\nid:: ^SPEC-001-US3\nKeep this prose. status:: done\n",
			ops:      []operation{{field: "status", value: "done"}, {node: 1, field: "status", value: "done"}, {node: 2, field: "status", value: "done"}},
		},
		{
			name:     "remove same-line duplicates around undeclared property",
			source:   "### Story A\nid:: ^SPEC-001-US1\nstatus:: ready unknown:: Keep this text status:: old\n",
			expected: "### Story A\nid:: ^SPEC-001-US1\n unknown:: Keep this text \n",
			ops:      []operation{{field: "status", unset: true}},
		},
		{
			name:     "remove field after narrative",
			source:   "### Story A\nid:: ^SPEC-001-US1\nKeep this prose. status:: ready\n",
			expected: "### Story A\nid:: ^SPEC-001-US1\nKeep this prose. \n",
			ops:      []operation{{field: "status", unset: true}},
		},
		{
			name:     "replace standalone duplicate metadata",
			source:   "### Story A\nid:: ^SPEC-001-US1\nstatus:: ready\nstatus:: old\nUnchanged paragraph.\n",
			expected: "### Story A\nid:: ^SPEC-001-US1\nstatus:: done\nUnchanged paragraph.\n",
			ops:      []operation{{field: "status", value: "done"}},
		},
		{
			name:     "replace duplicate bullet metadata",
			source:   "### Story A\nid:: ^SPEC-001-US1\n  - status:: ready\n  - status:: old\nUnchanged paragraph.\n",
			expected: "### Story A\nid:: ^SPEC-001-US1\n  - status:: done\nUnchanged paragraph.\n",
			ops:      []operation{{field: "status", value: "done"}},
		},
		{
			name:     "remove standalone and bullet metadata lines",
			source:   "### Story A\nid:: ^SPEC-001-US1\nstatus:: ready\n  - status:: old\nUnchanged paragraph.\n",
			expected: "### Story A\nid:: ^SPEC-001-US1\nUnchanged paragraph.\n",
			ops:      []operation{{field: "status", unset: true}},
		},
		{
			name:     "packed field at end of file",
			source:   "### Story A\nid:: ^SPEC-001-US1 summary:: Keep this text. status:: ready",
			expected: "### Story A\nid:: ^SPEC-001-US1 summary:: Keep this text. status:: done",
			ops:      []operation{{field: "status", value: "done"}},
		},
		{
			name:     "packed field with CRLF",
			source:   "### Story A\r\nid:: ^SPEC-001-US1 summary:: Keep this text. status:: ready\r\n",
			expected: "### Story A\r\nid:: ^SPEC-001-US1 summary:: Keep this text. status:: done\r\n",
			ops:      []operation{{field: "status", value: "done"}},
		},
	}
	for _, tc := range cases {
		for _, mode := range []string{"preview", "lineage", "commit"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				session, refs := newPackedInlineEditSession(t, tc.source)
				for _, op := range tc.ops {
					if op.unset {
						require.NoError(t, session.UnsetField(refs[op.node], op.field))
					} else {
						require.NoError(t, session.SetScalarField(refs[op.node], op.field, op.value))
					}
				}
				var plan CommitPlan
				var err error
				switch mode {
				case "preview":
					plan, err = session.Preview(context.Background())
				case "lineage":
					var lineage []PreviewRefLineage
					plan, lineage, err = session.PreviewWithRefLineage(context.Background())
					if err == nil {
						require.NotEmpty(t, lineage)
						for _, item := range lineage {
							require.Equal(t, item.Original.NodeID, item.Preview.NodeID, "editing a property must preserve the authored identifier")
						}
					}
				case "commit":
					var result CommitResult
					result, err = session.Commit(context.Background())
					require.True(t, result.Applied)
					plan = result.Plan
				}
				require.NoError(t, err)
				require.Len(t, plan.Files, 1)
				require.Equal(t, packedInlineNotePrefix+tc.expected, plan.Files[0].UpdatedContentPreview)
				onDisk, err := os.ReadFile(filepath.Join(session.vaultDef.Path, "specs/spec.md"))
				require.NoError(t, err)
				if mode == "commit" {
					require.Equal(t, packedInlineNotePrefix+tc.expected, string(onDisk))
				} else {
					require.Equal(t, packedInlineNotePrefix+tc.source, string(onDisk))
				}
			})
		}
	}
}

const packedInlineNotePrefix = "---\ntype: Spec\nsummary: Test\n---\n\n# Spec\n\n## Stories\n\n"

func newPackedInlineEditSession(t *testing.T, body string) (*EditSession, []NodeRef) {
	t.Helper()
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  summary: String @field
  status: String @field
}
type Stories implements Section { stories: [UserStory!] @contains(level: H3) }
type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: Stories @contains(level: H2, heading: "Stories")
}
`)
	writeOntologyNote(t, root, "specs/spec.md", packedInlineNotePrefix+body)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	vault := obsidian.VaultDefinition{Path: root}
	snapshot, err := LoadDocumentSnapshot(context.Background(), vault, &obsidian.Note{}, "specs/spec.md")
	require.NoError(t, err)
	nodes, err := ProjectDocumentNodesFromSnapshot(snapshot, schema)
	require.NoError(t, err)
	var refs []NodeRef
	for _, node := range nodes {
		if node.Ref.TypeName == "UserStory" {
			refs = append(refs, node.Ref)
		}
	}
	require.Len(t, refs, strings.Count(body, "### Story"))
	return NewEditSession(vault, &obsidian.Note{}, schema), refs
}
