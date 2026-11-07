package ontology

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func recordFixtureRoot(t *testing.T, path string, input any) *RootDocumentSnapshot {
	t.Helper()
	value, err := noteformat.NewMetadataValue(input)
	require.NoError(t, err)
	facts := &noteformat.StructureFacts{Root: "root"}
	rootValue := facts.AddMetadataValue(value)
	var walk func(noteformat.ValueID, noteformat.OwnerKey, string, int)
	walk = func(id noteformat.ValueID, parent noteformat.OwnerKey, address string, ordinal int) {
		value := facts.Values[id-1]
		key := noteformat.OwnerKey("value:" + address)
		if parent == "" {
			key = "root"
		}
		shape := noteformat.OwnerScalar
		if value.Kind == noteformat.MetadataObject {
			shape = noteformat.OwnerMapping
		}
		if value.Kind == noteformat.MetadataArray {
			shape = noteformat.OwnerSequence
		}
		if parent == "" {
			shape = noteformat.OwnerDocument
		}
		owner := noteformat.OwnerFact{Key: key, Parent: parent, Collection: "values", Ordinal: ordinal, Order: len(facts.Owners), Shape: shape, Address: address, Value: id}
		for _, entry := range value.Entries {
			owner.Properties = append(owner.Properties, noteformat.PropertyOccurrenceFact{Key: sourceScalarText(entry.Key), Source: "property", Value: entry.Value})
		}
		facts.Owners = append(facts.Owners, owner)
		for i, entry := range value.Entries {
			walk(entry.Value, key, address+"/"+sourceScalarText(entry.Key), i)
		}
		for i, item := range value.Items {
			walk(item, key, fmt.Sprintf("%s/%d", address, i), i)
		}
	}
	walk(rootValue, "", "", 0)
	require.NoError(t, noteformat.ValidateStructureFacts(facts, -1))
	raw := append([]byte("!native source!\n"), value.CanonicalJSON()...)
	return &RootDocumentSnapshot{NotePath: paths.NotePath(path), Format: "synthetic", RawSource: raw, ContentHash: hashText(string(raw)), Projection: noteformat.Projection{Status: noteformat.ProjectionStatusCurrent, Capabilities: noteformat.MustCapabilities(noteformat.CapabilityStructuralReading), Facts: noteformat.ProjectionFacts{Structure: facts}}, Root: NodeRef{NotePath: path, Kind: NodeKindNote}}
}

const recordRowsSDL = `type Document @node(default: true) { status: String }
type Row @node(locator: EMBEDDED) @source(shape: RECORD, paths: ["*.native"], select: "rows", key: {field: "id"}, title: ["name", "$key"], space: "test", logicalKind: "row") {
 id: ID @field(path: "id")
 name: String @field(path: "name")
 empty: String @field(path: "empty")
 nothing: String @field(path: "nothing")
 absent: String @field(path: "absent")
 refs: [Row!] @field(path: "refs") @references(to: ["Row"], mode: STRICT, values: BY_SHAPE, edge: {name: "uses"})
}`

func recordFixtureSchema(t *testing.T, sdl string) *ontoschema.Schema {
	t.Helper()
	root := t.TempDir()
	writeOntologySchema(t, root, sdl)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	return schema
}

