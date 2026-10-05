package ontology

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type EnsureLinkTargetMode string

const (
	EnsureLinkTargetNever EnsureLinkTargetMode = "never"
	EnsureLinkTargetPlan  EnsureLinkTargetMode = "plan"
	EnsureLinkTargetApply EnsureLinkTargetMode = "apply"
)

// NodeLocatorStatus describes whether a node has an author-facing address.
type NodeLocatorStatus string

const (
	NodeLocatorLinkable    NodeLocatorStatus = "linkable"
	NodeLocatorRequiresFix NodeLocatorStatus = "requires_fix"
	NodeLocatorUnsupported NodeLocatorStatus = "unsupported"
	NodeLocatorUnresolved  NodeLocatorStatus = "unresolved"
)

type NodeLinkService struct {
	VaultDef            obsidian.VaultDefinition
	NoteReader          obsidian.NoteReader
	Schema              *Schema
	VaultWriteLeaseHeld bool
}

type LinkTargetRequest struct {
	Refs    []NodeRef
	Ensure  EnsureLinkTargetMode
	Purpose string
}

type LinkTargetResult struct {
	// Applied reports that this request committed a source edit. It remains true
	// when the post-commit reread cannot produce refreshed targets, so callers
	// can complete any required durable convergence before reporting that error.
	Applied     bool                      `json:"applied,omitempty"`
	Targets     map[string]NodeLinkTarget `json:"targets,omitempty"`
	FixPlan     *NodeLinkFixPlan          `json:"fixPlan,omitempty"`
	Diagnostics []NodeLinkDiagnostic      `json:"diagnostics,omitempty"`
}

type NodeLinkTarget struct {
	Ref          NodeRef `json:"ref"`
	Markdown     string  `json:"markdown,omitempty"`
	Wikilink     string  `json:"wikilink,omitempty"`
	DisplayLabel string  `json:"displayLabel,omitempty"`
	Exists       bool    `json:"exists"`
	RequiresFix  bool    `json:"requiresFix,omitempty"`
	BlockID      string  `json:"blockId,omitempty"`
}

type NodeLinkFixPlan struct {
	Actions []NodeLinkFixAction `json:"actions,omitempty"`
}

type NodeLinkFixAction struct {
	Ref     NodeRef `json:"ref"`
	BlockID string  `json:"blockId"`
}

type NodeLinkDiagnostic struct {
	Code     string  `json:"code"`
	NotePath string  `json:"notePath,omitempty"`
	Ref      NodeRef `json:"ref,omitempty"`
	BlockID  string  `json:"blockId,omitempty"`
	Message  string  `json:"message"`
}

// NodeLocator projects a canonical NodeRef into the stable author-facing
// locator contract used by APIs and UI copy-link actions.
// Docs: [[ontology-browser-workspace#^spec-0014-us5-ac3]] and
// [[linkable-embedded-node-identifiers#^spec-0023-us1-ac3]] require explicit
// requires-fix status instead of fragile heading-only links.
type NodeLocator struct {
	Ref           NodeRef              `json:"ref"`
	Kind          NodeKind             `json:"kind"`
	SourceLocator string               `json:"sourceLocator"`
	LinkTarget    *NodeLinkTarget      `json:"linkTarget,omitempty"`
	Status        NodeLocatorStatus    `json:"status"`
	Diagnostics   []NodeLinkDiagnostic `json:"diagnostics,omitempty"`
	FixActions    []NodeLinkFixAction  `json:"fixActions,omitempty"`
}

