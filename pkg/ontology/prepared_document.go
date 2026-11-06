package ontology

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/paths"
)

type SourcePresence string

const (
	SourceMissing SourcePresence = "missing"
	SourceNull    SourcePresence = "null"
	SourcePresent SourcePresence = "value"
)

type SourceValueOccurrence struct {
	Value   noteformat.ValueID
	Address string
}
type RecordDiagnostic struct {
	Code, Message                string
	Owner                        noteformat.OwnerKey
	Address, TypeName, FieldName string
}

// PreparedDocument owns one captured provider result and one binding/resolution
// pass. Markdown syntax remains an explicit edit/body view of those same facts.
type PreparedDocument struct {
	root                 *RootDocumentSnapshot
	schema               *ontoschema.Schema
	structure            *noteformat.StructureFacts
	owners               map[noteformat.OwnerKey]*noteformat.OwnerFact
	ownerByValue         map[noteformat.ValueID]noteformat.OwnerKey
	markdownOwners       map[string]map[int]noteformat.OwnerKey
	properties           map[noteformat.ValueID]map[string]noteformat.ValueID
	foldedProperties     map[noteformat.ValueID]map[string]noteformat.ValueID
	selectedRecordValues map[noteformat.ValueID]bool
	markdown             *projectionResolver
	rootProjection       *NodeProjection
	records              []*recordProjection
	recordByID           map[string][]*recordProjection
	diagnostics          []RecordDiagnostic
	resolverBuilds       int
}

func PrepareDocument(root *RootDocumentSnapshot, schema *ontoschema.Schema) (*PreparedDocument, error) {
	if root == nil || schema == nil {
		return nil, fmt.Errorf("source and schema are required")
	}
	doc := noteDocForRoot(root)
	snapshot, err := markdownEditView(root)
	if err != nil {
		return nil, err
	}
	doc.Snapshot = snapshot
	prepared, err := prepareDocumentForDoc(doc, schema, resolveNoteAssessment(doc, schema))
	if err != nil {
		return nil, err
	}
	BindRecordDocuments(prepared)
	return prepared, nil
}

func noteDocForRoot(root *RootDocumentSnapshot) *noteDoc {
	return &noteDoc{Path: root.NotePath.String(), Title: root.Title, Content: string(root.RawSource), Frontmatter: cloneAnyMap(root.Metadata), Inline: cloneNoteInline(root.InlineProperties), Tags: append([]string(nil), root.Tags...), Links: append([]notemeta.ResolvedNoteLink(nil), root.Links...), Projection: root.Projection.Copy(), TypeName: stringValue(root.Metadata[ontoschema.TypeFieldName()]), Root: root}
}

