package identifierreconcile

import (
	"sort"

	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func singletonMembership(key string) []string { return []string{key} }

func unionIdenticalIntentMemberships(components []CollisionRepairIntent) {
	rewrites := make(map[string]map[string]struct{})
	fields := make(map[string]map[string]struct{})
	aliases := make(map[string]map[string]struct{})
	links := make(map[string]map[string]struct{})
	moves := make(map[string]map[string]struct{})
	diagnostics := make(map[string]map[string]struct{})
	for _, component := range components {
		seed := component.MembershipKeys[0]
		for _, intent := range component.Rewrites {
			addIntentMembership(rewrites, jsonKey(intent.Rewrite), seed)
		}
		for _, intent := range component.FieldEdits {
			addIntentMembership(fields, jsonKey(intent.Edit), seed)
		}
		for _, intent := range component.AliasEdits {
			addIntentMembership(aliases, jsonKey(intent.Edit), seed)
		}
		for _, intent := range component.LinkEdits {
			addIntentMembership(links, jsonKey(intent.Edit), seed)
		}
		for _, intent := range component.Moves {
			addIntentMembership(moves, jsonKey(intent.Move), seed)
		}
		for _, diagnostic := range component.Diagnostics {
			addIntentMembership(diagnostics, repairDiagnosticPhysicalKey(diagnostic), seed)
		}
	}
	for componentIndex := range components {
		component := &components[componentIndex]
		for index := range component.Rewrites {
			component.Rewrites[index].MembershipKeys = sortedMemberships(rewrites[jsonKey(component.Rewrites[index].Rewrite)])
		}
		for index := range component.FieldEdits {
			component.FieldEdits[index].MembershipKeys = sortedMemberships(fields[jsonKey(component.FieldEdits[index].Edit)])
		}
		for index := range component.AliasEdits {
			component.AliasEdits[index].MembershipKeys = sortedMemberships(aliases[jsonKey(component.AliasEdits[index].Edit)])
		}
		for index := range component.LinkEdits {
			component.LinkEdits[index].MembershipKeys = sortedMemberships(links[jsonKey(component.LinkEdits[index].Edit)])
		}
		for index := range component.Moves {
			component.Moves[index].MembershipKeys = sortedMemberships(moves[jsonKey(component.Moves[index].Move)])
		}
		for index := range component.Diagnostics {
			component.Diagnostics[index].MembershipKeys = sortedMemberships(diagnostics[repairDiagnosticPhysicalKey(component.Diagnostics[index])])
		}
	}
}

func repairDiagnosticPhysicalKey(diagnostic RepairDiagnostic) string {
	return jsonKey(struct {
		Kind         string                                 `json:"kind"`
		Blocking     bool                                   `json:"blocking"`
		Field        *reference.IdentifierRewriteDiagnostic `json:"field,omitempty"`
		Link         *reference.LinkRewriteDiagnostic       `json:"link,omitempty"`
		MoveConflict *obsidian.MoveConflict                 `json:"moveConflict,omitempty"`
	}{diagnostic.Kind, diagnostic.Blocking, diagnostic.Field, diagnostic.Link, diagnostic.MoveConflict})
}

func addIntentMembership(index map[string]map[string]struct{}, physicalKey, membership string) {
	if index[physicalKey] == nil {
		index[physicalKey] = make(map[string]struct{})
	}
	index[physicalKey][membership] = struct{}{}
}

func sortedMemberships(input map[string]struct{}) []string {
	out := make([]string, 0, len(input))
	for key := range input {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func dedupeFieldIntents(input []FieldRepairIntent) []FieldRepairIntent {
	out := input[:0]
	for _, intent := range input {
		if len(out) == 0 || jsonKey(out[len(out)-1].Edit) != jsonKey(intent.Edit) {
			out = append(out, intent)
		}
	}
	return out
}

func dedupeLinkIntents(input []LinkRepairIntent) []LinkRepairIntent {
	out := input[:0]
	for _, intent := range input {
		if len(out) == 0 || jsonKey(out[len(out)-1].Edit) != jsonKey(intent.Edit) {
			out = append(out, intent)
		}
	}
	return out
}
