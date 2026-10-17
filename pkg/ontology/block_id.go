package ontology

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

	"strings"
	"unicode"
)

func ValidBlockID(id string) bool {
	return markdownsyntax.ValidBlockID(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(id), "^")))
}

func GenerateBlockIDForProjection(snapshot *DocumentSnapshot, projection *NodeProjection) string {
	if projection == nil {
		return "node"
	}
	seed := authoredIDSeed(projection)
	if minted, ok := mintedDerivableIdentifierSeed(snapshot, projection, seed); ok {
		seed = minted
	}
	stable := seed
	if seed == "" {
		if structuralSeed, ok := structuralBlockIDSeed(snapshot, projection); ok {
			seed = structuralSeed
			stable = structuralSeed
		} else {
			node := snapshot.SectionsByID[projection.Ref.NodeID]
			title := ""
			if node != nil {
				title = node.Title
			}
			seed = strings.TrimSpace(projection.ResolvedType + "-" + title)
			stable = projection.Ref.String() + "|" + projection.Ref.Structural
		}
	}
	base := BlockSafeIdentifier(seed)
	if authoredIDSeed(projection) == "" && stable != seed {
		base = slugBlockID(seed)
		base = base + "-" + hashText(stable)[:8]
	}
	return uniqueBlockID(base, existingBlockIDs(snapshot), projection)
}

func structuralBlockIDSeed(snapshot *DocumentSnapshot, projection *NodeProjection) (string, bool) {
	if snapshot == nil || projection == nil || projection.Type == nil {
		return "", false
	}
	node := snapshot.SectionsByID[projection.Ref.NodeID]
	if node == nil {
		return "", false
	}
	parentByID := sectionParentMap(snapshot)
	ancestor := parentByID[node.ID]
	for ancestor != nil && strings.TrimSpace(ancestor.BlockID) == "" {
		ancestor = parentByID[ancestor.ID]
	}
	parentBlock := ""
	if ancestor != nil {
		parentBlock = strings.TrimSpace(strings.TrimPrefix(ancestor.BlockID, "^"))
	}
	if parentBlock == "" {
		return "", false
	}
	suffix := typeAcronym(projection.ResolvedType)
	if suffix == "" {
		return "", false
	}
	prefix := parentBlock + "-" + suffix
	candidate := siblingOrdinal(node, parentByID)
	maxAuthored := 0
	for _, sibling := range siblingsForNode(node, parentByID, snapshot) {
		if sibling == nil || sibling.ID == node.ID {
			continue
		}
		if n, ok := trailingNumberAfterPrefix(strings.TrimSpace(strings.TrimPrefix(sibling.BlockID, "^")), prefix); ok && n > maxAuthored {
			maxAuthored = n
		}
	}
	if maxAuthored >= candidate {
		candidate = maxAuthored + 1
	}
	return fmt.Sprintf("%s%d", prefix, candidate), true
}

func sectionParentMap(snapshot *DocumentSnapshot) map[string]*SectionNode {
	out := map[string]*SectionNode{}
	var walk func(parent *SectionNode, nodes []*SectionNode)
	walk = func(parent *SectionNode, nodes []*SectionNode) {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			if parent != nil {
				out[node.ID] = parent
			}
			walk(node, node.Children)
		}
	}
	if snapshot != nil {
		walk(nil, snapshot.Sections)
	}
	return out
}

func siblingsForNode(node *SectionNode, parentByID map[string]*SectionNode, snapshot *DocumentSnapshot) []*SectionNode {
	if node == nil {
		return nil
	}
	if parent := parentByID[node.ID]; parent != nil {
		return parent.Children
	}
	if snapshot != nil {
		return snapshot.Sections
	}
	return nil
}

func siblingOrdinal(node *SectionNode, parentByID map[string]*SectionNode) int {
	ordinal := 1
	for _, sibling := range siblingsForNode(node, parentByID, nil) {
		if sibling == nil {
			continue
		}
		if sibling.ID == node.ID {
			return ordinal
		}
		ordinal++
	}
	return ordinal
}

func typeAcronym(typeName string) string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return ""
	}
	var out []rune
	var prev rune
	for i, r := range typeName {
		if i == 0 || (unicode.IsUpper(r) && prev != 0 && !unicode.IsUpper(prev)) {
			out = append(out, unicode.ToUpper(r))
		}
		prev = r
	}
	if len(out) == 0 {
		return strings.ToUpper(typeName[:1])
	}
	return string(out)
}

