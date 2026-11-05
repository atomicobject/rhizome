package query

import (
	"context"
	"fmt"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/stretchr/testify/require"
)

const meetingRootSchema = `
interface Dated { date: Date }
interface Meeting implements Dated { date: Date who: [String!] clientRef: String }
interface Empty { name: String }
type GeneralMeeting implements Meeting & Dated @node(paths: ["general/*.md"]) {
  date: Date @field
  who: [String!] @field
  clientRef: String @field(source: "client")
  privateField: String @field
}
type CoachingSession implements Meeting & Dated @node(paths: ["coaching/*.md"]) {
  date: Date @field(source: "sessiondate")
  who: [String!] @field
  clientRef: String @field(source: "customer")
}
type Other @node(paths: ["other/*.md"]) { name: String }
`

func meetingRootEnv(t *testing.T) *queryTestEnv {
	t.Helper()
	return newCustomQueryTestEnv(t, meetingRootSchema, map[string]string{
		"general/a.md":  "---\ntitle: Meeting A\ndate: 2026-01-04\nwho: [Drew]\nclient: Acme\ngroup: common\n---\n",
		"general/b.md":  "---\ntitle: Meeting B\ndate: 2026-01-02\nwho: [Drew]\nclient: Other\ngroup: common\n---\n",
		"coaching/a.md": "---\ntitle: Meeting C\nsessiondate: 2026-01-03\nwho: [Drew]\ncustomer: Acme\ngroup: common\n---\n",
		"coaching/b.md": "---\ntitle: Meeting D\nsessiondate: 2026-01-01\nwho: [Drew]\ncustomer: Acme\ngroup: common\n---\n",
		"other/a.md":    "---\nname: Other\n---\n",
	})
}

func executeMeetingRoot(t *testing.T, env *queryTestEnv, text string) Result {
	t.Helper()
	prepared, errs := Prepare(env.execSchema, text)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	return result
}

func TestInterfaceRootSchemaDiscoveryAndBounds(t *testing.T) {
	env := meetingRootEnv(t)
	requireSchemaField(t, env.execSchema, "Query", "meeting", "[Meeting!]!")
	requireSchemaField(t, env.execSchema, "Query", "dated", "[Dated!]!")
	for _, name := range []string{"empty", "section", "noteNode"} {
		require.Nil(t, env.execSchema.Schema.Query.Fields.ForName(name))
	}
	discovery, err := DiscoverSchema(env.schema, env.execSchema, "Meeting")
	require.NoError(t, err)
	require.Equal(t, []DiscoveredRoot{{Name: "meeting", MaxFirst: publicRootFirstMax, ExclusiveSelectors: []string{"path", "find", "property", "semantic"}}}, discovery.Roots)
	require.Contains(t, discovery.Schema, "offset: Int = 0")
	for _, query := range []string{
		`{ meeting(first: 5001) { date } }`,
		`{ meeting(offset: -1) { date } }`,
		`{ meeting(path: "general/a.md", find: "Meeting") { date } }`,
	} {
		_, errs := Prepare(env.execSchema, query)
		require.NotEmpty(t, errs, query)
	}
	prepared, errs := Prepare(env.execSchema, `{ __type(name: "Query") { fields { name } } }`)
	require.Empty(t, errs)
	result := ExecuteIntrospection(env.execSchema, prepared)
	require.Empty(t, result.Errors)
	fields := result.Data["__type"].(map[string]any)["fields"].([]any)
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.(map[string]any)["name"].(string))
	}
	require.Contains(t, names, "meeting")
}