func TestPreparedSyntheticRecordsFieldsAndStableIdentity(t *testing.T) {
	schema := recordFixtureSchema(t, recordRowsSDL)
	input := map[string]any{"rows": []any{map[string]any{"id": "A", "name": "First", "empty": "", "nothing": nil, "refs": []any{"B", "B"}}, map[string]any{"id": "B", "name": "Second"}}}
	prepared, err := PrepareDocument(recordFixtureRoot(t, "model.native", input), schema)
	require.NoError(t, err)
	nodes, err := prepared.Nodes()
	require.NoError(t, err)
	require.Len(t, nodes, 3)
	first, second := nodes[1], nodes[2]
	require.Nil(t, first.Snapshot)
	require.Same(t, prepared, first.Prepared)
	require.Same(t, first.RootSnapshot, second.RootSnapshot)
	require.Equal(t, []string{""}, first.Fields["empty"].Values)
	require.Equal(t, SourcePresent, first.Fields["empty"].Presence)
	require.Equal(t, SourceNull, first.Fields["nothing"].Presence)
	require.Equal(t, SourceMissing, first.Fields["absent"].Presence)
	require.Equal(t, []string{"B", "B"}, first.Fields["refs"].Values)
	children, err := prepared.Children(nodes[0].Ref)
	require.NoError(t, err)
	require.Equal(t, []NodeRef{first.Ref, second.Ref}, children)
	for _, node := range nodes {
		for i := 0; i < 3; i++ {
			same, err := prepared.Project(node.Ref)
			require.NoError(t, err)
			require.Equal(t, node.Ref, same.Ref)
		}
	}
	require.Same(t, prepared, nodes[1].Prepared)
	input["rows"] = []any{map[string]any{"id": "B", "name": "Changed"}, map[string]any{"id": "A", "name": "Changed too"}}
	reordered, err := PrepareDocument(recordFixtureRoot(t, "model.native", input), schema)
	require.NoError(t, err)
	next, err := reordered.Project(first.Ref)
	require.NoError(t, err)
	require.Equal(t, first.Ref.NodeID, next.Ref.NodeID)
	require.Equal(t, OntologyNodeID(first.Ref), OntologyNodeID(next.Ref))
	catalog, err := BuildIntelOntologyNodeReadModel(schema, nodes[0], 123)
	require.NoError(t, err)
	require.Len(t, catalog.Nodes, 3)
	require.Equal(t, "First", catalog.Nodes[1].Title)
	require.Equal(t, "Second", catalog.Nodes[2].Title)
	raw, err := json.Marshal(catalog.Nodes)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "^record")
}

func TestPreparedRecordKeysSelectorsAndPositionalLocators(t *testing.T) {
	tests := []struct {
		name, selector, key, extra string
		input                      any
		keys                       []string
	}{
		{"mapping keys", "groups{}", `{mappingKey: true}`, `group: String @field(path: "group", fallback: MAPPING_KEY)`, map[string]any{"groups": map[string]any{"alpha": map[string]any{"id": "ignored"}}}, []string{"alpha"}},
		{"mapping sequence", "groups{}[]", `{field: "id"}`, `group: String @field(path: "group", fallback: MAPPING_KEY)`, map[string]any{"groups": map[string]any{"alpha": []any{map[string]any{"id": "A"}}}}, []string{"A"}},
		{"scalar rows", "rows", `{field: "id"}`, `id: String @field(path: "id")`, map[string]any{"rows": []any{"one", 2}}, []string{"one", "2"}},
		{"composite", "rows", `{template: "{from}>{to}"}`, `from: String @field(path: "from")`, map[string]any{"rows": []any{map[string]any{"from": "A", "to": "B"}}}, []string{"A>B"}},
		{"positional", "rows", `null`, `id: String @field(path: "id")`, map[string]any{"rows": []any{map[string]any{"value": "one"}, map[string]any{"value": "two"}}}, []string{"1", "2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sdl := fmt.Sprintf(`type Row @node(locator: EMBEDDED) @source(shape: RECORD, paths: ["*.native"], select: %q, key: %s, title: ["$key"], scalarAs: "id", space: "test", logicalKind: "row") {%s}`, tt.selector, tt.key, tt.extra)
			schema := recordFixtureSchema(t, sdl)
			root := recordFixtureRoot(t, "model.native", tt.input)
			prepared, err := PrepareDocument(root, schema)
			require.NoError(t, err)
			var keys []string
			for _, record := range prepared.records {
				keys = append(keys, record.identity.LogicalKey)
			}
			require.Equal(t, tt.keys, keys)
			if tt.name == "mapping keys" || tt.name == "mapping sequence" {
				require.Equal(t, []string{"alpha"}, prepared.records[0].projection.Fields["group"].Values)
			}
			if tt.name == "positional" {
				before := prepared.records[0].projection.Ref
				changed := recordFixtureRoot(t, "model.native", tt.input)
				changed.ContentHash = hashText("unrelated host edit")
				after, err := PrepareDocument(changed, schema)
				require.NoError(t, err)
				require.Equal(t, before.NodeID, after.records[0].projection.Ref.NodeID)
				require.Equal(t, OntologyNodeID(before), OntologyNodeID(after.records[0].projection.Ref))
				_, err = after.Project(before)
				require.ErrorContains(t, err, "record_locator_stale")
			}
		})
	}
}

