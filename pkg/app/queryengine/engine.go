// Package queryengine composes the shared GraphQL execution ports.
package queryengine

import (
	"context"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
)

type Engine struct {
	schema     *ontoschema.Schema
	executable *ontologyquery.ExecutableSchema
	deps       ontologyquery.Deps
}

func New(schema *ontoschema.Schema, executable *ontologyquery.ExecutableSchema, deps ontologyquery.Deps) (*Engine, error) {
	if !executable.MatchesOntologySchema(schema) {
		return nil, fmt.Errorf("ontology query executable does not match its source schema")
	}
	return &Engine{schema: schema, executable: executable, deps: deps}, nil
}

func (e *Engine) Run(ctx context.Context, prepared *ontologyquery.PreparedQuery) (ontologyquery.Result, error) {
	if !e.executable.MatchesOntologySchema(e.schema) {
		return ontologyquery.Result{}, fmt.Errorf("ontology query schema changed after compilation")
	}
	return ontologyquery.ExecutePrepared(ctx, e.deps, e.schema, e.executable, prepared), nil
}

// Dependencies keeps custom capabilities explicit while sharing search adapters.
// Callers pass the same vault and store to both query and search ports.
func Dependencies(base ontologyquery.Deps, search unifiedsearch.Options) (ontologyquery.Deps, error) {
	if base.Store != nil && base.Store != search.IntelStore {
		return ontologyquery.Deps{}, fmt.Errorf("ontology query and search stores differ")
	}
	if base.VaultDef.BasePath() != "" && search.VaultDef.BasePath() != "" && filepath.Clean(base.VaultDef.BasePath()) != filepath.Clean(search.VaultDef.BasePath()) {
		return ontologyquery.Deps{}, fmt.Errorf("ontology query and search vaults differ")
	}
	if search.VaultPath != "" && search.VaultDef.BasePath() != "" && filepath.Clean(search.VaultPath) != filepath.Clean(search.VaultDef.BasePath()) {
		return ontologyquery.Deps{}, fmt.Errorf("ontology query search path differs from its vault")
	}
	base.VaultDef = search.VaultDef
	search.NoteFormats = base.NoteFormats
	if search.IntelStore != nil {
		base.Store = search.IntelStore
		if base.SemanticSearcher == nil && search.NoteProvider != nil {
			base.SemanticSearcher = &semantic.Searcher{NoteProvider: search.NoteProvider, IntelStore: search.IntelStore}
		}
		if base.NoteSearcher == nil {
			base.NoteSearcher = unifiedsearch.NoteSearcher{Runtime: search}
		}
	}
	return base, nil
}