// Locators returns structured locator projections. It only mutates files when
// the caller explicitly uses EnsureLinkTargetApply.
// Docs: [[linkable-embedded-node-identifiers#^spec-0023-us3-ac1]] owns that
// explicit write-capable make-linkable flow.
func (s *NodeLinkService) Locators(ctx context.Context, req LinkTargetRequest) (map[string]NodeLocator, error) {
	result, err := s.LinkTargets(ctx, req)
	if err != nil {
		return nil, err
	}
	diagnostics := map[string][]NodeLinkDiagnostic{}
	for _, diagnostic := range result.Diagnostics {
		key := nodeLocatorMapKey(normalizeLinkNodeRef(diagnostic.Ref))
		if key == "" {
			key = diagnostic.NotePath
		}
		diagnostics[key] = append(diagnostics[key], diagnostic)
	}
	fixes := map[string][]NodeLinkFixAction{}
	if result.FixPlan != nil {
		for _, action := range result.FixPlan.Actions {
			key := nodeLocatorMapKey(normalizeLinkNodeRef(action.Ref))
			fixes[key] = append(fixes[key], action)
		}
	}

	locators := make(map[string]NodeLocator, len(req.Refs))
	for _, inputRef := range req.Refs {
		ref := normalizeLinkNodeRef(inputRef)
		if ref.IsZero() {
			continue
		}
		key := nodeLocatorMapKey(ref)
		target, hasTarget := result.Targets[key]
		status := NodeLocatorUnresolved
		var targetPtr *NodeLinkTarget
		if hasTarget {
			targetCopy := target
			targetPtr = &targetCopy
			if target.RequiresFix {
				status = NodeLocatorRequiresFix
			} else if target.Exists {
				status = NodeLocatorLinkable
			}
		} else {
			status = locatorStatusFromDiagnostics(diagnostics[key])
		}
		fixActions := nodeLocatorFixActions(ref, fixes[key], result.FixPlan)
		if len(fixActions) == 0 && targetPtr != nil && targetPtr.RequiresFix && strings.TrimSpace(targetPtr.BlockID) != "" {
			fixActions = []NodeLinkFixAction{{Ref: targetPtr.Ref, BlockID: targetPtr.BlockID}}
		}
		locators[key] = NodeLocator{
			Ref:           ref,
			Kind:          nodeLocatorKind(ref),
			SourceLocator: nodeSourceLocator(ref),
			LinkTarget:    targetPtr,
			Status:        status,
			Diagnostics:   diagnostics[key],
			FixActions:    fixActions,
		}
	}
	return locators, nil
}

// LinkTargets renders durable author-facing links for ontology nodes. It never
// mutates source unless EnsureLinkTargetApply is requested.
// Docs: [[ontology-browser-workspace#^spec-0014-us5-ac4]] keeps workspace,
// coderef, and agent copy-link flows on this one target contract.
func (s *NodeLinkService) LinkTargets(ctx context.Context, req LinkTargetRequest) (LinkTargetResult, error) {
	if s == nil {
		return LinkTargetResult{}, fmt.Errorf("node link service is nil")
	}
	if s.NoteReader == nil {
		return LinkTargetResult{}, fmt.Errorf("note reader is required")
	}
	mode := req.Ensure
	if mode == "" {
		mode = EnsureLinkTargetNever
	}
	allNotes, listErr := s.NoteReader.GetNotesList(s.VaultDef)
	if canceled := ctx.Err(); canceled != nil {
		return LinkTargetResult{}, canceled
	}
	if errors.Is(listErr, context.Canceled) || errors.Is(listErr, context.DeadlineExceeded) {
		return LinkTargetResult{}, listErr
	}
	renderer := newWikilinkRenderer(allNotes)
	result := LinkTargetResult{Targets: map[string]NodeLinkTarget{}}
	snapshots := map[string]*DocumentSnapshot{}
	fixPlan := &NodeLinkFixPlan{}

	for _, inputRef := range req.Refs {
		ref := normalizeLinkNodeRef(inputRef)
		if ref.IsZero() {
			continue
		}
		key := nodeLocatorMapKey(ref)
		if ref.Kind == "" || ref.Kind == NodeKindNote {
			target := noteLinkTarget(ref, renderer)
			result.Targets[key] = target
			continue
		}
		if ref.Kind != NodeKindEmbedded {
			result.Diagnostics = append(result.Diagnostics, NodeLinkDiagnostic{
				Code:     "unsupported_node_kind",
				NotePath: ref.NotePath,
				Ref:      ref,
				Message:  "only embedded ontology nodes are linkable with block identifiers",
			})
			continue
		}
		snapshot := snapshots[ref.NotePath]
		if snapshot == nil {
			loaded, err := LoadDocumentSnapshot(ctx, s.VaultDef, s.NoteReader, ref.NotePath)
			if canceled := ctx.Err(); canceled != nil {
				return result, canceled
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return result, err
			}
			if err != nil {
				result.Diagnostics = append(result.Diagnostics, NodeLinkDiagnostic{
					Code:     "snapshot_load_failed",
					NotePath: ref.NotePath,
					Ref:      ref,
					Message:  err.Error(),
				})
				continue
			}
			snapshot = loaded
			snapshots[ref.NotePath] = snapshot
		}
		projection, err := ProjectNodeFromSnapshot(snapshot, s.Schema, ref)
		if err != nil || projection == nil {
			msg := "embedded node could not be resolved"
			if err != nil {
				msg = err.Error()
			}
			result.Diagnostics = append(result.Diagnostics, NodeLinkDiagnostic{
				Code:     "node_unresolved",
				NotePath: ref.NotePath,
				Ref:      ref,
				Message:  msg,
			})
			continue
		}
		target, action, diags := embeddedLinkTarget(snapshot, projection, renderer)
		result.Targets[key] = target
		result.Diagnostics = append(result.Diagnostics, diags...)
		if action != nil && mode != EnsureLinkTargetNever {
			fixPlan.Actions = append(fixPlan.Actions, *action)
		}
	}

	if len(fixPlan.Actions) > 0 {
		result.FixPlan = fixPlan
	}
	if mode == EnsureLinkTargetApply && len(fixPlan.Actions) > 0 {
		session := NewEditSession(s.VaultDef, s.NoteReader, s.Schema)
		for _, action := range fixPlan.Actions {
			if err := session.EnsureBlockID(action.Ref, action.BlockID); err != nil {
				return result, err
			}
		}
		commit, err := session.CommitWithOptions(ctx, CommitOptions{
			IncludeDiff:                  true,
			IncludeUpdatedContentPreview: true,
			VaultWriteLeaseHeld:          s.VaultWriteLeaseHeld,
		})
		if err != nil {
			return result, err
		}
		if !commit.Applied || len(commit.Conflicts) > 0 {
			result.Diagnostics = append(result.Diagnostics, NodeLinkDiagnostic{
				Code:    "apply_failed",
				Message: "block ID fix plan could not be applied",
			})
			return result, nil
		}
		refresh := req
		refresh.Ensure = EnsureLinkTargetNever
		refreshed, refreshErr := s.LinkTargets(ctx, refresh)
		refreshed.Applied = true
		return refreshed, refreshErr
	}

	return result, nil
}