func TestInterfaceRootGlobalPagesAndConcreteBindings(t *testing.T) {
	env := meetingRootEnv(t)
	result := executeMeetingRoot(t, env, `{
	  first: meeting(first: 2, sort: [{field: "date", direction: desc}]) { date ... on NoteNode { path resolvedType } }
	  next: meeting(first: 2, offset: 2, sort: [{field: "date", direction: desc}]) { date ... on NoteNode { path resolvedType } }
	  filtered: meeting(filters: [{field: "clientRef", value: "Acme"}], sort: [{field: "date", direction: desc}]) { date who clientRef ... on NoteNode { resolvedType } }
	  parent: dated(first: 1, sort: [{field: "date", direction: desc}]) { date }
	}`)
	require.Equal(t, []any{
		map[string]any{"date": "2026-01-04", "path": "general/a.md", "resolvedType": "GeneralMeeting"},
		map[string]any{"date": "2026-01-03", "path": "coaching/a.md", "resolvedType": "CoachingSession"},
	}, result.Data["first"])
	require.Equal(t, []any{
		map[string]any{"date": "2026-01-02", "path": "general/b.md", "resolvedType": "GeneralMeeting"},
		map[string]any{"date": "2026-01-01", "path": "coaching/b.md", "resolvedType": "CoachingSession"},
	}, result.Data["next"])
	require.Len(t, result.Data["filtered"].([]any), 3)
	require.Equal(t, []any{map[string]any{"date": "2026-01-04"}}, result.Data["parent"])
	coverage := result.Extensions["typedRoots"].(map[string]typedRootCoverage)
	require.Equal(t, "capped", coverage["first"].Status)
	require.Equal(t, 2, *coverage["first"].NextOffset)
	require.Equal(t, "complete", coverage["next"].Status)
	require.False(t, *coverage["next"].HasMore)
}

func TestInterfaceRootSelectorsResidualSortAndOverlayParity(t *testing.T) {
	env := meetingRootEnv(t)
	for _, selector := range []string{"", `find: "Meeting",`, `property: {name: "group", value: "common"},`} {
		text := fmt.Sprintf(`{ meeting(%s first: 1, offset: 1, filters: [{field: "clientRef", value: "Acme"}], sort: [{field: "title", direction: desc}]) { date ... on NoteNode { path } } }`, selector)
		prepared, errs := Prepare(env.execSchema, text)
		require.Empty(t, errs)
		for _, overlay := range append([]*ReadOverlay{nil}, scalarParityOverlays(t, env)...) {
			deps := env.deps(nil)
			deps.ReadOverlay = overlay
			result := Execute(context.Background(), deps, env.schema, prepared)
			require.Empty(t, result.Errors)
			require.Equal(t, []any{map[string]any{"date": "2026-01-03", "path": "coaching/a.md"}}, result.Data["meeting"], selector)
			page := result.Extensions["typedRoots"].(map[string]typedRootCoverage)["meeting"]
			require.Equal(t, "capped", page.Status)
			require.Equal(t, 2, *page.NextOffset)
		}
	}
}

func TestInterfaceRootPathAndPrivateFieldRejection(t *testing.T) {
	env := meetingRootEnv(t)
	result := executeMeetingRoot(t, env, `{
	  found: meeting(path: "coaching/a.md") { date }
	  wrong: meeting(path: "other/a.md") { date }
	  past: meeting(path: "general/a.md", offset: 1) { date }
	}`)
	require.Equal(t, []any{map[string]any{"date": "2026-01-03"}}, result.Data["found"])
	require.Empty(t, result.Data["wrong"])
	require.Empty(t, result.Data["past"])
	for _, constraint := range []string{`filters: [{field: "privateField", value: "x"}]`, `sort: [{field: "privateField"}]`} {
		prepared, errs := Prepare(env.execSchema, `{ meeting(`+constraint+`) { date } }`)
		require.Empty(t, errs)
		result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
		require.NotEmpty(t, result.Errors)
		require.Contains(t, result.Errors[0].Message, "not a shared interface field")
	}
}

