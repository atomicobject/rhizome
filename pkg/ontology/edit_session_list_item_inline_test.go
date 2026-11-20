package ontology

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// New inline properties on list and checkbox items belong on indented
// continuation lines; a column-0 line ends the list in Obsidian.
func TestEditSession_ListItemInlinePropertyStaysUnderItem(t *testing.T) {
	cases := []struct {
		name, source, expected string
		shape                  string // defaults to LIST_ITEM
		item                   int
		value                  string // empty unsets the field
	}{
		{
			name:     "add after existing properties",
			source:   "- **Simpler core** #radar\n  state:: Reaffirmed.\n  updated:: 2026-11-13\n- **SalesBridge** #radar\n",
			expected: "- **Simpler core** #radar\n  state:: Reaffirmed.\n  updated:: 2026-11-13\n  triage:: done\n- **SalesBridge** #radar\n",
			value:    "done",
		},
		{
			name:     "add to item without properties",
			source:   "- **Simpler core** #radar\n- **SalesBridge** #radar\n",
			expected: "- **Simpler core** #radar\n  triage:: done\n- **SalesBridge** #radar\n",
			value:    "done",
		},
		{
			name:     "add to nested item",
			source:   "- Parent\n  - **Nested** #radar\n    state:: Open.\n- Other\n",
			expected: "- Parent\n  - **Nested** #radar\n    state:: Open.\n    triage:: done\n- Other\n",
			value:    "done",
		},
		{
			name:     "add to nested item without properties at end of file",
			source:   "- Parent\n  - **Nested** #radar",
			expected: "- Parent\n  - **Nested** #radar\n    triage:: done\n",
			value:    "done",
		},
		{
			name:     "add to checkbox item",
			source:   "- [ ] **Ship it** #radar\n  state:: Open.\n- [ ] **Next** #radar\n",
			expected: "- [ ] **Ship it** #radar\n  state:: Open.\n  triage:: done\n- [ ] **Next** #radar\n",
			shape:    "CHECKBOX_ITEM",
			value:    "done",
		},
		{
			name:     "add to nested checkbox item without properties",
			source:   "- [ ] **Parent** #radar\n  - [x] **Child** #radar\n- [ ] **Next** #radar\n",
			expected: "- [ ] **Parent** #radar\n  - [x] **Child** #radar\n    triage:: done\n- [ ] **Next** #radar\n",
			shape:    "CHECKBOX_ITEM",
			item:     1,
			value:    "done",
		},
		{
			name:     "add before child items",
			source:   "- **Parent** #radar\n  state:: Open.\n  - child note\n- **Next** #radar\n",
			expected: "- **Parent** #radar\n  state:: Open.\n  triage:: done\n  - child note\n- **Next** #radar\n",
			value:    "done",
		},
		{
			name:     "add before a paragraph that ends the list",
			source:   "- **Simpler core** #radar\n  state:: Open.\n\nClosing prose.\n",
			expected: "- **Simpler core** #radar\n  state:: Open.\n  triage:: done\n\nClosing prose.\n",
			value:    "done",
		},
		{
			name:     "indent follows property lines, not prose",
			source:   "- **Simpler core** #radar\n  state:: Open.\n    wrapped prose\n- **Next** #radar\n",
			expected: "- **Simpler core** #radar\n  state:: Open.\n    wrapped prose\n  triage:: done\n- **Next** #radar\n",
			value:    "done",
		},
		{
			name:     "property-like text in a code block does not set the indent",
			source:   "- **Simpler core** #radar\n  state:: Open.\n  ```\n      example:: value\n  ```\n- **Next** #radar\n",
			expected: "- **Simpler core** #radar\n  state:: Open.\n  ```\n      example:: value\n  ```\n  triage:: done\n- **Next** #radar\n",
			value:    "done",
		},
		{
			name:     "property-like text in a fence at the item's column does not set the indent",
			source:   "- **Simpler core** #radar\n    state:: Open.\n  ```\n  example:: value\n  ```\n- **Next** #radar\n",
			expected: "- **Simpler core** #radar\n    state:: Open.\n  ```\n  example:: value\n  ```\n    triage:: done\n- **Next** #radar\n",
			value:    "done",
		},
		{
			name:     "column-0 property from the old writer is not copied",
			source:   "- **Simpler core** #radar\n  state:: Open.\nupdated:: 2026-11-13\n- **Next** #radar\n",
			expected: "- **Simpler core** #radar\n  state:: Open.\nupdated:: 2026-11-13\n  triage:: done\n- **Next** #radar\n",
			value:    "done",
		},
		{
			name:     "trailing spaces at end of file stay on the item line",
			source:   "- **Simpler core** #radar  ",
			expected: "- **Simpler core** #radar  \n  triage:: done\n",
			value:    "done",
		},
		{
			name:     "replace keeps indentation",
			source:   "- **Simpler core** #radar\n  triage:: keep\n  updated:: 2026-11-13\n- **SalesBridge** #radar\n",
			expected: "- **Simpler core** #radar\n  triage:: done\n  updated:: 2026-11-13\n- **SalesBridge** #radar\n",
			value:    "done",
		},
		{
			name:     "remove leaves no blank line",
			source:   "- **Simpler core** #radar\n  state:: Reaffirmed.\n  triage:: done\n  updated:: 2026-11-13\n- **SalesBridge** #radar\n",
			expected: "- **Simpler core** #radar\n  state:: Reaffirmed.\n  updated:: 2026-11-13\n- **SalesBridge** #radar\n",
		},
		{
			name:     "remove last property of nested item",
			source:   "- Parent\n  - **Nested** #radar\n    triage:: done\n- Other\n",
			expected: "- Parent\n  - **Nested** #radar\n- Other\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shape := tc.shape
			if shape == "" {
				shape = "LIST_ITEM"
			}
			root := t.TempDir()
			writeOntologyTestConfig(t, root)
			writeOntologySchema(t, root, `
type RadarItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: `+shape+`, marker: "#radar", paths: ["**/*.md"]) {
  state: String @field @authoring(style: INLINE_PROPERTY)
  updated: Date @field @authoring(style: INLINE_PROPERTY)
  triage: RadarTriage @field @authoring(style: INLINE_PROPERTY)
}

enum RadarTriage { done drop keep }
`)
			const notePrefix = "# Matter\n\n## On the radar\n\n"
			writeOntologyNote(t, root, "notes/matter.md", notePrefix+tc.source)
			schema, err := ontoschema.LoadSchema(root)
			require.NoError(t, err)
			snapshot, err := BuildDocumentSnapshot("notes/matter.md", notePrefix+tc.source, time.Time{})
			require.NoError(t, err)
			refs, err := GlobalSourceNodeRefsFromSnapshot(snapshot, schema)
			require.NoError(t, err)
			require.Greater(t, len(refs), tc.item)

			session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
			if tc.value == "" {
				require.NoError(t, session.UnsetField(refs[tc.item], "triage"))
			} else {
				require.NoError(t, session.SetScalarField(refs[tc.item], "triage", tc.value))
			}
			result, err := session.Commit(context.Background())
			require.NoError(t, err)
			require.True(t, result.Applied)
			written, err := os.ReadFile(filepath.Join(root, "notes", "matter.md"))
			require.NoError(t, err)
			require.Equal(t, notePrefix+tc.expected, string(written))
		})
	}
}
