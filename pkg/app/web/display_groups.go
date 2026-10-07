package web

import (
	"context"
	"errors"
	"net/http"
	"sort"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

// DisplayGroupsResponse lists every effective display group (SPEC-0111) with
// the members the Notes rail shows under it.
type DisplayGroupsResponse struct {
	Groups []DisplayGroup `json:"groups"`
}

type DisplayGroup struct {
	Name    string               `json:"name"`
	Members []DisplayGroupMember `json:"members"`
}

// DisplayGroupMember is a type or interface in a group. Children are the
// members nested under it: implementors an interface claims and types that name
// it as their display parent. IssueCount is the published validation issues in
// the member's type or interface scope, the count its scoped issues panel opens
// on.
type DisplayGroupMember struct {
	Name         string               `json:"name"`
	Kind         string               `json:"kind"`
	Label        string               `json:"label"`
	PluralLabel  string               `json:"pluralLabel"`
	Description  string               `json:"description,omitempty"`
	Count        int                  `json:"count"`
	IssueCount   int                  `json:"issueCount"`
	Implementors []string             `json:"implementors"`
	Children     []DisplayGroupMember `json:"children"`
}

func (s *Server) handleDisplayGroups(w http.ResponseWriter, r *http.Request) {
	summary, err := s.ontologySummary(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defs, err := s.ontologyDefinitions()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var schema *ontology.Schema
	if defs != nil {
		schema = defs.schema
	}
	groups := displayGroups(schema, summary)
	if err := s.countDisplayGroupIssues(r.Context(), groups); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, DisplayGroupsResponse{Groups: groups})
}

// countDisplayGroupIssues sets every member's IssueCount from the published
// validation generation with one scope summary read per batch, resolving
// interface scopes through their implementors as the validation handlers do.
// Counts stay 0 until validation publishes.
func (s *Server) countDisplayGroupIssues(ctx context.Context, groups []DisplayGroup) error {
	var members []*DisplayGroupMember
	var collect func(list []DisplayGroupMember)
	collect = func(list []DisplayGroupMember) {
		for i := range list {
			members = append(members, &list[i])
			collect(list[i].Children)
		}
	}
	for i := range groups {
		collect(groups[i].Members)
	}
	scopes := make([]semdb.ValidationScope, len(members))
	for i, member := range members {
		scopes[i] = semdb.ValidationScope{Kind: member.Kind, Key: member.Name}
	}
	counts, err := s.validationScopeIssueCounts(ctx, scopes, s.validationInterfaceImplementors())
	if err != nil {
		return err
	}
	for i, count := range counts {
		members[i].IssueCount = count
	}
	return nil
}

// validationScopeIssueCounts uses the same published scopes as the issues panel.
// Counts stay zero until validation publishes; an expired generation retries once.
func (s *Server) validationScopeIssueCounts(ctx context.Context, scopes []semdb.ValidationScope, implementors map[string][]string) ([]int, error) {
	counts := make([]int, len(scopes))
	store := s.runtime.Intel()
	if store == nil || len(scopes) == 0 {
		return counts, nil
	}
	read := func() error {
		state, err := store.GetValidationState(ctx)
		if err != nil || state.PublishedGeneration <= 0 {
			return err
		}
		filter := semdb.ValidationDiagnosticFilter{InterfaceImplementors: implementors}
		for start := 0; start < len(scopes); start += semdb.ValidationScopeBatchMax {
			batch := scopes[start:min(start+semdb.ValidationScopeBatchMax, len(scopes))]
			response, err := store.GetValidationScopeSummaries(ctx, semdb.ValidationScopeSummaryRequest{Generation: state.PublishedGeneration, Scopes: batch, Filter: filter})
			if err != nil {
				return err
			}
			for i, summary := range response.Summaries {
				counts[start+i] = summary.IssueCount
			}
		}
		return nil
	}
	err := read()
	if errors.Is(err, semdb.ErrValidationGenerationExpired) {
		err = read()
	}
	return counts, err
}

// displayGroups arranges the summary's members with viewconfig.DisplayTree,
// the membership rule group mounts and the Notes rail share. Ungrouped roots
// form the rail's "Other" group, which the catalog targets like any group and
// which absorbs an authored group of that name; like the rail, it sorts last.
func displayGroups(schema *ontology.Schema, summary OntologySummaryResponse) []DisplayGroup {
	members := map[string]DisplayGroupMember{}
	for _, item := range summary.Types {
		members[item.Name] = displayGroupMember(item.Name, "type", item.Label, item.PluralLabel, item.Description, item.Count, nil)
	}
	for _, item := range summary.Interfaces {
		members[item.Name] = displayGroupMember(item.Name, "interface", item.Label, item.PluralLabel, item.Description, item.Count, item.Implementors)
	}
	tree := viewconfig.DisplayTree(schema)
	var build func(name string) DisplayGroupMember
	build = func(name string) DisplayGroupMember {
		member, ok := members[name]
		if !ok {
			member = displayGroupMember(name, "type", "", "", "", 0, nil)
		}
		member.Children = []DisplayGroupMember{}
		for _, child := range tree.Children(name) {
			member.Children = append(member.Children, build(child))
		}
		sortDisplayGroupMembers(member.Children)
		return member
	}
	groups := []DisplayGroup{}
	for name, roots := range tree.Roots {
		group := DisplayGroup{Name: name, Members: []DisplayGroupMember{}}
		for _, root := range roots {
			group.Members = append(group.Members, build(root))
		}
		sortDisplayGroupMembers(group.Members)
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool {
		if (groups[i].Name == "Other") != (groups[j].Name == "Other") {
			return groups[j].Name == "Other"
		}
		return appviews.RailLess(groups[i].Name, groups[j].Name)
	})
	return groups
}

func displayGroupMember(name, kind, label, plural, description string, count int, implementors []string) DisplayGroupMember {
	label = firstNonEmpty(label, name)
	if implementors == nil {
		implementors = []string{}
	}
	return DisplayGroupMember{
		Name: name, Kind: kind, Label: label, PluralLabel: firstNonEmpty(plural, label), Description: description,
		Count: count, Implementors: implementors, Children: []DisplayGroupMember{},
	}
}

// sortDisplayGroupMembers orders members by the label the rail shows, the
// order the bundled views and Trace applicability claim types in.
func sortDisplayGroupMembers(members []DisplayGroupMember) {
	sort.SliceStable(members, func(i, j int) bool {
		if members[i].PluralLabel == members[j].PluralLabel {
			return members[i].Name < members[j].Name
		}
		return appviews.RailLess(members[i].PluralLabel, members[j].PluralLabel)
	})
}
