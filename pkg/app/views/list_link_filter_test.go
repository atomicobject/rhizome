package views

import (
	"context"
	"sort"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

// A list link field matches a target it holds alongside others, pushed or
// residual, and missing treats an empty list as no value.
func TestListLinkFiltersMatchMembership(t *testing.T) {
	files := map[string]string{}
	for path, body := range laneFixture {
		files[path] = body
	}
	files["tasks/f.md"] = "---\nstatus: planned\nproject: []\n---\n# F\n"
	service, _ := newTypeViewsService(t, files)
	id := viewconfig.GeneratedTypeID("Task")
	titles := func(req ExecuteRequest) []string {
		t.Helper()
		resp, err := service.Execute(context.Background(), id, req)
		require.NoError(t, err)
		out := rowTitles(resp.Rows)
		sort.Strings(out)
		return out
	}
	for _, filter := range []viewconfig.FilterSpec{
		{Field: "project", Op: "eq", Value: "projects/apollo.md"},
		{Field: "project", Op: "in", Values: []string{"projects/apollo.md"}},
		{Field: "project", Op: "eq", Value: "[[apollo]]"},
	} {
		require.Equal(t, []string{"A", "B", "D"}, titles(ExecuteRequest{Filters: []viewconfig.FilterSpec{filter}}), filter)
		require.Equal(t, []string{"A", "B", "D"}, titles(ExecuteRequest{
			Filters: []viewconfig.FilterSpec{filter},
			Sort:    []viewconfig.SortSpec{{Field: "summary"}},
		}), "residual: %v", filter)
	}
	require.Equal(t, []string{"E", "F"}, titles(ExecuteRequest{Filters: []viewconfig.FilterSpec{{Field: "project", Op: "missing"}}}))
}
