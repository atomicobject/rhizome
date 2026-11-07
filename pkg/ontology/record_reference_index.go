package ontology

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
)

type recordReferenceIndex struct {
	exact    map[string][]recordReferenceCandidate
	names    map[string][]recordReferenceCandidate
	physical map[string][]NodeRef
}

func referenceLookupKey(space, typ, key string) string { return recordDigest(space, typ, key) }
func buildRecordReferenceIndex(candidates []recordReferenceCandidate) *recordReferenceIndex {
	index := &recordReferenceIndex{exact: map[string][]recordReferenceCandidate{}, names: map[string][]recordReferenceCandidate{}}
	paths := map[string]ontoschema.RecordSelector{}
	for _, c := range candidates {
		for _, field := range c.record.typ.Fields {
			if field.Record != nil && field.Record.References != nil && field.Record.References.MatchPath.Text != "" {
				path := field.Record.References.MatchPath
				paths[path.Text] = path
			}
		}
	}
	for _, candidate := range candidates {
		record := candidate.record
		types := []string{record.typ.Name}
		for name := range candidate.document.schema.Interfaces {
			if ontoschema.TypeMatchesOrImplements(candidate.document.schema, record.typ.Name, name) {
				types = append(types, name)
			}
		}
		for _, name := range types {
			key := referenceLookupKey(record.identity.Space, name, record.identity.LogicalKey)
			index.exact[key] = append(index.exact[key], candidate)
			for _, path := range paths {
				if value, ok := candidate.document.scalarAt(record, path); ok {
					key := referenceLookupKey(record.identity.Space, name, path.Text+"\x00"+strings.ToLower(strings.TrimSpace(value)))
					index.names[key] = append(index.names[key], candidate)
				}
			}
		}
	}
	return index
}
func (index *recordReferenceIndex) resolve(space string, types []string, id string, matchPath ontoschema.RecordSelector) []recordReferenceCandidate {
	collect := func(source map[string][]recordReferenceCandidate, key string) []recordReferenceCandidate {
		var out []recordReferenceCandidate
		seen := map[*recordProjection]bool{}
		for _, typ := range types {
			for _, candidate := range source[referenceLookupKey(space, typ, key)] {
				if !seen[candidate.record] {
					seen[candidate.record] = true
					out = append(out, candidate)
				}
			}
		}
		return out
	}
	matches := collect(index.exact, id)
	if len(matches) == 0 && matchPath.Text != "" {
		matches = collect(index.names, matchPath.Text+"\x00"+strings.ToLower(strings.TrimSpace(id)))
	}
	return matches
}

// Explicit Markdown host paths use canonical physical identity, never a global
// alias/name fallback that could select an unselected product's evidence.
func (index *recordReferenceIndex) addMarkdownTargets(documents []*PreparedDocument) {
	index.physical = map[string][]NodeRef{}
	for _, document := range documents {
		if document == nil || document.markdown == nil {
			continue
		}
		node := document.rootProjection
		if node == nil || node.Type == nil {
			continue
		}
		names := []string{node.Type.Name}
		for name := range document.schema.Interfaces {
			if ontoschema.TypeMatchesOrImplements(document.schema, node.Type.Name, name) {
				names = append(names, name)
			}
		}
		for _, name := range names {
			key := referenceLookupKey("", name, node.Ref.NotePath)
			index.physical[key] = append(index.physical[key], node.Ref)
		}
	}
}
func (index *recordReferenceIndex) physicalTargets(types []string, id string) []NodeRef {
	var out []NodeRef
	seen := map[NodeRef]bool{}
	for _, typ := range types {
		for _, ref := range index.physical[referenceLookupKey("", typ, id)] {
			if !seen[ref] {
				seen[ref] = true
				out = append(out, ref)
			}
		}
	}
	return out
}
