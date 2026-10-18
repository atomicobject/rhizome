package ontology

import (
	"encoding/json"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func ontologyLinkTargetResolver(cache *obsidian.NotePathCache, resolveType func(string) string) OntologyLinkTargetResolver {
	if cache == nil {
		return nil
	}
	return OntologyLinkTargetResolverFunc(func(field *ontoschema.Field, targetInput string) (OntologyLinkTarget, bool) {
		targetInput = UnwrapLinkValue(targetInput)
		target, ok := cache.ResolveNoteTarget(targetInput)
		if !ok {
			return OntologyLinkTarget{}, false
		}
		targetType := ""
		if resolveType != nil {
			targetType = strings.TrimSpace(resolveType(target.Path))
		}
		ref := NodeRef{
			NotePath:  target.Path,
			Fragment:  target.Fragment,
			TypeName:  targetType,
			Kind:      NodeKindNote,
			StartByte: 0,
			EndByte:   0,
		}
		refJSON, err := json.Marshal(ref)
		if err != nil {
			return OntologyLinkTarget{}, false
		}
		return OntologyLinkTarget{
			NotePath:      ref.NotePath,
			TypeName:      ref.TypeName,
			NodeID:        OntologyNodeID(ref),
			RefJSON:       string(refJSON),
			SourceLocator: NodeSourceLocator(ref),
		}, true
	})
}