func TestPreparedRecordDuplicatesAndMembership(t *testing.T) {
	schema := recordFixtureSchema(t, strings.Replace(recordRowsSDL, `shape: RECORD, paths:`, `shape: RECORD, membership: {path: "products", includes: "selected", includeMissing: true}, paths:`, 1))
	root := recordFixtureRoot(t, "model.native", map[string]any{"rows": []any{map[string]any{"id": "A", "products": []any{"selected"}}, map[string]any{"id": "A", "products": []any{"other"}}, map[string]any{"id": "B"}}})
	prepared, err := PrepareDocument(root, schema)
	require.NoError(t, err)
	require.Len(t, prepared.records, 2)
	require.Empty(t, prepared.Diagnostics())
	duplicate, err := PrepareDocument(recordFixtureRoot(t, "model.native", map[string]any{"rows": []any{map[string]any{"id": "A"}, map[string]any{"id": "A"}}}), schema)
	require.NoError(t, err)
	require.Len(t, duplicate.Diagnostics(), 2)
	_, err = duplicate.Project(duplicate.records[0].projection.Ref)
	require.ErrorContains(t, err, "ambiguous")
}

func TestPreparedRecordContainmentAndReferenceModes(t *testing.T) {
	sdl := `type Service @node(locator: EMBEDDED) @source(shape: RECORD, paths: ["*.native"], select: "services", key: {field: "id"}, title: ["$key"], space: "test", logicalKind: "service") {
  jobs: [Job!] @contains(shape: RECORD, select: "jobs", edge: {name: "made-of"})
  strict: [Service!] @field(path: "strict") @references(to: ["Service"], mode: STRICT, values: BY_SHAPE, edge: {name: "uses"})
  tolerant: [String!] @field(path: "tolerant") @references(to: ["Service"], mode: TOLERANT, values: ELEMENTS)
  validate: [String!] @field(path: "validate") @references(to: ["Service"], mode: STRICT, values: MAPPING_KEYS)
  typed: [Service!] @field(path: "typed") @references(to: ["Service"], mode: STRICT, encoding: TYPED, values: ELEMENTS, targets: [{tag: "field", type: "Service", memberPath: "fields[].name"}], edge: {name: "reads"})
 }
 type Job @node(locator: EMBEDDED) @source(shape: RECORD, paths: ["*.native"], select: "services[].jobs", key: null, title: ["$key"], container: "Service", scalarAs: "id", space: "test", logicalKind: "job") { id: String @field(path: "id") }`
	schema := recordFixtureSchema(t, sdl)
	root := recordFixtureRoot(t, "model.native", map[string]any{"services": []any{
		map[string]any{"id": "A", "jobs": []any{"one", "two"}, "strict": []any{"B", "B", "missing"}, "tolerant": []any{"words", "missing"}, "validate": map[string]any{"B": "annotation"}, "typed": []any{"field:B.name.nested", "field:B.missing"}},
		map[string]any{"id": "B", "fields": []any{map[string]any{"name": "name"}}},
	}})
	prepared, err := PrepareDocument(root, schema)
	require.NoError(t, err)
	service := prepared.records[0].projection
	require.Len(t, service.Fields["jobs"].ChildRefs, 2)
	job, err := prepared.ProjectBound(service.Fields["jobs"].ChildRefs[0])
	require.NoError(t, err)
	require.Equal(t, "A.1", job.Record.LogicalKey)
	parent, ok, err := prepared.SemanticParent(job.Ref)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, service.Ref, parent)
	require.Len(t, service.Fields["strict"].ReferenceOccurrences, 3)
	require.Equal(t, service.Fields["strict"].ReferenceOccurrences[0].Target, service.Fields["strict"].ReferenceOccurrences[1].Target)
	require.Equal(t, "record_reference_unresolved", service.Fields["strict"].ReferenceOccurrences[2].Diagnostic)
	require.Empty(t, service.Fields["tolerant"].ReferenceOccurrences[0].Diagnostic)
	require.Len(t, service.Fields["validate"].ReferenceTargets, 1)
	require.Nil(t, service.Type.ByName["validate"].Record.References.Edge)
	require.Equal(t, "name.nested", service.Fields["typed"].ReferenceOccurrences[0].At)
	require.NotNil(t, service.Fields["typed"].ReferenceOccurrences[0].Target)
	require.Equal(t, "record_reference_member", service.Fields["typed"].ReferenceOccurrences[1].Diagnostic)
	require.Len(t, prepared.Diagnostics(), 2)
}

