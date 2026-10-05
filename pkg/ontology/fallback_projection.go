package ontology

import "strings"

// FallbackNoteTypeName is the reserved internal ontology type used to give
// genuinely untyped note roots the same semantic surface as typed notes.
const FallbackNoteTypeName = "_FallbackNote"

// FallbackSectionTypeName is the reserved internal ontology type used to give
// authored sections inside untyped notes stable node identity.
const FallbackSectionTypeName = "_FallbackSection"

// AsFallbackNoteProjection converts an unresolved, unassessed note-root
// projection into the internal fallback note projection.
func AsFallbackNoteProjection(projection *NodeProjection) (*NodeProjection, bool) {
	if projection == nil || (projection.Snapshot == nil && projection.RootSnapshot == nil) {
		return nil, false
	}
	if strings.TrimSpace(projection.ResolvedType) != "" || projection.Assessment != nil {
		return nil, false
	}
	ref := projection.Ref
	if ref.Kind != "" && ref.Kind != NodeKindNote {
		return nil, false
	}
	if strings.TrimSpace(ref.Fragment) != "" || strings.TrimSpace(ref.NodeID) != "" {
		return nil, false
	}
	clone := *projection
	clone.ResolvedType = FallbackNoteTypeName
	clone.Type = nil
	clone.PropertyCase = ""
	clone.Fields = map[string]FieldBinding{}
	clone.Collections = map[string]CollectionBinding{}
	clone.Ref.TypeName = FallbackNoteTypeName
	clone.Ref.Kind = NodeKindNote
	if strings.TrimSpace(clone.Ref.NotePath) == "" {
		if clone.Snapshot != nil {
			clone.Ref.NotePath = clone.Snapshot.NotePath
		} else {
			clone.Ref.NotePath = clone.RootSnapshot.NotePath.String()
		}
	}
	if clone.Ref.EndByte == 0 {
		if clone.Snapshot != nil {
			clone.Ref.EndByte = len(clone.Snapshot.Content)
		} else {
			clone.Ref.EndByte = len(clone.RootSnapshot.RawSource)
		}
	}
	return &clone, true
}

// AsFallbackSectionProjection converts an unresolved section projection from an
// untyped fallback note into the internal fallback section projection.
func AsFallbackSectionProjection(projection *NodeProjection) (*NodeProjection, bool) {
	if projection == nil || projection.Snapshot == nil {
		return nil, false
	}
	if strings.TrimSpace(projection.ResolvedType) != "" || projection.Assessment != nil {
		return nil, false
	}
	ref := projection.Ref
	if ref.Kind != NodeKindSection && ref.Kind != NodeKindEmbedded {
		return nil, false
	}
	section := projection.Snapshot.SectionsByID[ref.NodeID]
	if section == nil {
		return nil, false
	}
	parentID := ref.ParentID
	if strings.TrimSpace(parentID) == "" {
		parentID = sectionParentID(projection.Snapshot.Sections, section.ID)
	}
	clone := *projection
	clone.ResolvedType = FallbackSectionTypeName
	clone.Type = nil
	clone.PropertyCase = ""
	clone.Fields = map[string]FieldBinding{}
	clone.Collections = map[string]CollectionBinding{}
	clone.Ref = fallbackSectionNodeRef(projection.Snapshot.NotePath, section, parentID)
	return &clone, true
}

func sectionParentID(sections []*SectionNode, childID string) string {
	var found string
	var walk func(parentID string, nodes []*SectionNode)
	walk = func(parentID string, nodes []*SectionNode) {
		if found != "" {
			return
		}
		for _, node := range nodes {
			if node == nil {
				continue
			}
			if node.ID == childID {
				found = parentID
				return
			}
			walk(node.ID, node.Children)
		}
	}
	walk("", sections)
	return found
}

// IsFallbackNoteProjection reports whether a projection is the internal
// fallback semantic projection for an untyped note.
func IsFallbackNoteProjection(projection *NodeProjection) bool {
	return projection != nil && IsFallbackNoteTypeName(projection.ResolvedType)
}

// IsFallbackSectionProjection reports whether a projection is the internal
// fallback semantic projection for an authored section in an untyped note.
func IsFallbackSectionProjection(projection *NodeProjection) bool {
	return projection != nil && IsFallbackSectionTypeName(projection.ResolvedType)
}

// IsFallbackNoteTypeName reports whether typeName is the internal fallback
// note type.
func IsFallbackNoteTypeName(typeName string) bool {
	return strings.TrimSpace(typeName) == FallbackNoteTypeName
}