func prepareDocumentForDoc(doc *noteDoc, schema *ontoschema.Schema, assessment *NoteAssessment) (*PreparedDocument, error) {
	if doc == nil || schema == nil {
		return nil, fmt.Errorf("document and schema are required")
	}
	if doc.Root == nil && doc.Snapshot != nil {
		doc.Root = markdownRootForSnapshot(doc.Snapshot)
	}
	if doc.Root == nil {
		return nil, fmt.Errorf("captured source is required")
	}
	p := &PreparedDocument{root: doc.Root, schema: schema, structure: doc.Root.Projection.Facts.Structure, owners: map[noteformat.OwnerKey]*noteformat.OwnerFact{}, ownerByValue: map[noteformat.ValueID]noteformat.OwnerKey{}, markdownOwners: map[string]map[int]noteformat.OwnerKey{}, properties: map[noteformat.ValueID]map[string]noteformat.ValueID{}, recordByID: map[string][]*recordProjection{}, resolverBuilds: 1}
	p.foldedProperties = map[noteformat.ValueID]map[string]noteformat.ValueID{}
	p.selectedRecordValues = map[noteformat.ValueID]bool{}
	if p.structure != nil {
		for i := range p.structure.Owners {
			o := &p.structure.Owners[i]
			p.owners[o.Key] = o
			if o.Markdown != nil {
				id := o.Markdown.SourceID
				if p.markdownOwners[id] == nil {
					p.markdownOwners[id] = map[int]noteformat.OwnerKey{}
				}
				p.markdownOwners[id][o.Range.Range.StartByte] = o.Key
			}
			if o.Value != 0 {
				p.ownerByValue[o.Value] = o.Key
			}
		}
		for i, v := range p.structure.Values {
			if v.Kind == noteformat.MetadataObject {
				props := map[string]noteformat.ValueID{}
				for _, entry := range v.Entries {
					props[sourceScalarText(entry.Key)] = entry.Value
				}
				p.properties[noteformat.ValueID(i+1)] = props
				names := make([]string, 0, len(props))
				for name := range props {
					names = append(names, name)
				}
				sort.Strings(names)
				folded := map[string]noteformat.ValueID{}
				for _, name := range names {
					key := strings.ToLower(name)
					if _, exists := folded[key]; !exists {
						folded[key] = props[name]
					}
				}
				p.foldedProperties[noteformat.ValueID(i+1)] = folded
			}
		}
	}
	if doc.Snapshot != nil {
		resolver, err := newProjectionResolverForDoc(doc.Snapshot, schema, doc, assessment)
		if err != nil {
			return nil, err
		}
		resolver.prepared = p
		p.markdown = resolver
	}
	root, err := projectRootNodeWithAssessment(doc, schema, assessment, p.markdown)
	if err != nil {
		return nil, err
	}
	p.rootProjection = p.attach(root)
	if err := p.compileRecordProjections(doc); err != nil {
		return nil, err
	}
	p.validateRecordFields()
	for _, record := range p.records {
		if record.valid {
			p.selectedRecordValues[record.selection.value] = true
		}
	}
	return p, nil
}

func markdownRootForSnapshot(s *DocumentSnapshot) *RootDocumentSnapshot {
	if s.root != nil {
		return s.root
	}
	facts := s.SelectorFacts()
	root := &RootDocumentSnapshot{NotePath: paths.NotePath(s.NotePath), Format: "markdown", RawSource: []byte(s.Content), ContentHash: s.ContentFingerprint, Title: facts.Title, Metadata: cloneAnyMap(facts.Frontmatter), InlineProperties: cloneNoteInline(facts.Inline), Tags: append([]string(nil), facts.Tags...), Projection: noteformat.Projection{Status: noteformat.ProjectionStatusCurrent, Facts: noteformat.ProjectionFacts{Structure: s.structure}}, Root: NodeRef{NotePath: s.NotePath, Kind: NodeKindNote}}
	s.root = root
	return root
}

func prepareMarkdownSnapshot(s *DocumentSnapshot, schema *ontoschema.Schema) (*PreparedDocument, error) {
	if s == nil {
		return nil, fmt.Errorf("Markdown snapshot is required")
	}
	doc := noteDocFromSnapshot(s)
	doc.Root = markdownRootForSnapshot(s)
	return prepareDocumentForDoc(doc, schema, resolveNoteAssessment(doc, schema))
}

func (p *PreparedDocument) attach(projection *NodeProjection) *NodeProjection {
	if projection == nil {
		return nil
	}
	projection.Prepared = p
	projection.RootSnapshot = p.root
	if p.structure != nil {
		if projection.Ref.Kind == NodeKindNote {
			projection.OwnerKey = p.structure.Root
		} else {
			projection.OwnerKey = p.markdownOwners[projection.Ref.NodeID][projection.Ref.StartByte]
		}
	}
	return projection
}

func (p *PreparedDocument) Project(ref NodeRef) (*NodeProjection, error) {
	return p.project(ref, false)
}
func (p *PreparedDocument) ProjectBound(ref NodeRef) (*NodeProjection, error) {
	return p.project(ref, true)
}
func (p *PreparedDocument) project(ref NodeRef, bound bool) (*NodeProjection, error) {
	if p == nil || ref.NotePath != p.root.NotePath.String() {
		return nil, fmt.Errorf("ref does not belong to prepared host")
	}
	if isRecordSelector(ref.Fragment) || strings.HasPrefix(ref.NodeID, "record-v1-") || strings.HasPrefix(ref.NodeID, "record-pos-v1-") {
		return p.projectRecord(ref, bound)
	}
	if p.markdown != nil {
		var node *NodeProjection
		var err error
		if bound {
			node, err = p.markdown.projectCurrent(ref)
		} else {
			node, err = p.markdown.project(ref)
		}
		return p.attach(node), err
	}
	if ref.Kind == NodeKindNote || (ref.Kind == "" && ref.Fragment == "" && ref.NodeID == "") {
		return p.rootProjection, nil
	}
	return nil, fmt.Errorf("source provider has no such owner")
}