func noteLinkTarget(ref NodeRef, renderer wikilinkRenderer) NodeLinkTarget {
	base := renderer.target(ref.NotePath)
	label := strings.TrimSuffix(filepath.Base(ref.NotePath), filepath.Ext(ref.NotePath))
	return NodeLinkTarget{
		Ref:          ref,
		Markdown:     ref.NotePath,
		Wikilink:     "[[" + base + "]]",
		DisplayLabel: label,
		Exists:       true,
	}
}

func embeddedLinkTarget(snapshot *DocumentSnapshot, projection *NodeProjection, renderer wikilinkRenderer) (NodeLinkTarget, *NodeLinkFixAction, []NodeLinkDiagnostic) {
	ref := projection.Ref
	node := snapshot.SectionsByID[ref.NodeID]
	var label string
	var blockID string
	if node != nil {
		label = node.Title
		blockID = node.BlockID
	} else if span := snapshot.SourceSpansByID[ref.NodeID]; span != nil {
		label = span.Title
		blockID = span.BlockID
	}
	if label == "" {
		label = ref.TypeName
	}
	if node == nil && snapshot.SourceSpansByID[ref.NodeID] == nil {
		return NodeLinkTarget{}, nil, []NodeLinkDiagnostic{{
			Code:     "node_missing_source",
			NotePath: ref.NotePath,
			Ref:      ref,
			Message:  "embedded node source range is unavailable",
		}}
	}
	if strings.TrimSpace(blockID) != "" {
		target := nodeLinkTargetWithBlock(ref, renderer, label, blockID, true)
		return target, nil, nil
	}
	blockID = GenerateBlockIDForProjection(snapshot, projection)
	target := nodeLinkTargetWithBlock(ref, renderer, label, blockID, false)
	target.RequiresFix = true
	return target, &NodeLinkFixAction{Ref: ref, BlockID: blockID}, []NodeLinkDiagnostic{{
		Code:     "missing_embedded_block_id",
		NotePath: ref.NotePath,
		Ref:      ref,
		BlockID:  blockID,
		Message:  "embedded ontology node needs a block ID before its link target is durable",
	}}
}

func nodeLinkTargetWithBlock(ref NodeRef, renderer wikilinkRenderer, label, blockID string, exists bool) NodeLinkTarget {
	blockID = strings.TrimPrefix(strings.TrimSpace(blockID), "^")
	markdown := ref.NotePath + "#^" + blockID
	wikiTarget := renderer.target(ref.NotePath) + "#^" + blockID
	return NodeLinkTarget{
		Ref:          ref,
		Markdown:     markdown,
		Wikilink:     "[[" + wikiTarget + "]]",
		DisplayLabel: label,
		Exists:       exists,
		BlockID:      blockID,
	}
}

