package query

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/vektah/gqlparser/v2/ast"
)

type PropertySource string

const (
	PropertySourceAny         PropertySource = "ANY"
	PropertySourceFrontmatter PropertySource = "FRONTMATTER"
	PropertySourceInline      PropertySource = "INLINE"
)

const (
	publicRootFirstMax  = 5000
	nestedFieldFirstMax = 200
)

type Error struct {
	Message    string         `json:"message"`
	Path       []string       `json:"path,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

type Result struct {
	Data        map[string]any `json:"data,omitempty"`
	Errors      []Error        `json:"errors,omitempty"`
	Extensions  map[string]any `json:"extensions,omitempty"`
	orderedData any
}

func (r Result) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	wrote := false
	data := any(r.Data)
	if r.orderedData != nil {
		data = r.orderedData
	}
	if data != nil {
		buf.WriteString(`"data":`)
		encoded, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		buf.Write(encoded)
		wrote = true
	}
	if len(r.Errors) > 0 {
		if wrote {
			buf.WriteByte(',')
		}
		buf.WriteString(`"errors":`)
		encoded, err := json.Marshal(r.Errors)
		if err != nil {
			return nil, err
		}
		buf.Write(encoded)
		wrote = true
	}
	if len(r.Extensions) > 0 {
		if wrote {
			buf.WriteByte(',')
		}
		buf.WriteString(`"extensions":`)
		encoded, err := json.Marshal(r.Extensions)
		if err != nil {
			return nil, err
		}
		buf.Write(encoded)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

type orderedField struct {
	key   string
	value any
}

type orderedObject []orderedField

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, field := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(field.key)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(field.value)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

type ExecutableSchema struct {
	SDL          string
	Schema       *ast.Schema
	RootTypes    map[string]string
	RuntimeRoots map[string]RuntimeRootKind
}

type PreparedQuery struct {
	Raw           string
	Document      *ast.QueryDocument
	Operation     *ast.OperationDefinition
	Variables     map[string]any
	UsesSemantic  bool
	UsesSearch    bool
	Introspection bool
	schema        *ast.Schema
}

// ExactNotePaths returns candidate canonical note paths addressed by an
// operation containing only singular note/node roots. The application must
// still confirm current note ownership; broad, mixed, and introspection
// operations return ok=false and require the complete model.
func (p *PreparedQuery) ExactNotePaths() (notePaths []string, ok bool) {
	return p.selectedNotePaths(true)
}

// SelectedNotePaths identifies singular canonical note roots, including node
// fragments and workspace selections. These still require the complete model.
func (p *PreparedQuery) SelectedNotePaths() ([]string, bool) {
	return p.selectedNotePaths(false)
}

func (p *PreparedQuery) selectedNotePaths(rawOnly bool) (notePaths []string, ok bool) {
	if p == nil || p.Operation == nil || p.Document == nil || p.Introspection {
		return nil, false
	}
	seenFragments := map[string]bool{}
	ok = true
	var visit func(ast.SelectionSet)
	visit = func(set ast.SelectionSet) {
		for _, selection := range set {
			switch current := selection.(type) {
			case *ast.Field:
				if current.Name != "note" && current.Name != "node" {
					ok = false
					continue
				}
				if rawOnly && selectionNeedsCompleteModel(p.schema, p.Document, current.SelectionSet, map[string]bool{}) {
					ok = false
					continue
				}
				args := current.ArgumentMap(p.Variables)
				raw := strings.TrimSpace(stringValue(args["path"]))
				if raw == "" {
					raw = strings.TrimSpace(stringValue(args["ref"]))
				}
				if hash := strings.IndexByte(raw, '#'); hash >= 0 {
					if rawOnly {
						ok = false
						continue
					}
					raw = raw[:hash]
				}
				canonical, err := paths.CleanNotePath(raw)
				if err != nil || canonical.String() != raw {
					ok = false
					continue
				}
				notePaths = append(notePaths, canonical.String())
			case *ast.InlineFragment:
				visit(current.SelectionSet)
			case *ast.FragmentSpread:
				if seenFragments[current.Name] {
					continue
				}
				seenFragments[current.Name] = true
				fragment := p.Document.Fragments.ForName(current.Name)
				if fragment == nil {
					ok = false
					continue
				}
				visit(fragment.SelectionSet)
			default:
				ok = false
			}
		}
	}
	visit(p.Operation.SelectionSet)
	return notePaths, ok && len(notePaths) > 0
}

func selectionNeedsCompleteModel(schema *ast.Schema, document *ast.QueryDocument, set ast.SelectionSet, seenFragments map[string]bool) bool {
	for _, selection := range set {
		switch current := selection.(type) {
		case *ast.Field:
			if fieldNeedsCompleteModel(schema, current) || selectionNeedsCompleteModel(schema, document, current.SelectionSet, seenFragments) {
				return true
			}
		case *ast.InlineFragment:
			if current.TypeCondition != "" || selectionNeedsCompleteModel(schema, document, current.SelectionSet, seenFragments) {
				return true
			}
		case *ast.FragmentSpread:
			if seenFragments[current.Name] {
				continue
			}
			seenFragments[current.Name] = true
			fragment := document.Fragments.ForName(current.Name)
			if fragment == nil || fragment.TypeCondition != "" || selectionNeedsCompleteModel(schema, document, fragment.SelectionSet, seenFragments) {
				return true
			}
		default:
			return true
		}
	}
	return false
}

func fieldNeedsCompleteModel(schema *ast.Schema, field *ast.Field) bool {
	if field == nil {
		return true
	}
	switch field.Name {
	case "__typename", "resolvedType":
		return true
	case "workspace", "neighborhood", "localGraph", "linked", "backlinked", "connected":
		return true
	}
	if field.Name == "typeName" && field.ObjectDefinition != nil && field.ObjectDefinition.Name == "NodeRef" {
		return true
	}
	if schema == nil || field.Definition == nil || field.Definition.Type == nil {
		return true
	}
	return typeNeedsCompleteModel(schema, field.Definition.Type.Name(), map[string]bool{})
}

func typeNeedsCompleteModel(schema *ast.Schema, typeName string, seen map[string]bool) bool {
	switch typeName {
	case "Node", "Note", "NoteNode", "Section", "CodeFile", "CodeSymbol":
		return true
	}
	if seen[typeName] {
		return false
	}
	seen[typeName] = true
	definition := schema.Types[typeName]
	if definition == nil {
		return false
	}
	for _, implemented := range schema.GetImplements(definition) {
		if implemented != nil && typeNeedsCompleteModel(schema, implemented.Name, seen) {
			return true
		}
	}
	return false
}

// Store is the query executor's indexed read contract.
//
// Keep this interface read-only. Query execution may fall back to note snapshots
// for missing records, but it must not update ontology, metadata, or linkability
// state while resolving a GraphQL request.
type Store interface {
	GetOntologyTypeByPath(context.Context, string) (semdb.OntologyNoteTypeRow, bool, error)
	GetOntologyAssessmentByPath(context.Context, string) (semdb.OntologyNoteAssessmentRow, bool, error)
	OntologyAssessmentsByPaths(context.Context, []string) (map[string]semdb.OntologyNoteAssessmentRow, error)
	OntologyAssessmentFlags(context.Context) (map[string]semdb.OntologyAssessmentFlags, error)
	GetOntologySchemaState(context.Context) (semdb.OntologySchemaState, error)
	OntologyTypesByPaths(context.Context, []string) (map[string]semdb.OntologyNoteTypeRow, error)
	OntologyPathsByType(context.Context, string, int) ([]string, error)
	OntologyNodesByIDs(context.Context, []string) (map[string]codeanchor.IntelOntologyNode, error)
	OntologyNodesByTypePlan(context.Context, codeanchor.OntologyNodeQueryPlan) ([]codeanchor.IntelOntologyNode, error)
	OntologyNodeFieldValuesByNodeIDs(context.Context, []string, []string) ([]codeanchor.IntelOntologyNodeFieldValue, error)
	OntologyEdgesForPaths(context.Context, []string, bool, string, int) ([]semdb.OntologyEdgeRow, error)
	OntologyStructuralEdgesBySources(context.Context, []string, string, int) ([]semdb.OntologyEdgeRow, error)
	OntologyAmbientEdgesBySources(context.Context, []string, string, int) ([]semdb.OntologyEdgeRow, error)
	OntologyAmbientEdgesByTargets(context.Context, []string, string, int) ([]semdb.OntologyEdgeRow, error)
	CurrentNoteMetadataRows(context.Context) ([]semdb.NoteMetadataRow, error)
	CurrentNoteMetadataRowsByPaths(context.Context, []string) (map[string]semdb.NoteMetadataRow, error)
	CurrentNotePropertyValues(context.Context, []string, []string, semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error)
	CurrentNoteTags(context.Context, []string) ([]semdb.NoteTagRow, error)
	CurrentNotePathsByPropertyValue(context.Context, string, string, semdb.NotePropertySource) ([]string, error)
	CurrentNoteAliases(context.Context) (map[string][]string, error)
	IntelAnchorsByPath(context.Context, string) ([]codeanchor.IntelAnchor, error)
}

type validationResultStore interface {
	GetValidationStateSnapshot(context.Context) (semdb.ValidationStateSnapshot, error)
	GetValidationDiagnosticsPage(context.Context, semdb.ValidationDiagnosticPageRequest) (semdb.ValidationDiagnosticPage, error)
	GetValidationScopeSummaries(context.Context, semdb.ValidationScopeSummaryRequest) (semdb.ValidationScopeSummaryResponse, error)
}

type SemanticSearcher interface {
	Search(context.Context, semantic.SearchRequest) ([]semantic.Result, error)
	SurveyNodesByType(context.Context, semantic.SurveyNodesByTypeRequest) ([]semantic.Result, error)
}

// NoteSearcher runs the shared unified search engine for the search root. It
// returns owning-note paths in engine rank order; the executor hydrates them.
type NoteSearcher interface {
	SearchNotes(ctx context.Context, request NoteSearchRequest) (NoteSearchResponse, error)
}

type NoteSearchRequest struct {
	Queries   []string // one facet per term; the engine aggregates facets
	NoteTypes []string // concrete owning-note type names; empty means any
	First     int      // maximum hits wanted
}

type NoteSearchHit struct {
	Path  string
	Score float64
}

type NoteSearchResponse struct {
	Hits     []NoteSearchHit
	Warnings []RuntimeWarning
}

type OntologyService interface {
	ResolvedType(context.Context, string) (semdb.OntologyNoteTypeRow, bool, error)
	Assessment(context.Context, string) (*ontology.NoteAssessment, bool, error)
}

type RuntimeRootKind string

const (
	RuntimeRootOntology RuntimeRootKind = "ontology"
	RuntimeRootCode     RuntimeRootKind = "code"
)

type RuntimeWarning struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Path    string `json:"path,omitempty"`
}

type OntologyRuntimeProvider interface {
	AuthoringGuide(context.Context, OntologyAuthoringGuideRequest) (OntologyAuthoringGuide, error)
	NextID(context.Context, OntologyNextIDRequest) (OntologyNextID, error)
	CurrentUser(context.Context) (OntologyCurrentUser, error)
}

type OntologyAuthoringGuideRequest struct {
	Type string
}

type OntologyAuthoringGuide struct {
	Type      string
	Markdown  string
	Available bool
	Warnings  []RuntimeWarning
}

type OntologyNextIDRequest struct {
	Type  string
	Count int
	Paths []string
}

type OntologyIDAllocation struct {
	Path          string
	ID            string
	Base          string
	Disambiguator *int
}

type OntologyNextID struct {
	Type            string
	IdentifierField string
	Strategy        ontology.IdentifierStrategy
	Source          string
	Prefix          string
	Separator       string
	Pad             *int
	Next            string
	IDs             []string
	Count           int
	Last            string
	CurrentMax      *int
	CurrentMaxValue string
	CurrentMaxOwner string
	OwnersScanned   int
	SharedWith      []string
	OwnersMatched   []string
	OwnersSkipped   []string
	Notes           []string
	Paths           []string
	Allocations     []OntologyIDAllocation
	Available       bool
	ErrorCode       string
	Error           string
	Warnings        []RuntimeWarning
}

type OntologyCurrentUser struct {
	Configured  bool
	Ref         string
	Found       bool
	ResolvedRef string
	Path        string
	Title       string
	TypeName    string
	ErrorCode   string
	Error       string
	Warnings    []RuntimeWarning
}

type OntologyQueryPlan struct {
	Type            string
	Root            string
	Execution       string
	Projection      string
	First           int
	PushedFilters   []string
	ResidualFilters []string
	PushedSort      []string
	ResidualSort    []string
	Warnings        []RuntimeWarning
}

type CodeRuntimeProvider interface {
	DocsForCode(context.Context, CodeRuntimeRequest) (CodeContextPack, error)
	CodeForNote(context.Context, CodeRuntimeRequest) (CodeContextPack, error)
	TestsForCode(context.Context, CodeRuntimeRequest) (CodeContextPack, error)
}

type CodeRuntimeRequest struct {
	Path  string
	First int
}

type RuntimePath struct {
	Path      string
	Title     string
	Kind      string
	Reason    string
	Snippet   string
	Content   string
	Line      int
	Truncated bool
}

type CodeContextPack struct {
	InputPath      string
	NormalizedPath string
	Available      bool
	Warnings       []RuntimeWarning
	Docs           []RuntimePath
	Notes          []RuntimePath
	Code           []RuntimePath
	Tests          []RuntimePath
}

// Deps wires the read-only GraphQL executor to vault, index, and optional
// semantic-search services.
type Deps struct {
	VaultDef                obsidian.VaultDefinition
	NoteReader              obsidian.NoteReader
	Store                   Store
	ExactNoteMetadataRows   func(context.Context, []string) (map[string]semdb.NoteMetadataRow, error)
	ExactNotePropertyValues func(context.Context, []string, []string, semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error)
	ExactNoteTags           func(context.Context, []string) ([]semdb.NoteTagRow, error)
	PathOwner               func(string) PathOwner
	SemanticSearcher        SemanticSearcher
	NoteSearcher            NoteSearcher
	Service                 OntologyService
	OntologyRuntime         OntologyRuntimeProvider
	CodeRuntime             CodeRuntimeProvider
	ReadOverlay             *ReadOverlay
	NoteFormats             noteformat.Runtime
}

// PathOwner is the configured, exclusive owner of a vault path. GraphQL uses
// it only to decide whether a missing note may fall back to code.
type PathOwner string

const (
	PathOwnerUnknown PathOwner = ""
	PathOwnerNote    PathOwner = "note"
	PathOwnerCode    PathOwner = "code"
)

// ReadOverlay aliases noderead's request/session-local read overlay so GraphQL
// dependencies can accept staged edit-session previews without importing web.
type ReadOverlay = noderead.ReadOverlay

type noteRecord struct {
	Path                   string
	Title                  string
	Content                string
	Frontmatter            map[string]any
	InlineProps            map[string][]string
	Tags                   []string
	TypeName               string
	Format                 noteformat.FormatID
	SourceRepresentation   ontology.SourceRepresentation
	EvidenceRepresentation ontology.EvidenceRepresentation
	Capabilities           []noteformat.Capability
	Score                  *float64
}

type sectionRecord struct {
	Node       *ontology.SectionNode
	Projection *ontology.NodeProjection
	Note       *noteRecord
	TypeName   string

	// catalogRef carries the full NodeRef from the indexed catalog row when the
	// record was hydrated from the catalog rather than a live projection. The
	// SectionNode struct does not preserve the structural fingerprint, so
	// without this field sectionNodeRef would rebuild a Structural-less ref and
	// downstream identity matching (e.g. view hydrateRows) would miss.
	catalogRef ontology.NodeRef

	// PropertyCase is the enclosing note type's case convention. Section types
	// themselves have no propertyCase — their inline-key derivation is driven
	// by whichever note type is referencing them, so we carry that context on
	// the record as we descend through nested sections.
	PropertyCase ontology.PropertyCase

	// inlineProps caches the Dataview-style `Key:: Value` pairs found inside
	// this section's own content (descendant ranges subtracted). It is
	// populated lazily on the first call to fieldValues because most queries
	// don't read section scalar fields and parsing is cheap but not free.
	inlineProps       map[string][]string
	inlinePropsLoaded bool
	indexedFields     map[string][]codeanchor.IntelOntologyNodeFieldValue
}

type codeRecord struct {
	Path    string
	Title   string
	Lang    string
	Symbols []codeanchor.IntelAnchor
	Anchor  *codeanchor.IntelAnchor
}

// fieldValues returns the inline-property values for a section scalar field.
// The inline key is derived from the enclosing note type's propertyCase so
// the same section type can be reused under note types with different casing
// conventions.
func (s *sectionRecord) fieldValues(field *ontology.Field, propertyCase ontology.PropertyCase) []string {
	if s == nil || field == nil {
		return nil
	}
	if len(s.indexedFields) > 0 {
		if rows := s.indexedFields[strings.ToLower(strings.TrimSpace(field.Name))]; len(rows) > 0 {
			out := make([]string, 0, len(rows))
			for _, row := range rows {
				out = append(out, row.ValueText)
			}
			return normalizeStrings(out)
		}
	}
	if s.Projection != nil {
		if binding, ok := s.Projection.Fields[field.Name]; ok {
			return normalizeStrings(binding.Values)
		}
	}
	s.ensureInlinePropsLoaded()
	propertyName := ontology.DefaultPropertyName(field.Name, propertyCase)
	return normalizeStrings(caseInsensitiveInlineLookup(s.inlineProps, propertyName))
}

func (s *sectionRecord) hasFieldValue(field *ontology.Field, propertyCase ontology.PropertyCase) bool {
	if s == nil || field == nil {
		return false
	}
	if s.Projection != nil {
		if binding, ok := s.Projection.Fields[field.Name]; ok {
			return binding.Present || len(binding.Values) > 0
		}
	}
	s.ensureInlinePropsLoaded()
	propertyName := ontology.DefaultPropertyName(field.Name, propertyCase)
	return caseInsensitiveInlineHasKey(s.inlineProps, propertyName)
}

func (s *sectionRecord) ensureInlinePropsLoaded() {
	if s == nil || s.inlinePropsLoaded {
		return
	}
	s.inlinePropsLoaded = true
	if s.Node == nil {
		return
	}
	s.inlineProps = ontology.SectionInlineProperties(ontology.SectionOwnContent(s.Node))
}

func (n *noteRecord) hasField(field *ontology.Field) bool {
	if n == nil || field == nil {
		return false
	}
	switch field.SourceKind {
	case ontology.FieldSourceInline:
		for _, name := range ontology.FieldSourceNames(field) {
			if caseInsensitiveInlineHasKey(n.InlineProps, name) {
				return true
			}
		}
		return false
	default:
		for _, name := range ontology.FieldSourceNames(field) {
			if caseInsensitiveFrontmatterHasKey(n.Frontmatter, name) {
				return true
			}
		}
		return false
	}
}

func (n *noteRecord) cloneFrontmatter() map[string]any {
	if len(n.Frontmatter) == 0 {
		return nil
	}
	out := make(map[string]any, len(n.Frontmatter))
	for k, v := range n.Frontmatter {
		out[k] = v
	}
	return out
}

func (n *noteRecord) cloneTags() []string {
	if len(n.Tags) == 0 {
		return []string{}
	}
	out := make([]string, len(n.Tags))
	copy(out, n.Tags)
	return out
}

func (n *noteRecord) fieldValues(field *ontology.Field) []string {
	if n == nil || field == nil {
		return nil
	}
	switch field.SourceKind {
	case ontology.FieldSourceInline:
		for _, name := range ontology.FieldSourceNames(field) {
			if values := normalizeStrings(caseInsensitiveInlineLookup(n.InlineProps, name)); len(values) > 0 {
				return values
			}
		}
		return nil
	default:
		for _, name := range ontology.FieldSourceNames(field) {
			value := caseInsensitiveFrontmatterLookup(n.Frontmatter, name)
			if value == nil {
				continue
			}
			values := flattenValueStrings(value)
			if len(values) > 0 || caseInsensitiveFrontmatterHasKey(n.Frontmatter, name) {
				return values
			}
		}
		return nil
	}
}

func caseInsensitiveFrontmatterLookup(values map[string]any, key string) any {
	if len(values) == 0 {
		return nil
	}
	if value, ok := values[key]; ok {
		return value
	}
	needle := strings.ToLower(strings.TrimSpace(key))
	for currentKey, value := range values {
		if strings.ToLower(strings.TrimSpace(currentKey)) == needle {
			return value
		}
	}
	return nil
}

func caseInsensitiveFrontmatterHasKey(values map[string]any, key string) bool {
	if len(values) == 0 {
		return false
	}
	if _, ok := values[key]; ok {
		return true
	}
	needle := strings.ToLower(strings.TrimSpace(key))
	for currentKey := range values {
		if strings.ToLower(strings.TrimSpace(currentKey)) == needle {
			return true
		}
	}
	return false
}

func caseInsensitiveInlineLookup(values map[string][]string, key string) []string {
	if len(values) == 0 {
		return nil
	}
	if value, ok := values[key]; ok {
		return value
	}
	needle := strings.ToLower(strings.TrimSpace(key))
	for currentKey, value := range values {
		if strings.ToLower(strings.TrimSpace(currentKey)) == needle {
			return value
		}
	}
	return nil
}

func caseInsensitiveInlineHasKey(values map[string][]string, key string) bool {
	if len(values) == 0 {
		return false
	}
	if _, ok := values[key]; ok {
		return true
	}
	needle := strings.ToLower(strings.TrimSpace(key))
	for currentKey := range values {
		if strings.ToLower(strings.TrimSpace(currentKey)) == needle {
			return true
		}
	}
	return false
}
