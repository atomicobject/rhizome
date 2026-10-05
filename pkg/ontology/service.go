package ontology

import (
	"context"
	"strings"
	"sync"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Service is the lightweight process-level ontology facade used by CLI/web
// callers that need current type/assessment docs.
//
// It intentionally caches only stable decoded rows and type docs. Request-shaped
// hydration, traversal, graph, and projection caches belong in noderead.Scope.
type Service struct {
	VaultDef   obsidian.VaultDefinition
	NoteReader obsidian.NoteReader
	Store      *semdb.Store
	Schema     *Schema

	mu               sync.RWMutex
	assessmentByPath map[string]*NoteAssessment
	assessmentKnown  map[string]bool
	typeDocByName    map[string]*TypeDoc
}

// NewService wires a store-backed ontology facade.
func NewService(vaultDef obsidian.VaultDefinition, noteReader obsidian.NoteReader, store *semdb.Store, schema *Schema) *Service {
	return &Service{
		VaultDef:         vaultDef,
		NoteReader:       noteReader,
		Store:            store,
		Schema:           schema,
		assessmentByPath: make(map[string]*NoteAssessment),
		assessmentKnown:  make(map[string]bool),
		typeDocByName:    make(map[string]*TypeDoc),
	}
}

// ResolvedType returns the indexed type row for a note path.
func (s *Service) ResolvedType(ctx context.Context, notePath string) (semdb.OntologyNoteTypeRow, bool, error) {
	if s == nil || s.Store == nil {
		return semdb.OntologyNoteTypeRow{}, false, nil
	}
	return s.Store.GetOntologyTypeByPath(ctx, notePath)
}

// Assessment returns the decoded indexed validation assessment for a note path.
func (s *Service) Assessment(ctx context.Context, notePath string) (*NoteAssessment, bool, error) {
	if s == nil || s.Store == nil {
		return nil, false, nil
	}
	s.mu.RLock()
	assessment, ok := s.assessmentByPath[notePath]
	known := s.assessmentKnown[notePath]
	s.mu.RUnlock()
	if known {
		return assessment, ok, nil
	}

	row, ok, err := s.Store.GetOntologyAssessmentByPath(ctx, notePath)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		s.mu.Lock()
		s.assessmentKnown[notePath] = true
		s.mu.Unlock()
		return nil, false, nil
	}
	assessment, err = decodeAssessment(row.AssessmentJSON)
	if err != nil || assessment == nil {
		return nil, false, err
	}
	s.mu.Lock()
	s.assessmentByPath[notePath] = assessment
	s.assessmentKnown[notePath] = true
	s.mu.Unlock()
	return assessment, true, nil
}

func (s *Service) TypeDocs(typeName string) ([]TypeDoc, error) {
	if s == nil {
		return nil, nil
	}
	return SchemaDocs(s.Schema, typeName)
}

func (s *Service) TypeDoc(typeName string) (*TypeDoc, error) {
	if s == nil {
		return nil, nil
	}
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return nil, nil
	}
	s.mu.RLock()
	if doc, ok := s.typeDocByName[typeName]; ok {
		s.mu.RUnlock()
		return doc, nil
	}
	s.mu.RUnlock()

	docs, err := SchemaDocs(s.Schema, typeName)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, nil
	}
	doc := docs[0]
	s.mu.Lock()
	s.typeDocByName[typeName] = &doc
	s.mu.Unlock()
	return &doc, nil
}
