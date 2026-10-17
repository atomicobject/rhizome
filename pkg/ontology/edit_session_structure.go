package ontology

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
)

func (op addEmbeddedNodeOp) apply(state *documentState) error {
	snapshot, parentProjection, err := projectState(state, op.ParentRef)
	if err != nil {
		return err
	}
	field := parentProjection.Type.ByName[op.Field]
	if field == nil || field.Kind != ontoschema.FieldKindSection || !field.List {
		return fmt.Errorf("%w: collection %s not found on %s", errMissingField, op.Field, parentProjection.Ref.String())
	}
	childType := state.schema.Types[field.TypeName]
	if childType == nil || childType.Role != ontoschema.TypeRoleEmbeddedNode {
		return fmt.Errorf("%w: collection %s does not target embedded nodes", errUnsupportedTarget, op.Field)
	}
	blockID := strings.TrimSpace(strings.TrimPrefix(op.BlockID, "^"))
	if blockID == "" {
		blockID = slugBlockID(op.Heading)
	}
	if !ValidBlockID(blockID) {
		return fmt.Errorf("%w: invalid block ID %q", errUnsupportedTarget, blockID)
	}
	if owner, ok := existingBlockIDs(snapshot)[blockID]; ok && strings.TrimSpace(owner) != "" {
		return fmt.Errorf("%w: duplicate block ID %q", errUnsupportedTarget, blockID)
	}
	level := strings.Repeat("#", sectionLevelInt(field.SectionLevel))
	if level == "" {
		return fmt.Errorf("%w: collection %s has invalid section level", errUnsupportedTarget, op.Field)
	}
	snippet := insertedDocumentText(state.content, strings.TrimRight(level+" "+strings.TrimSpace(op.Heading)+"\n"+strings.TrimSpace(op.Body)+"\n^"+blockID+"\n", "\n")+"\n")
	insertAt := collectionInsertOffset(snapshot, parentProjection, op.Field)
	if insertAt < 0 || insertAt > len(state.content) {
		return fmt.Errorf("%w: invalid insert offset for %s", errUnsupportedTarget, op.Field)
	}
	state.setContent(state.content[:insertAt] + snippet + state.content[insertAt:])
	return nil
}

func (op deleteNodeOp) apply(state *documentState) error {
	snapshot, projection, err := projectState(state, op.Ref)
	if err != nil {
		return err
	}
	if projection.Ref.Kind == NodeKindNote {
		return fmt.Errorf("%w: deleting full notes is out of scope", errUnsupportedTarget)
	}
	r := ByteRange{Start: projection.Ref.StartByte, End: projection.Ref.EndByte}
	if !r.Valid(len(snapshot.Content)) || r.Len() == 0 {
		return fmt.Errorf("%w: invalid node span for %s", errMissingNode, projection.Ref.String())
	}
	state.setContent(replaceRange(state.content, r, ""))
	return nil
}

func (op reorderCollectionOp) apply(state *documentState) error {
	snapshot, parentProjection, err := projectState(state, op.ParentRef)
	if err != nil {
		return err
	}
	collection, ok := parentProjection.Collections[op.Field]
	if !ok {
		return fmt.Errorf("%w: collection %s not found on %s", errMissingField, op.Field, parentProjection.Ref.String())
	}
	if err := validateCollectionPermutation(collection.Items, op.OrderedFragments); err != nil {
		return fmt.Errorf("%w: collection %s %v", errCollectionDrift, op.Field, err)
	}
	if state.baseSnapshot != nil {
		baseProjection, err := ProjectNodeFromSnapshot(state.baseSnapshot, state.schema, op.ParentRef)
		if err != nil {
			return err
		}
		baseCollection, ok := baseProjection.Collections[op.Field]
		if !ok {
			return fmt.Errorf("%w: collection %s not found on %s", errMissingField, op.Field, baseProjection.Ref.String())
		}
		if len(baseCollection.Items) == len(collection.Items) &&
			sameCollectionMembers(baseCollection.Items, collection.Items) &&
			baseCollection.OrderFingerprint != "" && collection.OrderFingerprint != "" &&
			baseCollection.OrderFingerprint != collection.OrderFingerprint {
			return fmt.Errorf("%w: collection %s order changed", errCollectionDrift, op.Field)
		}
	}
	itemByFragment := make(map[string]CollectionItem, len(collection.Items))
	blockByFragment := make(map[string]string, len(collection.Items))
	for _, item := range collection.Items {
		key := strings.TrimSpace(strings.TrimPrefix(item.Ref.Fragment, "#"))
		itemByFragment[key] = item
	}
	for i, item := range collection.Items {
		key := strings.TrimSpace(strings.TrimPrefix(item.Ref.Fragment, "#"))
		blockStart := item.Range.Start
		blockEnd := collection.Range.End
		if i+1 < len(collection.Items) {
			blockEnd = collection.Items[i+1].Range.Start
		}
		if blockEnd < blockStart || blockEnd > len(snapshot.Content) {
			return fmt.Errorf("%w: invalid member span for %s", errCollectionDrift, item.Ref.String())
		}
		blockByFragment[key] = snapshot.Content[blockStart:blockEnd]
	}
	ordered := make([]CollectionItem, 0, len(op.OrderedFragments))
	for _, fragment := range op.OrderedFragments {
		item, ok := itemByFragment[strings.TrimSpace(strings.TrimPrefix(fragment, "#"))]
		if !ok {
			return fmt.Errorf("%w: collection %s is missing %s", errCollectionDrift, op.Field, fragment)
		}
		ordered = append(ordered, item)
	}
	if !collection.Range.Valid(len(snapshot.Content)) || collection.Range.Len() == 0 {
		return fmt.Errorf("%w: collection %s has invalid range", errCollectionDrift, op.Field)
	}
	var b strings.Builder
	for _, item := range ordered {
		key := strings.TrimSpace(strings.TrimPrefix(item.Ref.Fragment, "#"))
		block, ok := blockByFragment[key]
		if !ok {
			return fmt.Errorf("%w: invalid member span for %s", errCollectionDrift, item.Ref.String())
		}
		b.WriteString(block)
	}
	state.setContent(replaceRange(state.content, collection.Range, b.String()))
	return nil
}

func validateCollectionPermutation(items []CollectionItem, orderedFragments []string) error {
	if len(items) != len(orderedFragments) {
		return fmt.Errorf("membership changed")
	}
	actual := make(map[string]struct{}, len(items))
	for _, item := range items {
		key := strings.TrimSpace(strings.TrimPrefix(item.Ref.Fragment, "#"))
		if key == "" {
			return fmt.Errorf("member has no stable fragment")
		}
		if _, exists := actual[key]; exists {
			return fmt.Errorf("member %q is duplicated", key)
		}
		actual[key] = struct{}{}
	}
	requested := make(map[string]struct{}, len(orderedFragments))
	for _, fragment := range orderedFragments {
		key := strings.TrimSpace(strings.TrimPrefix(fragment, "#"))
		if key == "" {
			return fmt.Errorf("requested member is empty")
		}
		if _, exists := requested[key]; exists {
			return fmt.Errorf("requested member %q is duplicated", key)
		}
		if _, exists := actual[key]; !exists {
			return fmt.Errorf("requested member %q is missing", key)
		}
		requested[key] = struct{}{}
	}
	if len(requested) != len(actual) {
		return fmt.Errorf("membership changed")
	}
	return nil
}
