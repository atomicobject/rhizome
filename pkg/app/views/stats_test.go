package views

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func TestExecutionStatsSummarizeEveryMatchingRowBeforePaging(t *testing.T) {
	root := writeTypeViewsFixture(t, map[string]string{
		"projects/apollo.md": "---\nstatus: live\n---\n# Apollo\n",
		"tasks/a.md":         "---\nstatus: doing\nproject: ['[[apollo]]']\n---\n# A\n",
		"tasks/b.md":         "---\nstatus: doing\nsummary: Second\n---\n# B\n",
		"tasks/c.md":         "---\nstatus: planned\npriority: high\n---\n# C\n",
		"tasks/d.md":         "---\nstatus: shipped\n---\n# D\n",
		"tasks/e.md":         "---\nstatus: cancelled\n---\n# E\n",
		"tasks/f.md":         "---\ntitle: F\n---\n# F\n",
	})
	old := time.Now().Add(-60 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(root, "tasks/a.md"), old, old))
	service, _ := indexTypeViewsFixture(t, root)

	resp, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Task"), ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "status", Op: "neq", Value: "cancelled"}},
		Page:    PageRequest{First: 1},
	})
	require.NoError(t, err)

	require.Len(t, resp.Rows, 1)
	require.NotNil(t, resp.Stats)
	require.Equal(t, 5, resp.Stats.Total)
	require.Equal(t, 1, resp.Stats.StaleCount, "only the doing task unchanged for 60 days")
	require.Equal(t, []StatsValueCount{{Value: "planned", Count: 1}, {Value: "doing", Count: 2}, {Value: "shipped", Count: 1}}, resp.Stats.Lifecycle)
	require.Empty(t, resp.Stats.Kinds)
	fills := map[string]int{}
	var order []string
	for _, field := range resp.Stats.Fields {
		fills[field.Field] = field.Filled
		order = append(order, field.Field)
	}
	require.Equal(t, []string{"status", "priority", "owner", "project", "due"}, order, "generated column fields, then KEY fields")
	require.Equal(t, map[string]int{"status": 4, "priority": 1, "owner": 0, "project": 1, "due": 0}, fills)
	require.Nil(t, resp.Stats.Dates, "Task has a lifecycle, so no primary date")
}

func TestExecutionStatsCountKindsAndPrimaryDates(t *testing.T) {
	service, _ := newTypeViewsService(t, map[string]string{
		"meetings/a.md":  "---\nheld: 2026-08-14\n---\n# A\n",
		"meetings/b.md":  "---\nheld: 2026-09-02\n---\n# B\n",
		"workshops/c.md": "---\nheld: 2026-09-30\n---\n# C\n",
		"workshops/d.md": "---\ntitle: D\n---\n# D\n",
	})

	resp, err := service.Execute(context.Background(), viewconfig.GeneratedInterfaceID("Event"), ExecuteRequest{})
	require.NoError(t, err)

	require.Equal(t, []StatsValueCount{{Value: "Meeting", Count: 2}, {Value: "Workshop", Count: 2}}, resp.Stats.Kinds)
	require.Equal(t, &StatsDateRange{
		Field: "held", First: "2026-08-14", Last: "2026-09-30",
		Months: []StatsValueCount{{Value: "2026-08", Count: 1}, {Value: "2026-09", Count: 2}},
	}, resp.Stats.Dates)
	require.Equal(t, []StatsFieldFill{{Field: "held", Filled: 3}}, resp.Stats.Fields)
}

func TestExecutionStatsSayWhenTheSourceCapCutRows(t *testing.T) {
	service, _ := newTypeViewsService(t, saveFixture)
	id := viewconfig.GeneratedTypeID("Task")

	capped, err := service.Execute(context.Background(), id, ExecuteRequest{Variant: "table", Source: SourceRequest{MaxRows: 1}})
	require.NoError(t, err)
	require.True(t, capped.Stats.Truncated, "statistics cover only the capped rows")
	whole, err := service.Execute(context.Background(), id, ExecuteRequest{Variant: "table"})
	require.NoError(t, err)
	require.False(t, whole.Stats.Truncated)
}

func TestStaleFilterMatchesTheRowsStatsCountAsStale(t *testing.T) {
	root := writeTypeViewsFixture(t, map[string]string{
		"tasks/a.md": "---\nstatus: doing\n---\n# A\n",
		"tasks/b.md": "---\nstatus: doing\n---\n# B\n",
		"tasks/c.md": "---\nstatus: planned\n---\n# C\n",
	})
	old := time.Now().Add(-60 * 24 * time.Hour)
	for _, name := range []string{"a", "c"} {
		require.NoError(t, os.Chtimes(filepath.Join(root, "tasks", name+".md"), old, old))
	}
	service, _ := indexTypeViewsFixture(t, root)
	titles := func(value string) ([]string, int) {
		resp, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Task"), ExecuteRequest{
			Variant: "table",
			Filters: []viewconfig.FilterSpec{{Field: "stale", Op: "eq", Value: value}},
		})
		require.NoError(t, err)
		var out []string
		for _, row := range resp.Rows {
			out = append(out, row.Title)
		}
		sort.Strings(out)
		return out, resp.Stats.StaleCount
	}

	stale, staleCount := titles("true")
	require.Equal(t, []string{"A"}, stale, "an old planned task is not stale")
	require.Equal(t, 1, staleCount)
	fresh, _ := titles("false")
	require.Equal(t, []string{"B", "C"}, fresh)
	_, err := service.Execute(context.Background(), viewconfig.GeneratedTypeID("Task"), ExecuteRequest{
		Variant: "table",
		Filters: []viewconfig.FilterSpec{{Field: "stale", Op: "contains", Value: "t"}},
	})
	require.ErrorIs(t, err, ErrInvalidRequest)
}