func TestInterfaceRootsExcludeMixedNodeKindsAndDetectCollisions(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface Mixed { name: String }
interface EmbeddedOnly { name: String }
interface Note { summary: String }
type Entry implements Mixed @node(paths: ["entries/*.md"]) { name: String }
type Item implements Mixed & EmbeddedOnly & Section @node(locator: EMBEDDED) @source(shape: LIST_ITEM, marker: "#item") { name: String @field }
`, nil)
	for _, root := range []string{"mixed", "embeddedOnly"} {
		require.Nil(t, env.execSchema.Schema.Query.Fields.ForName(root))
	}
	require.Equal(t, "NoteNode", env.execSchema.Schema.Query.Fields.ForName("note").Type.Name())
	for _, iface := range []string{"Search", "Entry"} {
		schema := *env.schema
		schema.Interfaces = map[string]*ontoschema.InterfaceType{iface: {Name: iface, Fields: []*ontoschema.Field{}}}
		schema.Types = map[string]*ontoschema.NoteType{"entry": {Name: "entry", Role: ontoschema.TypeRoleNote, Implements: []string{iface}}}
		_, err := BuildExecutableSchema(&schema)
		require.Error(t, err)
		require.Contains(t, err.Error(), "conflicts")
	}
}

func TestInterfaceRootSemanticTypeScopeAndQueryPlan(t *testing.T) {
	env := meetingRootEnv(t)
	prepared, errs := Prepare(env.execSchema, `{ meeting(semantic: ["coaching"], first: 1) { date ... on NoteNode { resolvedType score } } }`)
	require.Empty(t, errs)
	searcher := &interfaceSemanticSearcher{t: t}
	result := Execute(context.Background(), env.deps(searcher), env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, []any{map[string]any{"date": "2026-01-03", "resolvedType": "CoachingSession", "score": 0.9}}, result.Data["meeting"])
	result = executeMeetingRoot(t, env, `{ ontology { queryPlan(type: "Meeting", filters: [{field: "clientRef", value: "Acme"}], sort: [{field: "date", direction: desc}], select: ["date"]) { root execution projection pushedFilters pushedSort warnings { code } } } }`)
	plan := result.Data["ontology"].(map[string]any)["queryPlan"].(map[string]any)
	require.Equal(t, "meeting", plan["root"])
	require.Equal(t, "indexed", plan["execution"])
	require.Equal(t, "not_required", plan["projection"])
	require.Equal(t, []any{"clientRef eq"}, plan["pushedFilters"])
	require.Equal(t, []any{"date desc"}, plan["pushedSort"])
	require.Empty(t, plan["warnings"])
}

type interfaceSemanticSearcher struct{ t *testing.T }

func (s *interfaceSemanticSearcher) Search(context.Context, semantic.SearchRequest) ([]semantic.Result, error) {
	s.t.Fatal("interface root must use type-scoped semantic retrieval")
	return nil, nil
}

func (s *interfaceSemanticSearcher) SurveyNodesByType(_ context.Context, req semantic.SurveyNodesByTypeRequest) ([]semantic.Result, error) {
	require.Equal(s.t, []string{"CoachingSession", "GeneralMeeting"}, req.TypeNames)
	return []semantic.Result{{SourceRef: search.SourceRef{NoteID: "coaching/a.md"}, Score: 0.9}}, nil
}

func TestInterfaceRootResidualCandidateCoverage(t *testing.T) {
	env := meetingRootEnv(t)
	prepared, errs := Prepare(env.execSchema, `{ meeting(first: 1, filters: [{field: "clientRef", op: contains, value: "absent"}]) { date } }`)
	require.Empty(t, errs)
	store := &interfaceCappedStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = store
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Empty(t, result.Data["meeting"])
	page := result.Extensions["typedRoots"].(map[string]typedRootCoverage)["meeting"]
	require.Equal(t, "unknown_candidate_bound", page.Status)
	require.Nil(t, page.HasMore)
	require.Nil(t, page.NextOffset)
}

type interfaceCappedStore struct{ Store }

func (s *interfaceCappedStore) OntologyNodesByTypePlan(ctx context.Context, plan readmodel.NodeQueryPlan) ([]readmodel.NodeRow, error) {
	rows, err := s.Store.OntologyNodesByTypePlan(ctx, plan)
	if err != nil || len(rows) == 0 {
		return rows, err
	}
	out := make([]readmodel.NodeRow, plan.Limit)
	for i := range out {
		out[i] = rows[i%len(rows)]
	}
	return out, nil
}
