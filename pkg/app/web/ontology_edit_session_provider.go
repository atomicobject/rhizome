package web

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func (s *Server) newOntologyEditSession(schema *ontoschema.Schema) *ontology.EditSession {
	formats, err := s.noteMetadata.FormatRuntime()
	if err != nil {
		return ontology.NewEditSession(s.cfg.VaultDef, &obsidian.Note{}, schema)
	}
	return ontology.NewProviderAwareEditSession(s.cfg.VaultDef, &obsidian.Note{}, schema, formats)
}