func TestPreparedRecordCompositionOrdersSourcesAndLimitsNamespace(t *testing.T) {
	schema := recordFixtureSchema(t, `type Row @node(locator: EMBEDDED) @source(shape: RECORD, paths: ["*.native"], select: "rows", key: null, title: ["$key"], scalarAs: "id", space: "selected", logicalKind: "row") { id: String @field(path: "id") }`)
	account, err := PrepareDocument(recordFixtureRoot(t, "account.native", map[string]any{"rows": []any{"account"}}), schema)
	require.NoError(t, err)
	product, err := PrepareDocument(recordFixtureRoot(t, "product.native", map[string]any{"rows": []any{"product"}}), schema)
	require.NoError(t, err)
	account.records[0].identity.SourceOrder = 0
	product.records[0].identity.SourceOrder = 1
	beforeAccount, beforeProduct := account.records[0].projection.Ref, product.records[0].projection.Ref
	BindRecordDocuments(product, account)
	require.Equal(t, "1", account.records[0].identity.LogicalKey)
	require.Equal(t, "2", product.records[0].identity.LogicalKey)
	require.Equal(t, "2", product.records[0].projection.DisplayTitle())
	require.Equal(t, beforeAccount, account.records[0].projection.Ref)
	require.Equal(t, beforeProduct, product.records[0].projection.Ref)
}

func TestPreparedRecordHostContainmentHasOptionalEdge(t *testing.T) {
	for _, edge := range []string{"", `, edge: {name: "contains"}`} {
		t.Run(edge, func(t *testing.T) {
			sdl := strings.Replace(recordRowsSDL, `status: String`, `rows: [Row!] @contains(shape: RECORD, select: "rows"`+edge+`)`, 1)
			schema := recordFixtureSchema(t, sdl)
			p, err := PrepareDocument(recordFixtureRoot(t, "model.native", map[string]any{"rows": []any{map[string]any{"id": "A"}, map[string]any{"id": "B"}}}), schema)
			require.NoError(t, err)
			require.Len(t, p.rootProjection.Fields["rows"].ChildRefs, 2)
			edges, err := buildNodeScopedEdges(nil, p.rootProjection, schema, nil, nil, 1)
			require.NoError(t, err)
			if edge == "" {
				require.Empty(t, edges)
			} else {
				require.Len(t, edges, 2)
				for _, row := range edges {
					require.Equal(t, "contains", row.RelationName)
					require.Equal(t, "model.native", row.SrcPath)
					require.NotEmpty(t, row.DstNodeID)
				}
			}
		})
	}
}

