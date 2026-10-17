package planner

import (
	"github.com/atomicobject/rhizome/pkg/noteformat"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Deps supplies persisted search indexes and application-owned format and packing services.
type Deps struct {
	Semantic   *semantic.Searcher
	IntelStore *semdb.Store
	Corpus     *search.CorpusSnapshot
	Packer     search.Packer // Application-owned presentation adapter

	VaultPath       string
	VaultDef        obsidian.VaultDefinition
	NoteReader      obsidian.NoteReader
	GraphNoteReader obsidian.NoteReader
	DocPatterns     []string
	OntologySchema  *ontoschema.Schema
	NoteFormats     noteformat.Runtime
}

// DirectorySeedExpansionPolicy controls whether directory seed expansion may
// inspect the filesystem after indexed prefix lookups return no candidates.
type DirectorySeedExpansionPolicy uint8

const (
	DirectorySeedExpansionAllowFilesystemFallback DirectorySeedExpansionPolicy = iota
	DirectorySeedExpansionIndexedOnly
)

// Options control which retrieval strategies are enabled and per-owner result limits.
//
// Default behavior: if Options{} is zero-valued, all strategies are enabled with MaxPerOwner=3.
//
// Strategy flags:
//   - EnableVector: enables VectorRetriever and SeedVectorRetriever (requires Deps.Semantic)
//   - EnableIntel: enables IntelLexicalRetriever (requires Deps.IntelStore)
//   - EnableGraph: enables GraphRetriever and OutgoingLinkRetriever (requires Deps.NoteReader)
//   - EnableRefs: enables refs-based retrievers (DocLinksRetriever, CodeAnchorNotesRetriever, etc.; requires Deps.IntelStore)
//
// Diversity:
//   - MaxPerOwner: limits how many results come from the same owner (note/file); default 3, minimum 1.
//     Prevents single notes/files from dominating results. See [Search (Hub)](docs/hubs/Search (Hub).md) for details.
type Options struct {
	EnableVector bool
	EnableIntel  bool
	EnableGraph  bool
	EnableRefs   bool

	DirectorySeedExpansion DirectorySeedExpansionPolicy
	GraphSource            retrieval.GraphSourcePolicy

	MaxPerOwner int
}

// Plan is the ready-to-run production search service. Planning never executes
// retrieval; callers execute the returned service with the same query spec.
type Plan = search.Service

// Planner translates QuerySpec into a Plan based on available indexes and intent.
//
// The planner does NOT execute retrieval; it only builds a Plan that search.Service.Search()
// runs. Responsibilities include:
//   - Seed expansion (directory → files, file → notes/anchors)
//   - Retriever selection (based on options, query shape, available indexes)
//   - Weight tuning (intent-specific ranking weights)
//   - Auto-expansion (text-only queries use top hits as implicit seeds)
//
// See [Search (Hub)](docs/hubs/Search (Hub).md), [Search - Seed expansion strategies](docs/reference/analysis/Search - Seed expansion strategies.md), and [Search - Intent and weight tuning](docs/reference/domain/Search - Intent and weight tuning.md) for details.
type Planner struct {
	Deps    Deps
	Options Options
}
