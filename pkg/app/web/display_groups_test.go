package web

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/stretchr/testify/require"
)

// The shared rail fixture pins the Notes rail and group mounts to one
// membership answer; the display-groups endpoint must give the same one.
func TestDisplayGroupsMatchSharedRailFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "display-groups", "rail-groups.json"))
	require.NoError(t, err)
	type item struct {
		Name, Role, Label, PluralLabel, DisplayGroup, DisplayParent string
		Implements                                                  []string
	}
	var fixture struct {
		Types, Interfaces []item
		Expected          struct {
			Parents map[string]string
			Groups  []struct {
				Name  string
				Roots []string
			}
		}
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	schema := &ontology.Schema{Types: map[string]*ontology.NoteType{}, Interfaces: map[string]*ontology.InterfaceType{}}
	for _, it := range fixture.Types {
		schema.Types[it.Name] = &ontology.NoteType{Name: it.Name, Role: ontology.TypeRole(it.Role), Label: it.Label, PluralLabel: it.PluralLabel, DisplayGroup: it.DisplayGroup, DisplayParent: it.DisplayParent, Implements: it.Implements}
	}
	for _, it := range fixture.Interfaces {
		schema.Interfaces[it.Name] = &ontology.InterfaceType{Name: it.Name, Label: it.Label, PluralLabel: it.PluralLabel, DisplayGroup: it.DisplayGroup, DisplayParent: it.DisplayParent}
	}
	// The summary the endpoint reads: navigation members with their counts.
	typeNames, interfaces := viewconfig.NavigationMembers(schema)
	summary := OntologySummaryResponse{}
	for i, name := range typeNames {
		summary.Types = append(summary.Types, OntologyTypeSummary{Name: name, Label: schema.Types[name].Label, PluralLabel: schema.Types[name].PluralLabel, Count: 10 + i})
	}
	for name, implementors := range interfaces {
		summary.Interfaces = append(summary.Interfaces, OntologyInterfaceSummary{Name: name, PluralLabel: schema.Interfaces[name].PluralLabel, Count: 7, Implementors: implementors})
	}

	groups := displayGroups(schema, summary)

	var want []string
	for _, group := range fixture.Expected.Groups {
		want = append(want, group.Name)
	}
	var got []string
	parents := map[string]string{}
	var walk func(parent string, members []DisplayGroupMember)
	walk = func(parent string, members []DisplayGroupMember) {
		for _, member := range members {
			if parent != "" {
				parents[member.Name] = parent
			}
			walk(member.Name, member.Children)
		}
	}
	for i, group := range groups {
		got = append(got, group.Name)
		var roots []string
		for _, member := range group.Members {
			roots = append(roots, member.Name)
		}
		require.Equal(t, fixture.Expected.Groups[i].Roots, roots, group.Name)
		walk("", group.Members)
	}
	require.Equal(t, want, got, "groups sorted by name, with the rail's Other bucket last")
	require.Equal(t, fixture.Expected.Parents, parents)

	tracked := groups[2].Members[0]
	require.Equal(t, DisplayGroupMember{
		Name: "Tracked", Kind: "interface", Label: "Tracked", PluralLabel: "Tracked work", Count: 7,
		Implementors: []string{"Effort"},
		Children: []DisplayGroupMember{{
			Name: "Effort", Kind: "type", Label: "Effort", PluralLabel: "Efforts", Count: 13,
			Implementors: []string{}, Children: []DisplayGroupMember{},
		}},
	}, tracked)
}

// Every group target a view can open must resolve in /api/v1/display-groups:
// the bundled group views look their group up there by name. Ungrouped types
// form the rail's "Other" bucket, which the catalog targets like any group.
func TestEveryGroupTargetResolvesInDisplayGroups(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	assets := fstest.MapFS{}
	for id, file := range map[string]string{appviews.BriefingViewID: "briefing", appviews.SectionsViewID: "sections"} {
		assets["bundled-views/group/"+file+".yaml"] = &fstest.MapFile{Data: []byte("apiVersion: rhizome.view.v1\nid: " + id + "\nname: " + file +
			"\nsource: {kind: custom, entry: " + file + ".tsx}\nmount: {kind: group, group: \"*\"}\n")}
		assets["bundled-views/group/"+file+".tsx"] = &fstest.MapFile{Data: []byte("export default null")}
	}
	srv.assets = assets
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var catalog appviews.Catalog
	getJSON(t, httpSrv.URL+"/api/v1/views", &catalog)
	var targets []string
	for _, target := range catalog.Targets {
		if target.Kind == viewconfig.MountKindGroup {
			targets = append(targets, target.Name)
		}
	}
	require.Contains(t, targets, "Other", "ungrouped Plan and Research notes form the Other group")
	for _, target := range catalog.Targets {
		if target.Kind == viewconfig.MountKindGroup && target.Name == "Other" {
			require.Contains(t, target.DefaultChoiceID, appviews.BriefingViewID, "Other opens a bundled view by shape")
		}
	}

	var response DisplayGroupsResponse
	getJSON(t, httpSrv.URL+"/api/v1/display-groups", &response)
	byName := map[string]DisplayGroup{}
	var names []string
	for _, group := range response.Groups {
		byName[group.Name] = group
		names = append(names, group.Name)
	}
	for _, target := range targets {
		require.Contains(t, byName, target, "group target %s must resolve", target)
	}
	require.Equal(t, "Other", names[len(names)-1], "the rail lists Other last")
	var roots []string
	for _, member := range byName["Other"].Members {
		roots = append(roots, member.Name)
	}
	require.Contains(t, roots, "Plan")
	require.Contains(t, roots, "Research")
}

