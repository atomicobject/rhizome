package unifiedsearch

import (
	"errors"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/search"
	searchplanner "github.com/atomicobject/rhizome/pkg/search/planner"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

var ErrTimedOutBeforePlanning = errors.New("search timed out before planning completed")

type Options struct {
	Config      *repoconfig.Resolved
	Query       string
	Queries     []string
	QueryInputs []QueryInput
	Seeds       []string
	Files       []string
	IntentInput string
	Limit       int
	Pack        bool
	// DeferPack leaves body hydration to the application layer after it selects
	// the visible canonical page. Direct Run callers retain immediate packing.
	DeferPack   bool
	BudgetChars int
	MaxPerOwner int
	SeedLimit   int
	UseVector   bool
	UseIntel    bool
	UseGraph    bool
	UseRefs     bool
	UseFTSBody  bool
	Filters     search.Filters
	// PathKinds is caller-supplied configured ownership for raw seed paths.
	// Persisted ownership is merged in when the Intel store is available.
	PathKinds map[string]search.PathKind
	// DirectorySeedExpansion and GraphSource let indexed-only adapters forbid
	// filesystem fallback. Zero values keep the default permissive behavior.
	DirectorySeedExpansion searchplanner.DirectorySeedExpansionPolicy
	GraphSource            retrieval.GraphSourcePolicy
	VaultPath              string
	VaultDef               obsidian.VaultDefinition
	EmbCfg                 embeddings.Config
	NoteFormats            noteformat.Runtime
	ProviderAPIKey         string
	IntelStore             *semdb.Store
	NoteProvider           embeddings.Provider
	NoteProviderConfig     embeddings.ProviderConfig
	CodeProvider           embeddings.Provider
	CodeProviderConfig     embeddings.ProviderConfig
}

type QueryInput struct {
	Text string
	Mode string
}

type Result struct {
	VaultPath         string
	Intent            search.Intent
	IntelStore        *semdb.Store
	Results           []search.RankedResult
	Display           []DisplayItem
	PackedText        string
	DeferredPacker    search.Packer
	PackSpec          search.QuerySpec
	Warnings          []search.Warning
	Lanes             []search.LaneStatus
	TargetStatus      search.TargetStatus
	TargetConfidence  float64
	TargetCandidates  []search.TargetCandidate
	ResolvedTarget    *search.TargetCandidate
	IndexGeneration   string
	VectorUnavailable bool
	Cleanup           func()
}

type DisplayItem struct {
	Primary       search.RankedResult
	NoteChunks    []search.RankedResult
	Anchor        *codeanchor.IntelAnchor
	ModuleExports []string
}

type cleanupStack struct {
	fns []func()
}

func (c *cleanupStack) Add(fn func()) {
	if fn == nil {
		return
	}
	c.fns = append(c.fns, fn)
}

func (c *cleanupStack) Close() {
	for i := len(c.fns) - 1; i >= 0; i-- {
		c.fns[i]()
	}
	c.fns = nil
}