type wikilinkRenderer struct {
	uniqueStem map[string]bool
}

func newWikilinkRenderer(allNotes []string) wikilinkRenderer {
	counts := map[string]int{}
	for _, note := range allNotes {
		stem := strings.TrimSuffix(filepath.Base(note), filepath.Ext(note))
		if stem != "" {
			counts[stem]++
		}
	}
	unique := map[string]bool{}
	for stem, count := range counts {
		unique[stem] = count == 1
	}
	return wikilinkRenderer{uniqueStem: unique}
}

func (r wikilinkRenderer) target(notePath string) string {
	stem := strings.TrimSuffix(filepath.Base(notePath), filepath.Ext(notePath))
	if stem != "" && r.uniqueStem[stem] {
		return stem
	}
	return strings.TrimSuffix(notePath, filepath.Ext(notePath))
}

func normalizeLinkNodeRef(ref NodeRef) NodeRef {
	ref.NotePath = strings.TrimSpace(ref.NotePath)
	ref.Fragment = strings.TrimSpace(ref.Fragment)
	ref.NodeID = strings.TrimSpace(ref.NodeID)
	ref.TypeName = strings.TrimSpace(ref.TypeName)
	ref.ParentID = strings.TrimSpace(ref.ParentID)
	ref.Structural = strings.TrimSpace(ref.Structural)
	return ref
}

func nodeLocatorKind(ref NodeRef) NodeKind {
	if ref.Kind == "" {
		return NodeKindNote
	}
	return ref.Kind
}

func nodeLocatorMapKey(ref NodeRef) string {
	if locator := nodeSourceLocator(ref); locator != "" {
		return locator
	}
	return ref.String()
}

func nodeSourceLocator(ref NodeRef) string {
	ref = normalizeLinkNodeRef(ref)
	if ref.Kind == NodeKindNote {
		return ref.NotePath
	}
	if ref.Fragment != "" {
		return ref.String()
	}
	if ref.NodeID != "" {
		if fragment := normalizeSectionFragment(ref.NodeID, ref.NotePath); fragment != "" {
			return ref.NotePath + "#" + fragment
		}
		return ref.NotePath + "#node:" + ref.NodeID
	}
	if ref.Structural != "" {
		return ref.NotePath + "#struct:" + ref.Structural
	}
	return ref.String()
}

func locatorStatusFromDiagnostics(diagnostics []NodeLinkDiagnostic) NodeLocatorStatus {
	for _, diagnostic := range diagnostics {
		switch diagnostic.Code {
		case "unsupported_node_kind":
			return NodeLocatorUnsupported
		case "node_unresolved", "node_missing_source", "snapshot_load_failed":
			return NodeLocatorUnresolved
		}
	}
	return NodeLocatorUnresolved
}

func nodeLocatorFixActions(ref NodeRef, keyed []NodeLinkFixAction, plan *NodeLinkFixPlan) []NodeLinkFixAction {
	if len(keyed) > 0 || plan == nil {
		return keyed
	}
	for _, action := range plan.Actions {
		if nodeLocatorRefsMatch(ref, action.Ref) {
			return []NodeLinkFixAction{action}
		}
	}
	return nil
}

func nodeLocatorRefsMatch(a, b NodeRef) bool {
	a = normalizeLinkNodeRef(a)
	b = normalizeLinkNodeRef(b)
	if a.NotePath == "" || a.NotePath != b.NotePath {
		return false
	}
	if a.NodeID != "" && b.NodeID != "" {
		return a.NodeID == b.NodeID
	}
	if a.Structural != "" && b.Structural != "" {
		return a.Structural == b.Structural
	}
	if a.Fragment != "" && b.Fragment != "" {
		return a.Fragment == b.Fragment
	}
	return false
}