func mintedDerivableIdentifierSeed(snapshot *DocumentSnapshot, projection *NodeProjection, seed string) (string, bool) {
	if snapshot == nil || projection == nil || projection.Type == nil {
		return "", false
	}
	field := projection.Type.PreferredIdentifierField()
	if field == nil || !field.IsDerivableIdentifier {
		return "", false
	}
	binding, ok := projection.Fields[field.Name]
	if !ok || !binding.Derived || strings.TrimSpace(seed) == "" {
		return "", false
	}
	prefix, candidate, ok := identifierPrefixAndNumber(seed)
	if !ok {
		return "", false
	}
	parentID := strings.TrimSpace(projection.Ref.ParentID)
	var siblings []*SectionNode
	if parentID == "" {
		siblings = snapshot.Sections
	} else if parent := snapshot.SectionsByID[parentID]; parent != nil {
		siblings = parent.Children
	}
	maxAuthored := 0
	for _, sibling := range siblings {
		if sibling == nil || sibling.ID == projection.Ref.NodeID {
			continue
		}
		authored := authoredIdentifierValueForNode(snapshot, sibling, field)
		if n, ok := trailingNumberAfterPrefix(authored, prefix); ok && n > maxAuthored {
			maxAuthored = n
		}
	}
	if maxAuthored >= candidate {
		return fmt.Sprintf("%s%d", prefix, maxAuthored+1), true
	}
	return seed, true
}

func identifierPrefixAndNumber(seed string) (string, int, bool) {
	seed = strings.TrimSpace(strings.TrimPrefix(seed, "^"))
	if seed == "" {
		return "", 0, false
	}
	idx := len(seed)
	for idx > 0 && seed[idx-1] >= '0' && seed[idx-1] <= '9' {
		idx--
	}
	if idx == len(seed) || idx == 0 {
		return "", 0, false
	}
	prefix := seed[:idx]
	n, ok := trailingNumberAfterPrefix(seed, prefix)
	return prefix, n, ok
}

func authoredIDSeed(projection *NodeProjection) string {
	if projection == nil || projection.Fields == nil || projection.Type == nil {
		return ""
	}
	if field := projection.Type.PreferredIdentifierField(); field != nil {
		return fieldValueSeed(projection.Fields[field.Name])
	}
	for _, field := range projection.Type.Fields {
		if field == nil || !field.IsIdentifier {
			continue
		}
		if value := fieldValueSeed(projection.Fields[field.Name]); value != "" {
			return value
		}
	}
	return ""
}

func fieldValueSeed(binding FieldBinding) string {
	if len(binding.Values) == 0 {
		return ""
	}
	return strings.TrimSpace(binding.Values[0])
}

func slugBlockID(seed string) string {
	seed = strings.ToLower(strings.TrimSpace(seed))
	if seed == "" {
		seed = "node"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range seed {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == '_' || r == '-':
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "node"
	}
	return out
}

func BlockSafeIdentifier(seed string) string {
	seed = strings.TrimSpace(strings.TrimPrefix(seed, "^"))
	if seed == "" {
		return "node"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range seed {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == '_' || r == '-':
			if !lastDash {
				b.WriteRune(r)
				lastDash = r == '-'
			}
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return "node"
	}
	return out
}

func existingBlockIDs(snapshot *DocumentSnapshot) map[string]string {
	out := map[string]string{}
	if snapshot == nil {
		return out
	}
	var walk func([]*SectionNode)
	walk = func(nodes []*SectionNode) {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			if id := strings.TrimSpace(strings.TrimPrefix(node.BlockID, "^")); id != "" {
				out[id] = node.ID
			}
			walk(node.Children)
		}
	}
	walk(snapshot.Sections)
	for _, span := range snapshot.SourceSpans {
		if span == nil || span.Shape == ontoschema.EmbeddedSourceShapeSection {
			continue
		}
		if id := strings.TrimSpace(strings.TrimPrefix(span.BlockID, "^")); id != "" {
			out[id] = span.ID
		}
	}
	return out
}

func uniqueBlockID(base string, existing map[string]string, projection *NodeProjection) string {
	base = BlockSafeIdentifier(base)
	if _, ok := existing[base]; !ok {
		return base
	}
	refKey := ""
	if projection != nil {
		refKey = projection.Ref.String() + "|" + projection.Ref.Structural
	}
	candidate := base + "-" + hashText(base + "|" + refKey)[:8]
	if _, ok := existing[candidate]; !ok {
		return candidate
	}
	for i := 2; ; i++ {
		next := fmt.Sprintf("%s-%d", candidate, i)
		if _, ok := existing[next]; !ok {
			return next
		}
	}
}
