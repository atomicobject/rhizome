package ontology

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// TypedBodyLinkTarget narrows the highest-precedence candidates by the declared
// relation type before requiring uniqueness. Untyped prose keeps its own rules.
func TypedBodyLinkTarget(schema *ontoschema.Schema, candidates []obsidian.ResolvedNoteTarget, expected string, resolveType func(string) string) (string, bool) {
	var target string
	for _, candidate := range candidates {
		if resolveType == nil {
			continue
		}
		actual := resolveType(candidate.Path)
		if actual == "" || (expected != "" && !ontoschema.TypeMatchesOrImplements(schema, actual, expected)) {
			continue
		}
		if target != "" && target != candidate.Path {
			return "", false
		}
		target = candidate.Path
	}
	return target, target != ""
}

func BodyLinkCandidates(cache *obsidian.NotePathCache, syntax, target, from string) []obsidian.ResolvedNoteTarget {
	if syntax == "mdlink" {
		return cache.ResolveMdLinkCandidates(target, from)
	}
	return cache.ResolveNoteCandidates(target)
}