// IsFallbackSectionTypeName reports whether typeName is the internal fallback
// section type.
func IsFallbackSectionTypeName(typeName string) bool {
	return strings.TrimSpace(typeName) == FallbackSectionTypeName
}

// IsFallbackTypeName reports whether typeName is one of Rhizome's internal
// fallback semantic types.
func IsFallbackTypeName(typeName string) bool {
	return IsFallbackNoteTypeName(typeName) || IsFallbackSectionTypeName(typeName)
}

type TypeVisibility string

const (
	TypeVisibilityPublic   TypeVisibility = "public"
	TypeVisibilityInternal TypeVisibility = "internal"
)

// OntologyTypeVisibility classifies type names at public read boundaries.
// Fallback notes are first-class internal evidence, not authored schema types.
func OntologyTypeVisibility(typeName string) TypeVisibility {
	if IsFallbackTypeName(typeName) {
		return TypeVisibilityInternal
	}
	return TypeVisibilityPublic
}

// FallbackSectionProjections returns internal section projections for an
// untyped fallback note. They are catalog/search identities only; they are not
// public schema-authored ontology types.
func FallbackSectionProjections(root *NodeProjection) []*NodeProjection {
	if !IsFallbackNoteProjection(root) || root.Snapshot == nil {
		return nil
	}
	var out []*NodeProjection
	var walk func(parentID string, sections []*SectionNode)
	walk = func(parentID string, sections []*SectionNode) {
		for _, section := range sections {
			if section == nil {
				continue
			}
			ref := fallbackSectionNodeRef(root.Snapshot.NotePath, section, parentID)
			out = append(out, &NodeProjection{
				Snapshot:     root.Snapshot,
				Ref:          ref,
				ResolvedType: FallbackSectionTypeName,
				Fields:       map[string]FieldBinding{},
				Collections:  map[string]CollectionBinding{},
			})
			walk(section.ID, section.Children)
		}
	}
	walk("", UnwrapSingleH1SectionRoot(root.Snapshot.Sections))
	return out
}

func fallbackSectionNodeRef(notePath string, section *SectionNode, parentID string) NodeRef {
	if section == nil {
		return NodeRef{}
	}
	return NodeRef{
		NotePath:   notePath,
		Fragment:   sectionFragment(section),
		NodeID:     section.ID,
		TypeName:   FallbackSectionTypeName,
		Kind:       NodeKindSection,
		StartByte:  section.StartByte,
		EndByte:    section.EndByte,
		ParentID:   parentID,
		Structural: hashText(fallbackSectionFingerprint(section, parentID)),
	}
}

func fallbackSectionFingerprint(section *SectionNode, parentID string) string {
	if section == nil {
		return ""
	}
	if blockID := strings.TrimSpace(section.BlockID); blockID != "" {
		return "block:" + strings.TrimPrefix(blockID, "^")
	}
	return strings.Join([]string{
		strings.TrimSpace(parentID),
		string(section.Level),
		slugifyHeading(section.Title),
		strings.TrimSpace(section.ID),
	}, "|")
}

type fallbackBodyPart struct {
	Text string
}

// FallbackNoteBodyParts returns root body chunks for untyped fallback notes.
// Authored child sections are represented by separate fallback section nodes.
func FallbackNoteBodyParts(projection *NodeProjection, maxBytes int) []string {
	if !IsFallbackNoteProjection(projection) || projection.Snapshot == nil {
		return nil
	}
	if maxBytes <= 0 {
		maxBytes = 4096
	}
	snapshot := projection.Snapshot
	var raw []fallbackBodyPart
	rootStart := snapshot.FrontmatterRange.End
	for _, section := range snapshot.Sections {
		if section == nil {
			continue
		}
		if section.StartByte > rootStart {
			raw = append(raw, fallbackBodyPart{Text: snapshot.Content[rootStart:section.StartByte]})
		}
		if isFallbackRootH1(snapshot.Sections, section) {
			if text := strings.TrimSpace(fallbackSectionOwnMarkdown(snapshot.Content, section)); text != "" {
				raw = append(raw, fallbackBodyPart{Text: text})
			}
		}
		if section.EndByte > rootStart {
			rootStart = section.EndByte
		}
	}
	if rootStart < len(snapshot.Content) {
		raw = append(raw, fallbackBodyPart{Text: snapshot.Content[rootStart:]})
	}
	var parts []string
	for _, body := range packFallbackBodyParts(raw, maxBytes) {
		parts = append(parts, splitFallbackBody(body, maxBytes)...)
	}
	return parts
}