func (p *PreparedDocument) Children(ref NodeRef) ([]NodeRef, error) {
	node, err := p.Project(ref)
	if err != nil {
		return nil, err
	}
	var out []NodeRef
	seen := map[NodeRef]bool{}
	names := make([]string, 0, len(node.Fields))
	for name := range node.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, child := range node.Fields[name].ChildRefs {
			if !seen[child] {
				out = append(out, child)
				seen[child] = true
			}
		}
	}
	if ref.Kind == NodeKindNote {
		for _, record := range p.records {
			if record.parent == nil && !seen[record.projection.Ref] {
				out = append(out, record.projection.Ref)
				seen[record.projection.Ref] = true
			}
		}
	}
	return out, nil
}

func (p *PreparedDocument) Nodes() ([]*NodeProjection, error) {
	var out []*NodeProjection
	seen := map[NodeRef]bool{}
	var walk func(*NodeProjection) error
	walk = func(node *NodeProjection) error {
		if node == nil || seen[node.Ref] {
			return nil
		}
		seen[node.Ref] = true
		out = append(out, node)
		names := make([]string, 0, len(node.Fields))
		for name := range node.Fields {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			for _, ref := range node.Fields[name].ChildRefs {
				child, err := p.ProjectBound(ref)
				if err != nil {
					return err
				}
				if err = walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(p.rootProjection); err != nil {
		return nil, err
	}
	if p.markdown != nil {
		for _, ref := range p.markdown.globalSourceNodeRefs() {
			node, err := p.ProjectBound(ref)
			if err != nil {
				return nil, err
			}
			if err = walk(node); err != nil {
				return nil, err
			}
		}
	}
	for _, record := range p.records {
		if !record.valid {
			continue
		}
		if err := walk(record.projection); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (p *PreparedDocument) SemanticParent(ref NodeRef) (NodeRef, bool, error) {
	if isRecordSelector(ref.Fragment) {
		record, err := p.recordForRef(ref)
		if err != nil {
			return NodeRef{}, false, err
		}
		if record.parent != nil {
			return record.parent.projection.Ref, true, nil
		}
		return p.rootProjection.Ref, true, nil
	}
	if p.markdown == nil || ref.Kind == NodeKindNote {
		return NodeRef{}, false, nil
	}
	return semanticMarkdownParent(p.markdown, ref)
}
func (p *PreparedDocument) Source() *RootDocumentSnapshot { return p.root }
func (p *PreparedDocument) Owner(key noteformat.OwnerKey) (noteformat.OwnerFact, bool) {
	owner, ok := p.owners[key]
	if !ok {
		return noteformat.OwnerFact{}, false
	}
	s := (&noteformat.StructureFacts{Owners: []noteformat.OwnerFact{*owner}}).Copy()
	return s.Owners[0], true
}
func (p *PreparedDocument) Diagnostics() []RecordDiagnostic {
	return append([]RecordDiagnostic(nil), p.diagnostics...)
}

// Prepare adapts an explicitly Markdown edit/preview snapshot, preserving its captured facts.
func (s *DocumentSnapshot) Prepare(schema *ontoschema.Schema) (*PreparedDocument, error) {
	return prepareMarkdownSnapshot(s, schema)
}

func (p *PreparedDocument) GlobalSourceRefs() ([]NodeRef, error) {
	var refs []NodeRef
	if p.markdown != nil {
		refs = append(refs, p.markdown.globalSourceNodeRefs()...)
	}
	for _, record := range p.records {
		if record.valid {
			refs = append(refs, record.projection.Ref)
		}
	}
	return refs, nil
}