func ValidBlockID(id string) bool {
	id = strings.TrimSpace(strings.TrimPrefix(id, "^"))
	if id == "" {
		return false
	}
	for _, r := range id {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
		case r == '-', r == '_':
		default:
			return false
		}
	}
	return true
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
	field := preferredIdentifierField(projection.Type)
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
	if field := preferredIdentifierField(projection.Type); field != nil {
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

func preferredIdentifierField(noteType *NoteType) *Field {
	if noteType == nil {
		return nil
	}
	for _, field := range noteType.Fields {
		if field != nil && field.IsPreferredIdentifier {
			return field
		}
	}
	return nil
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
		if span == nil || span.Shape == EmbeddedSourceShapeSection {
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

func EnsureBlockIDInSnapshot(snapshot *DocumentSnapshot, projection *NodeProjection, preferred string) (string, string, error) {
	if snapshot == nil || projection == nil {
		return "", "", fmt.Errorf("%w: projection is required", errMissingNode)
	}
	if projection.Ref.Kind != NodeKindEmbedded {
		return snapshot.Content, projection.Ref.Fragment, nil
	}
	node := snapshot.SectionsByID[projection.Ref.NodeID]
	if node == nil {
		if span := snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil && span.Shape != EmbeddedSourceShapeSection {
			return ensureSourceSpanBlockID(snapshot, projection, span, preferred)
		}
	}
	if node == nil {
		return "", "", fmt.Errorf("%w: embedded node %s not found", errMissingNode, projection.Ref.String())
	}
	if fragment := sectionFragment(node); strings.HasPrefix(fragment, "^") {
		return snapshot.Content, strings.TrimPrefix(fragment, "^"), nil
	}
	blockID := strings.TrimSpace(strings.TrimPrefix(preferred, "^"))
	if blockID == "" {
		blockID = GenerateBlockIDForProjection(snapshot, projection)
	}
	if !ValidBlockID(blockID) {
		return "", "", fmt.Errorf("%w: invalid block ID %q", errUnsupportedTarget, blockID)
	}
	blockID = uniqueBlockID(blockID, existingBlockIDs(snapshot), projection)
	insertAt := blockIDInsertOffset(node)
	if insertAt < 0 || insertAt > len(snapshot.Content) {
		return "", "", fmt.Errorf("%w: invalid block ID insert offset for %s", errUnsupportedTarget, projection.Ref.String())
	}
	text := "\n^" + blockID + "\n"
	if insertAt > 0 && snapshot.Content[insertAt-1] == '\n' {
		text = "^" + blockID + "\n"
	}
	updated := snapshot.Content[:insertAt] + text + snapshot.Content[insertAt:]
	return updated, blockID, nil
}

func ensureSourceSpanBlockID(snapshot *DocumentSnapshot, projection *NodeProjection, span *MarkdownSourceSpan, preferred string) (string, string, error) {
	if snapshot == nil || projection == nil || span == nil {
		return "", "", fmt.Errorf("%w: source span is required", errMissingNode)
	}
	if fragment := sourceSpanFragment(span); strings.HasPrefix(fragment, "^") {
		return snapshot.Content, strings.TrimPrefix(fragment, "^"), nil
	}
	blockID := strings.TrimSpace(strings.TrimPrefix(preferred, "^"))
	if blockID == "" {
		blockID = GenerateBlockIDForProjection(snapshot, projection)
	}
	if !ValidBlockID(blockID) {
		return "", "", fmt.Errorf("%w: invalid block ID %q", errUnsupportedTarget, blockID)
	}
	blockID = uniqueBlockID(blockID, existingBlockIDs(snapshot), projection)
	insertAt := sourceSpanBlockIDInsertOffset(snapshot.Content, span)
	if insertAt < 0 || insertAt > len(snapshot.Content) {
		return "", "", fmt.Errorf("%w: invalid block ID insert offset for %s", errUnsupportedTarget, projection.Ref.String())
	}
	updated := snapshot.Content[:insertAt] + " ^" + blockID + snapshot.Content[insertAt:]
	return updated, blockID, nil
}

func sourceSpanBlockIDInsertOffset(content string, span *MarkdownSourceSpan) int {
	if span == nil || !span.Range.Valid(len(content)) {
		return -1
	}
	lineEnd := span.Range.End
	if idx := strings.IndexByte(content[span.Range.Start:span.Range.End], '\n'); idx >= 0 {
		lineEnd = span.Range.Start + idx
	}
	for lineEnd > span.Range.Start && (content[lineEnd-1] == ' ' || content[lineEnd-1] == '\t') {
		lineEnd--
	}
	return lineEnd
}

func blockIDInsertOffset(node *SectionNode) int {
	if node == nil {
		return -1
	}
	insertAt := node.EndByte
	for _, child := range node.Children {
		if child != nil && child.StartByte > node.StartByte && child.StartByte < insertAt {
			insertAt = child.StartByte
		}
	}
	return insertAt
}

type BlockIDValidationIssue struct {
	Code     string
	NotePath string
	Ref      NodeRef
	TypeName string
	Label    string
	BlockID  string
	Message  string
}

func ValidateEmbeddedBlockIDs(snapshot *DocumentSnapshot, schema *Schema) []BlockIDValidationIssue {
	if snapshot == nil || schema == nil {
		return nil
	}
	resolver, err := newProjectionResolver(snapshot, schema)
	if err != nil {
		return nil
	}
	counts := map[string]int{}
	var collect func([]*SectionNode)
	collect = func(nodes []*SectionNode) {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			if id := strings.TrimSpace(node.BlockID); id != "" {
				counts[strings.TrimPrefix(id, "^")]++
			}
			collect(node.Children)
		}
	}
	collect(snapshot.Sections)
	issues := make([]BlockIDValidationIssue, 0)
	var walk func([]*SectionNode)
	walk = func(nodes []*SectionNode) {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			ref := resolver.sectionNodeRef(node)
			rawBlockID := strings.TrimSpace(strings.TrimPrefix(node.BlockID, "^"))
			malformed := strings.TrimSpace(strings.TrimPrefix(node.MalformedBlockID, "^"))
			switch ref.Kind {
			case NodeKindEmbedded:
				switch {
				case malformed != "":
					issues = append(issues, BlockIDValidationIssue{
						Code:     "malformed_block_id",
						NotePath: snapshot.NotePath,
						Ref:      ref,
						TypeName: ref.TypeName,
						Label:    node.Title,
						BlockID:  malformed,
						Message:  "embedded ontology node has a malformed block ID",
					})
				case rawBlockID == "":
					projection, err := ProjectBoundNodeFromSnapshot(snapshot, schema, ref)
					expected := ""
					if err == nil {
						expected = GenerateBlockIDForProjection(snapshot, projection)
					}
					issues = append(issues, BlockIDValidationIssue{
						Code:     "missing_embedded_block_id",
						NotePath: snapshot.NotePath,
						Ref:      ref,
						TypeName: ref.TypeName,
						Label:    node.Title,
						BlockID:  expected,
						Message:  "embedded ontology node is missing a durable block ID",
					})
				case !ValidBlockID(rawBlockID):
					issues = append(issues, BlockIDValidationIssue{
						Code:     "malformed_block_id",
						NotePath: snapshot.NotePath,
						Ref:      ref,
						TypeName: ref.TypeName,
						Label:    node.Title,
						BlockID:  rawBlockID,
						Message:  "embedded ontology node has a malformed block ID",
					})
				case counts[rawBlockID] > 1:
					issues = append(issues, BlockIDValidationIssue{
						Code:     "duplicate_block_id",
						NotePath: snapshot.NotePath,
						Ref:      ref,
						TypeName: ref.TypeName,
						Label:    node.Title,
						BlockID:  rawBlockID,
						Message:  "block ID is duplicated within the note",
					})
				}
			case NodeKindSection:
				// WHY: a `^block-id` on a section that the schema does not
				// classify as embedded is "stray" — the surrounding sweep no
				// longer promotes such sections to EMBEDDED so the anchor's
				// presence becomes visible drift instead of silently changing
				// node identity. Coderefs:
				// [[linkable-embedded-node-identifiers#^spec-0023-us5]]
				switch {
				case malformed != "":
					issues = append(issues, BlockIDValidationIssue{
						Code:     "malformed_block_id",
						NotePath: snapshot.NotePath,
						Ref:      ref,
						TypeName: ref.TypeName,
						Label:    node.Title,
						BlockID:  malformed,
						Message:  "structural section has a malformed block ID",
					})
				case rawBlockID != "":
					issues = append(issues, BlockIDValidationIssue{
						Code:     "stray_block_id_on_non_embedded_section",
						NotePath: snapshot.NotePath,
						Ref:      ref,
						TypeName: ref.TypeName,
						Label:    node.Title,
						BlockID:  rawBlockID,
						Message:  "block ID on a section the schema does not classify as embedded; remove it or change the schema role",
					})
				}
			}
			walk(node.Children)
		}
	}
	walk(snapshot.Sections)
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].NotePath != issues[j].NotePath {
			return issues[i].NotePath < issues[j].NotePath
		}
		return issues[i].Ref.StartByte < issues[j].Ref.StartByte
	})
	return issues
}
