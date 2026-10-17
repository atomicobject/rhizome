package web

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	"sort"
	"strings"
)

func touchedPathsFromOps(ops []OntologyEditOp) []string {
	set := map[string]struct{}{}
	for _, op := range ops {
		path, _, _ := splitLinkFragment(strings.TrimSpace(op.Path))
		if path == "" {
			path = strings.TrimSpace(op.Path)
		}
		if path != "" {
			set[path] = struct{}{}
		}
	}
	return sortedStringSet(set)
}

func touchedNodesFromOps(ops []OntologyEditOp) []string {
	set := map[string]struct{}{}
	for _, op := range ops {
		if path := strings.TrimSpace(op.Path); path != "" {
			set[path] = struct{}{}
		}
	}
	return sortedStringSet(set)
}

func touchedNodeRefsFromOps(ops []OntologyEditOp) []ontology.NodeRef {
	if len(ops) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]ontology.NodeRef, 0, len(ops))
	for _, op := range ops {
		ref, err := ontologyNodeRefFromOp(op)
		if err != nil {
			continue
		}
		key := canonicalNodeRefKey(ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		return canonicalNodeRefKey(out[i]) < canonicalNodeRefKey(out[j])
	})
	return out
}

func canonicalNodeRefKey(ref ontology.NodeRef) string {
	return strings.Join([]string{
		strings.TrimSpace(ref.NotePath),
		strings.TrimSpace(ref.Fragment),
		strings.TrimSpace(ref.NodeID),
		string(ref.Kind),
		strings.TrimSpace(ref.Structural),
	}, "|")
}

func changedFieldsByNode(ops []OntologyEditOp) map[string][]string {
	return changedFieldsByKey(ops, func(op OntologyEditOp) string {
		return strings.TrimSpace(op.Path)
	})
}

func changedFieldsByNodeRef(ops []OntologyEditOp) map[string][]string {
	return changedFieldsByKey(ops, func(op OntologyEditOp) string {
		ref, err := ontologyNodeRefFromOp(op)
		if err != nil {
			return ""
		}
		return canonicalNodeRefKey(ref)
	})
}

func changedFieldsByKey(ops []OntologyEditOp, keyForOp func(OntologyEditOp) string) map[string][]string {
	out := map[string][]string{}
	seen := map[string]map[string]struct{}{}
	for _, op := range ops {
		key := keyForOp(op)
		if key == "" {
			continue
		}
		field := strings.TrimSpace(op.Field)
		if field == "" {
			continue
		}
		if seen[key] == nil {
			seen[key] = map[string]struct{}{}
		}
		if _, ok := seen[key][field]; ok {
			continue
		}
		seen[key][field] = struct{}{}
		out[key] = append(out[key], field)
	}
	for key := range out {
		sort.Strings(out[key])
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func collectionChangesFromOps(ops []OntologyEditOp) []OntologyCollectionChange {
	out := make([]OntologyCollectionChange, 0)
	for _, op := range ops {
		switch strings.TrimSpace(op.Kind) {
		case "addEmbeddedNode", "reorderCollection", "deleteNode":
			collection := strings.TrimSpace(op.Collection)
			if collection == "" && strings.TrimSpace(op.Kind) == "deleteNode" {
				continue
			}
			ref, _ := ontologyNodeRefFromOp(op)
			var refPtr *ontology.NodeRef
			if !ref.IsZero() {
				refCopy := ref
				refPtr = &refCopy
			}
			out = append(out, OntologyCollectionChange{
				Path:             strings.TrimSpace(op.Path),
				Ref:              refPtr,
				Collection:       collection,
				OrderedFragments: append([]string(nil), op.OrderedFragments...),
			})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func baseFingerprintsFromPlan(plan ontology.CommitPlan) map[string]string {
	if len(plan.Files) == 0 {
		return nil
	}
	out := make(map[string]string, len(plan.Files))
	for _, file := range plan.Files {
		out[file.NotePath] = file.BaseFingerprint
	}
	return out
}

func stalePathsFromPlan(plan ontology.CommitPlan) []string {
	set := map[string]struct{}{}
	for _, file := range plan.Files {
		if file.Rebased {
			set[file.NotePath] = struct{}{}
		}
	}
	return sortedStringSet(set)
}

func planHasRebased(plan ontology.CommitPlan) bool {
	for _, file := range plan.Files {
		if file.Rebased {
			return true
		}
	}
	return false
}

func sortedStringSet(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func mergeStringSets(values ...[]string) []string {
	set := map[string]struct{}{}
	for _, group := range values {
		for _, value := range group {
			if strings.TrimSpace(value) == "" {
				continue
			}
			set[value] = struct{}{}
		}
	}
	return sortedStringSet(set)
}
