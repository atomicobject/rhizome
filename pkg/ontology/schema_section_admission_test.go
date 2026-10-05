package ontology

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadSchema_SectionRelationTargetAdmissionIgnoresOrder(t *testing.T) {
	targets := []struct{ name, target, declaration string }{
		{"builtin", "Section", ""},
		{"section", "Story", `type Story implements Section { status: String @field }`},
		{"embedded", "Story", `type Story implements Section @node(locator: EMBEDDED) { status: String @field }`},
		{"interface", "StoryContract", `interface StoryContract implements Section { status: String @field }`},
		{"inherited interface", "StoryContract", `interface ZBase implements Section { status: String @field }
interface StoryContract implements ZBase & Section { status: String @field }`},
	}
	relations := []struct{ name, fieldType, directive, message string }{
		{"singular link", "%s", "@link", "link field %s.story cannot target Section types"},
		{"list link", "[%s!]", "@link", "link field %s.story cannot target Section types"},
		{"neighbors", "[%s!]", `@neighbors(direction: OUTBOUND, type: "%s")`, "neighbor field %s.story must target note types, not Section"},
	}
	for _, target := range targets {
		for _, relation := range relations {
			for _, source := range []string{"AConsumer", "ZConsumer"} {
				for _, targetFirst := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/targetFirst=%t", target.name, relation.name, source, targetFirst), func(t *testing.T) {
						root := t.TempDir()
						directive := relation.directive
						if relation.name == "neighbors" {
							directive = fmt.Sprintf(directive, target.target)
						}
						declaration := fmt.Sprintf(`type %s @node(paths: ["notes/*.md"]) { story: %s %s }`, source, fmt.Sprintf(relation.fieldType, target.target), directive)
						if targetFirst {
							writeOntologySchema(t, root, target.declaration+"\n"+declaration)
						} else {
							writeOntologySchema(t, root, declaration+"\n"+target.declaration)
						}
						_, err := LoadSchema(root)
						require.ErrorContains(t, err, fmt.Sprintf(relation.message, source))
						require.Contains(t, err.Error(), "schema.graphql:")
					})
				}
			}
		}
	}
}

func TestLoadSchema_NoteRelationTargetAdmissionIgnoresOrder(t *testing.T) {
	for _, target := range []struct{ name, target, declaration string }{
		{"note", "Record", `type Record @node(paths: ["records/*.md"]) { summary: String }`},
		{"interface", "RecordContract", `interface RecordContract { summary: String }
type Record implements RecordContract @node(paths: ["records/*.md"]) { summary: String }`},
		{"inherited interface", "RecordContract", `interface ZBase { summary: String }
interface RecordContract implements ZBase { summary: String }
type Record implements RecordContract & ZBase @node(paths: ["records/*.md"]) { summary: String }`},
	} {
		for _, source := range []string{"AConsumer", "ZConsumer"} {
			for _, targetFirst := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/targetFirst=%t", target.name, source, targetFirst), func(t *testing.T) {
					root := t.TempDir()
					declaration := fmt.Sprintf(`type %s @node(paths: ["notes/*.md"]) {
  record: %s @link
  records: [%s!] @link
  nearby: [%s!] @neighbors(direction: OUTBOUND, type: "%s")
  note: Note @link
  notes: [Note!] @link
}`, source, target.target, target.target, target.target, target.target)
					if targetFirst {
						writeOntologySchema(t, root, target.declaration+"\n"+declaration)
					} else {
						writeOntologySchema(t, root, declaration+"\n"+target.declaration)
					}
					schema, err := LoadSchema(root)
					require.NoError(t, err)
					for _, name := range []string{"record", "records", "note", "notes"} {
						require.Equal(t, FieldKindLink, schema.Types[source].ByName[name].Kind)
					}
					require.Equal(t, FieldKindNeighbor, schema.Types[source].ByName["nearby"].Kind)
				})
			}
		}
	}
}

func TestLoadSchema_ContainsTargetAdmissionIgnoresOrder(t *testing.T) {
	for _, target := range []struct{ name, target, declaration string }{
		{"builtin", "Section", ""},
		{"section", "Story", `type Story implements Section { status: String @field }`},
		{"embedded", "Story", `type Story implements Section @node(locator: EMBEDDED) { status: String @field }`},
		{"inherited section", "Story", `interface ZBase implements Section { status: String @field }
interface StoryContract implements ZBase & Section { status: String @field }
type Story implements StoryContract & ZBase & Section { status: String @field }`},
		{"inherited embedded", "Story", `interface ZBase implements Section { status: String @field }
interface StoryContract implements ZBase & Section { status: String @field }
type Story implements StoryContract & ZBase & Section @node(locator: EMBEDDED) { status: String @field }`},
	} {
		for _, source := range []string{"AConsumer", "ZConsumer"} {
			for _, targetFirst := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/targetFirst=%t", target.name, source, targetFirst), func(t *testing.T) {
					root := t.TempDir()
					declaration := fmt.Sprintf(`type %s @node(paths: ["notes/*.md"]) {
  story: %s @contains(level: H2, heading: "Story")
  stories: [%s!] @contains(level: H2)
}`, source, target.target, target.target)
					if targetFirst {
						writeOntologySchema(t, root, target.declaration+"\n"+declaration)
					} else {
						writeOntologySchema(t, root, declaration+"\n"+target.declaration)
					}
					schema, err := LoadSchema(root)
					require.NoError(t, err)
					require.Equal(t, FieldKindSection, schema.Types[source].ByName["story"].Kind)
					require.Equal(t, FieldKindSection, schema.Types[source].ByName["stories"].Kind)
					if target.target != "Section" {
						role := TypeRoleSection
						if target.name == "embedded" || target.name == "inherited embedded" {
							role = TypeRoleEmbeddedNode
						}
						require.Equal(t, role, schema.Types[target.target].Role)
					}
				})
			}
		}
	}
}

func TestLoadSchema_SectionAdmissionRetainsInvalidSchemaRejections(t *testing.T) {
	for _, tc := range []struct{ name, declaration, message string }{
		{"file-backed Section", `type Entry implements Section @node(paths: ["notes/*.md"]) { summary: String @field }`, "file-backed node type \"Entry\" cannot implement Section"},
		{"file-backed inherited Section", `interface ZBase implements Section { summary: String @field }
type Entry implements ZBase & Section @node(paths: ["notes/*.md"]) { summary: String @field }`, "file-backed node type \"Entry\" cannot implement Section"},
		{"non-Section embedded", `type Entry @node(locator: EMBEDDED) { summary: String @field }`, "embedded node type \"Entry\" must implement Section"},
		{"non-Section contains", `type Record @node(paths: ["records/*.md"]) { summary: String }
type Entry @node(paths: ["notes/*.md"]) { story: Record @contains(level: H2, heading: "Story") }`, "must target Section or a type that implements Section"},
		{"universal Note neighbors", `type Entry @node(paths: ["notes/*.md"]) { stories: [Note!] @neighbors(direction: OUTBOUND, type: "Note") }`, "must target note types, not Section"},
		{"self cycle", `interface Entry implements Entry { summary: String }`, "interface inheritance cycle involving Entry"},
		{"interface cycle", `interface Entry implements ZBase { summary: String }
interface ZBase implements Entry { summary: String }`, "circular reference"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologySchema(t, root, tc.declaration)
			_, err := LoadSchema(root)
			require.ErrorContains(t, err, tc.message)
		})
	}
}