func TestRecordIndexBindsAcrossHostsAndRemovesUnresolvedEdges(t *testing.T) {
	schema := recordFixtureSchema(t, recordRowsSDL)
	sources := func(includeTarget bool) []notemeta.NoteSourceSnapshot {
		roots := []*RootDocumentSnapshot{recordFixtureRoot(t, "a.native", map[string]any{"rows": []any{map[string]any{"id": "A", "refs": []any{"B", "B"}}}})}
		if includeTarget {
			roots = append(roots, recordFixtureRoot(t, "b.native", map[string]any{"rows": []any{map[string]any{"id": "B"}}}))
		}
		var out []notemeta.NoteSourceSnapshot
		for _, root := range roots {
			out = append(out, notemeta.NoteSourceSnapshot{Path: root.NotePath, Format: root.Format, RawSource: root.RawSource, Content: string(root.RawSource), ContentHash: root.ContentHash, Projection: root.Projection})
		}
		return out
	}
	built, err := BuildIndexFromNoteSources(t.Context(), obsidian.VaultDefinition{}, sources(true), schema, "before")
	require.NoError(t, err)
	require.Len(t, built.Nodes, 4)
	require.Len(t, built.Edges, 1)
	require.Equal(t, "uses", built.Edges[0].RelationName)
	require.Equal(t, "a.native", built.Edges[0].SrcPath)
	require.Equal(t, "b.native", built.Edges[0].DstPath)
	require.JSONEq(t, `[{"ordinal":0,"value":"B"},{"ordinal":1,"value":"B"}]`, built.Edges[0].OccurrencesJSON)
	after, err := BuildIndexFromNoteSources(t.Context(), obsidian.VaultDefinition{}, sources(false), schema, "after")
	require.NoError(t, err)
	require.Len(t, after.Nodes, 2)
	require.Empty(t, after.Edges)
	unresolved := 0
	for _, issue := range after.ValidationIssues {
		if issue.Code == "record_reference_unresolved" {
			unresolved++
		}
	}
	require.Equal(t, 2, unresolved)
}

func TestNativeLinksFollowNearestSelectedOwner(t *testing.T) {
	schema := recordFixtureSchema(t, recordRowsSDL)
	root := recordFixtureRoot(t, "model.native", map[string]any{"rows": []any{map[string]any{"id": "A", "detail": "one"}, map[string]any{"id": "B", "detail": "two"}}})
	root.Projection.Facts.Links = []noteformat.UnresolvedAuthoredLinkFact{{Owner: "value:/rows/0/detail", Target: "first"}, {Owner: "value:/rows/1/detail", Target: "second"}}
	p, err := PrepareDocument(root, schema)
	require.NoError(t, err)
	require.Empty(t, p.rootProjection.SourceLinks())
	require.Equal(t, "first", p.records[0].projection.SourceLinks()[0].Target)
	require.Len(t, p.records[0].projection.SourceLinks(), 1)
	require.Equal(t, "second", p.records[1].projection.SourceLinks()[0].Target)
	require.Len(t, p.records[1].projection.SourceLinks(), 1)
}

func TestPositionalCompositionAssignsParentsBeforeChildren(t *testing.T) {
	schema := recordFixtureSchema(t, `type Parent @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.native"],select:"parents",key:null,title:["$key"],space:"test",logicalKind:"parent",sourceOrder:1){ kids:[Kid!] @contains(shape:RECORD,select:"kids") }
 type Kid @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.native"],select:"parents[].kids",key:null,title:["$key"],container:"Parent",space:"test",logicalKind:"kid",sourceOrder:0){ name:String @field(path:"name") }`)
	input := map[string]any{"parents": []any{map[string]any{"kids": []any{map[string]any{"name": "child"}}}}}
	account, err := PrepareDocument(recordFixtureRoot(t, "account.native", input), schema)
	require.NoError(t, err)
	product, err := PrepareDocument(recordFixtureRoot(t, "product.native", input), schema)
	require.NoError(t, err)
	BindRecordDocuments(product, account)
	require.Equal(t, "2", product.records[0].identity.LogicalKey)
	require.Equal(t, "2.1", product.records[1].identity.LogicalKey)
	require.Equal(t, "2.1", product.records[1].projection.DisplayTitle())
}