// publishTestDiagnostics publishes one complete validation generation holding
// diagnostics.
func publishTestDiagnostics(t *testing.T, store *semdb.Store, diagnostics ...semdb.ValidationDiagnostic) int64 {
	t.Helper()
	ctx := context.Background()
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	paths := map[string]bool{}
	checks := map[string]int{}
	for _, diagnostic := range diagnostics {
		for _, path := range diagnostic.AffectedPaths {
			paths[path] = true
		}
		checks[diagnostic.Check]++
	}
	snapshot := semdb.ValidationSnapshot{
		VaultIdentity: "vault", Generation: generation, Scope: "default", Completion: semdb.ValidationCompletionComplete,
		IssueCount: len(diagnostics), AffectedFileCount: len(paths), AffectedNoteCount: len(paths), Diagnostics: diagnostics,
	}
	for check, count := range checks {
		snapshot.SelectedChecks = append(snapshot.SelectedChecks, check)
		snapshot.Checks = append(snapshot.Checks, semdb.ValidationCheckSnapshot{Check: check, Outcome: semdb.ValidationCheckOutcomeCompleted, IssueCount: count})
	}
	published, err := store.PublishValidationSnapshot(ctx, snapshot)
	require.NoError(t, err)
	require.True(t, published)
	return generation
}

// memberIssueCounts returns every display-group member's issueCount and the
// issue count of its scope in /api/v1/validation/summaries, which the scoped
// issues panel reads.
func memberIssueCounts(t *testing.T, srv *Server, generation int64) (got, want map[string]int) {
	t.Helper()
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	var response DisplayGroupsResponse
	getJSON(t, httpSrv.URL+"/api/v1/display-groups", &response)
	got = map[string]int{}
	var scopes []semdb.ValidationScope
	var walk func(members []DisplayGroupMember)
	walk = func(members []DisplayGroupMember) {
		for _, member := range members {
			got[member.Name] = member.IssueCount
			scopes = append(scopes, semdb.ValidationScope{Kind: member.Kind, Key: member.Name})
			walk(member.Children)
		}
	}
	for _, group := range response.Groups {
		walk(group.Members)
	}
	var summaries semdb.ValidationScopeSummaryResponse
	postJSON(t, httpSrv.URL+"/api/v1/validation/summaries", semdb.ValidationScopeSummaryRequest{Generation: generation, Scopes: scopes}, &summaries)
	want = map[string]int{}
	for _, summary := range summaries.Summaries {
		want[summary.Scope.Key] = summary.IssueCount
	}
	return got, want
}

// A member's issueCount is what the scoped issues panel opens on: published
// issues in the member's type or interface scope, links and type-level issues
// included, not notes with ontology assessment issues.
func TestDisplayGroupMemberIssueCountsMatchPublishedScopes(t *testing.T) {
	t.Parallel()

	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	plan := "specs/100-demo/plan.md"
	link := func(key string) semdb.ValidationDiagnostic {
		return semdb.ValidationDiagnostic{IssueKey: key, Check: validate.CheckBrokenLinks, Code: "broken_link",
			PrimaryPath: plan, AffectedPaths: []string{plan}, AffectedNotePaths: []string{plan}}
	}
	generation := publishTestDiagnostics(t, srv.runtime.Intel(), link("link-a"), link("link-b"),
		semdb.ValidationDiagnostic{IssueKey: "cycle", Check: validate.CheckOntology, Code: "parent_cycle", AffectedTypes: []string{"Spec"}})

	got, want := memberIssueCounts(t, srv, generation)
	require.Equal(t, want, got)
	require.Equal(t, 2, got["Plan"], "both broken links in the plan count")
	require.Equal(t, 1, got["Spec"], "a type-level issue counts")
}

func TestDisplayGroupInterfaceIssueCountCoversImplementors(t *testing.T) {
	t.Parallel()

	fixture := prepareInterfaceFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	generation := publishTestDiagnostics(t, srv.runtime.Intel(), semdb.ValidationDiagnostic{IssueKey: "link", Check: validate.CheckBrokenLinks,
		Code: "broken_link", PrimaryPath: "specs/proc.md", AffectedPaths: []string{"specs/proc.md"}, AffectedNotePaths: []string{"specs/proc.md"}})

	got, want := memberIssueCounts(t, srv, generation)
	require.Equal(t, want, got)
	require.Equal(t, 1, got["SpecLike"], "an issue in a process spec counts for the interface it implements")
}
