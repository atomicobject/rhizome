package pushdown

import (
	"context"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
	"testing"
)

type linkTargetCatalogFixture struct {
	rows          []readmodel.NodeRow
	planCalls     int
	pathCalls     int
	aliasPaths    []string
	fieldReadCall int
	aliasErr      error
}

func (s *linkTargetCatalogFixture) OntologyNodesByTypePlan(_ context.Context, plan readmodel.NodeQueryPlan) ([]readmodel.NodeRow, error) {
	s.planCalls++
	if plan.Limit < len(s.rows) {
		return s.rows[:plan.Limit], nil
	}
	return s.rows, nil
}

func (s *linkTargetCatalogFixture) OntologyNodeFieldValuesByNodeIDs(_ context.Context, _ []string, _ []string) ([]readmodel.FieldValueRow, error) {
	s.fieldReadCall++
	return nil, nil
}

func (s *linkTargetCatalogFixture) OntologyNodesByPaths(_ context.Context, paths []string) ([]readmodel.NodeRow, error) {
	s.pathCalls++
	var out []readmodel.NodeRow
	for _, row := range s.rows {
		for _, path := range paths {
			if row.NotePath == path {
				out = append(out, row)
			}
		}
	}
	return out, nil
}

func (s *linkTargetCatalogFixture) CurrentNotePropertyValues(_ context.Context, paths []string, _ []string, _ semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error) {
	s.aliasPaths = append([]string(nil), paths...)
	return nil, s.aliasErr
}

func TestCatalogLinkResolver_DoesNotClaimUniquenessWhenAliasReadFails(t *testing.T) {
	field := &ontoschema.Field{Name: "opportunities", TypeName: "Opportunity"}
	schema := &ontoschema.Schema{Types: map[string]*ontoschema.NoteType{"Opportunity": {Name: "Opportunity"}}}
	store := &linkTargetCatalogFixture{
		rows:     []readmodel.NodeRow{{NodeID: "opportunity-1", NodeKind: string(ontology.NodeKindNote), NotePath: "notes/one.md", TypeName: "Opportunity", Title: "One"}},
		aliasErr: errors.New("alias index unavailable"),
	}
	got, warning := NewCatalogLinkResolver(schema, store).ResolveLinkTarget(context.Background(), field, "One")
	require.Empty(t, got)
	require.Equal(t, "link_filter_resolution_failed", warning.Code)
	require.Contains(t, warning.Message, "alias index unavailable")
}

func TestCatalogLinkResolver_CanonicalPathUsesNoteIdentityForAnyProvider(t *testing.T) {
	field := &ontoschema.Field{Name: "opportunities", TypeName: "Opportunity"}
	schema := &ontoschema.Schema{Types: map[string]*ontoschema.NoteType{"Opportunity": {Name: "Opportunity"}}}
	store := &linkTargetCatalogFixture{rows: []readmodel.NodeRow{{
		NodeID: "opportunity-1", NodeKind: string(ontology.NodeKindNote),
		NotePath: "data/opportunity.json", TypeName: "Opportunity",
	}}}
	resolver := NewCatalogLinkResolver(schema, store)
	got, warning := resolver.ResolveLinkTarget(context.Background(), field, "data/opportunity.json")
	require.Empty(t, warning.Code)
	require.Equal(t, "data/opportunity.json", got)
	require.Zero(t, store.planCalls)

	store.rows[0].NodeKind = string(ontology.NodeKindEmbedded)
	_, _, handled := resolver.canonicalPathTarget(context.Background(), field, "data/opportunity.json")
	require.False(t, handled, "a contained node does not establish a note-root path target")
}

func TestCatalogLinkResolver_BoundedAndReusedAcrossFilterValues(t *testing.T) {
	field := &ontoschema.Field{Name: "opportunities", TypeName: "Opportunity"}
	schema := &ontoschema.Schema{Types: map[string]*ontoschema.NoteType{"Opportunity": {Name: "Opportunity"}}}
	rows := make([]readmodel.NodeRow, 1002)
	for i := range rows {
		rows[i] = readmodel.NodeRow{
			NodeID: fmt.Sprintf("opportunity-%04d", i), NotePath: fmt.Sprintf("notes/opportunity-%04d.md", i),
			NodeKind: string(ontology.NodeKindNote), TypeName: "Opportunity", Title: fmt.Sprintf("Opportunity %04d", i),
		}
	}
	store := &linkTargetCatalogFixture{rows: rows}
	resolver := NewCatalogLinkResolver(schema, store)
	got, warning := resolver.ResolveLinkTarget(context.Background(), field, "Opportunity 1001")
	require.Empty(t, warning.Code)
	require.Equal(t, "notes/opportunity-1001.md", got)
	got, warning = resolver.ResolveLinkTarget(context.Background(), field, "Opportunity 1000")
	require.Empty(t, warning.Code)
	require.Equal(t, "notes/opportunity-1000.md", got)
	got, warning = resolver.ResolveLinkTarget(context.Background(), field, "[[notes/opportunity-1001.md]]")
	require.Empty(t, warning.Code)
	require.Equal(t, "notes/opportunity-1001.md", got)
	require.Equal(t, 1, store.planCalls, "several values for one target type reuse its catalog read")
	require.Equal(t, 1, store.fieldReadCall)
	require.Len(t, store.aliasPaths, len(rows), "frontmatter alias lookup is scoped to candidates")
	require.Equal(t, 3, store.pathCalls, "every literal uses the indexed lookup before title and alias discovery")
}

func TestCatalogLinkResolver_ReportsIncompleteAboveCandidateBound(t *testing.T) {
	field := &ontoschema.Field{Name: "opportunities", TypeName: "Opportunity"}
	schema := &ontoschema.Schema{Types: map[string]*ontoschema.NoteType{"Opportunity": {Name: "Opportunity"}}}
	rows := make([]readmodel.NodeRow, LinkTargetCandidateLimit+1)
	for i := range rows {
		rows[i] = readmodel.NodeRow{NodeID: fmt.Sprintf("opportunity-%04d", i), NotePath: fmt.Sprintf("notes/opportunity-%04d.md", i), NodeKind: string(ontology.NodeKindNote), TypeName: "Opportunity"}
	}
	store := &linkTargetCatalogFixture{rows: rows}
	resolver := NewCatalogLinkResolver(schema, store)
	got, warning := resolver.ResolveLinkTarget(context.Background(), field, "notes/opportunity-5000.md")
	require.Empty(t, warning.Code)
	require.Equal(t, "notes/opportunity-5000.md", got)
	require.Zero(t, store.planCalls, "an exact path beyond the candidate cap does not scan the catalog")
	for _, value := range []string{"missing", "another missing title"} {
		got, warning := resolver.ResolveLinkTarget(context.Background(), field, value)
		require.Empty(t, got)
		require.Equal(t, "link_filter_resolution_incomplete", warning.Code)
		require.Contains(t, warning.Message, "more than 5000 indexed candidates")
	}
	require.Equal(t, 1, store.planCalls)
	require.Zero(t, store.fieldReadCall, "incomplete candidates must not produce an answer")
}