func isFallbackRootH1(rootSections []*SectionNode, section *SectionNode) bool {
	return len(rootSections) == 1 &&
		section != nil &&
		rootSections[0] == section &&
		section.Level == SectionLevelH1
}

// FallbackSectionBodyParts returns body chunks for one section fallback node.
func FallbackSectionBodyParts(projection *NodeProjection, maxBytes int) []string {
	if !IsFallbackSectionProjection(projection) || projection.Snapshot == nil {
		return nil
	}
	if maxBytes <= 0 {
		maxBytes = 4096
	}
	section := projection.Snapshot.SectionsByID[projection.Ref.NodeID]
	if section == nil {
		return nil
	}
	text := strings.TrimSpace(fallbackSectionOwnMarkdown(projection.Snapshot.Content, section))
	if text == "" {
		return nil
	}
	lines := []string{"Section: " + strings.TrimSpace(section.Title), "", text}
	return splitFallbackBody(strings.Join(lines, "\n"), maxBytes)
}

func fallbackSectionOwnMarkdown(content string, section *SectionNode) string {
	if section == nil || section.EndByte <= section.StartByte || section.StartByte < 0 || section.EndByte > len(content) {
		return ""
	}
	start := section.StartByte
	for start < section.EndByte && start < len(content) && content[start] != '\n' {
		start++
	}
	if start < section.EndByte && start < len(content) {
		start++
	}
	end := section.EndByte
	for _, child := range section.Children {
		if child != nil && child.StartByte >= start && child.StartByte < end {
			end = child.StartByte
			break
		}
	}
	if end <= start {
		return ""
	}
	return content[start:end]
}

func packFallbackBodyParts(parts []fallbackBodyPart, maxBytes int) []string {
	var out []string
	var current []string
	currentLen := 0
	flush := func() {
		if len(current) == 0 {
			return
		}
		out = append(out, strings.TrimSpace(strings.Join(current, "\n\n")))
		current = nil
		currentLen = 0
	}
	for _, part := range parts {
		text := strings.TrimSpace(part.Text)
		if text == "" {
			continue
		}
		if len(text) > maxBytes {
			flush()
			out = append(out, splitFallbackBody(text, maxBytes)...)
			continue
		}
		nextLen := currentLen + len(text)
		if len(current) > 0 {
			nextLen += 2
		}
		if nextLen > maxBytes {
			flush()
		}
		current = append(current, text)
		currentLen += len(text)
	}
	flush()
	return out
}

func splitFallbackBody(body string, maxBytes int) []string {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	if maxBytes <= 0 {
		maxBytes = 4096
	}
	if len(body) <= maxBytes {
		return []string{body}
	}
	var out []string
	var current []string
	currentLen := 0
	inFence := false
	flush := func() {
		if len(current) == 0 {
			return
		}
		out = append(out, strings.TrimSpace(strings.Join(current, "\n\n")))
		current = nil
		currentLen = 0
	}
	for _, paragraph := range fallbackMarkdownParagraphs(body) {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		for _, line := range strings.Split(paragraph, "\n") {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
				inFence = !inFence
			}
		}
		if len(paragraph) > maxBytes {
			flush()
			out = append(out, splitFallbackWithOverlap(paragraph, maxBytes, 300)...)
			continue
		}
		nextLen := currentLen + len(paragraph)
		if len(current) > 0 {
			nextLen += 2
		}
		if !inFence && nextLen > maxBytes {
			flush()
		}
		current = append(current, paragraph)
		currentLen += len(paragraph)
	}
	flush()
	return out
}

func fallbackMarkdownParagraphs(body string) []string {
	lines := strings.Split(body, "\n")
	var paragraphs []string
	var current []string
	inFence := false
	flush := func() {
		if len(current) == 0 {
			return
		}
		paragraphs = append(paragraphs, strings.Join(current, "\n"))
		current = nil
	}
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		fence := strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~")
		if trim == "" && !inFence {
			flush()
			continue
		}
		current = append(current, line)
		if fence {
			inFence = !inFence
		}
	}
	flush()
	return paragraphs
}

func splitFallbackWithOverlap(text string, maxBytes int, overlap int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxBytes <= 0 || len(text) <= maxBytes {
		return []string{text}
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= maxBytes {
		overlap = maxBytes / 4
	}
	step := maxBytes - overlap
	if step <= 0 {
		step = maxBytes
	}
	var out []string
	for start := 0; start < len(text); {
		end := start + maxBytes
		if end > len(text) {
			end = len(text)
		}
		part := strings.TrimSpace(text[start:end])
		if part != "" {
			out = append(out, part)
		}
		if end >= len(text) {
			break
		}
		start += step
	}
	return out
}
