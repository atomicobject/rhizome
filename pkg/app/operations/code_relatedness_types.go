package operations

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/anchors"
	semstore "github.com/atomicobject/rhizome/pkg/store"
)

type CodeRelatednessIntel interface {
	CallsFromFile(ctx context.Context, file string) ([]codeanchor.CallSite, error)
	SymbolsByFile(ctx context.Context, file string) ([]string, error)
	Ancestors(ctx context.Context, fqn string) ([]string, error)
	AnchorsBySymbols(ctx context.Context, symbols []string) (map[int64][]string, error)
	AnchorsByCallFiles(ctx context.Context, files []string) ([]int64, error)
	AnchorsByPathPrefix(ctx context.Context, target string) ([]int64, error)
	AnchorsByGlobMatch(ctx context.Context, target string) ([]int64, error)
	Anchors(ctx context.Context) ([]codeanchor.Anchor, error)
	NotesForAnchor(ctx context.Context, anchorID int64) ([]codeanchor.Note, error)
	IndexedFilePaths(ctx context.Context) ([]string, error)
	GraphDocScoresByPaths(ctx context.Context, paths []string) (map[string]semstore.GraphDocScore, error)
	AnchorScoresByIDs(ctx context.Context, anchorIDs []string) (map[string]float64, error)
}

type CodeRelatednessOptions struct {
	VaultPath        string
	Roots            []string
	Excludes         []string
	MaxRelated       int
	ClusterThreshold int
	IncludeTests     bool
	CriticalLimit    int
	HotFilesLimit    int
}

type CodeRelatednessReport struct {
	VaultPath string   `json:"vaultPath"`
	Roots     []string `json:"roots"`
	Module    string   `json:"module"`

	Packages []CodeRelatednessPackage `json:"packages"`
	Clusters []CodeRelatednessCluster `json:"clusters"`

	GodCandidates []CodeRelatednessGodCandidate `json:"godCandidates"`

	// CriticalSymbols ranks exported symbols that are called from other packages.
	CriticalSymbols []CodeRelatednessSymbol `json:"criticalSymbols,omitempty"`

	// DocGapCount is the number of critical symbols with no documentation coverage.
	DocGapCount int `json:"docGapCount,omitempty"`

	// CouplingEdges lists the strongest directed cross-package coupling edges.
	CouplingEdges []CodeRelatednessCouplingEdge `json:"couplingEdges,omitempty"`
}

type CodeRelatednessCluster struct {
	Packages []string `json:"packages"`
}

type CodeRelatednessGodCandidate struct {
	Package          string `json:"package"`
	Dir              string `json:"dir"`
	Files            int    `json:"files"`
	ImportFanIn      int    `json:"importFanIn"`
	ImportFanOut     int    `json:"importFanOut"`
	CallFanIn        int    `json:"callFanIn"`
	CallFanOut       int    `json:"callFanOut"`
	AnchorLabels     int    `json:"anchorLabels"`
	LinkedNotes      int    `json:"linkedNotes"`
	CombinedGodScore int    `json:"combinedGodScore"`
}

type CodeRelatednessPackage struct {
	ImportPath string `json:"importPath"`
	Dir        string `json:"dir"`
	Files      int    `json:"files"`

	ImportsInternal int `json:"importsInternal"`
	ImportedBy      int `json:"importedBy"`
	CallsInternal   int `json:"callsInternal"`
	CalledBy        int `json:"calledBy"`

	AnchorLabels int `json:"anchorLabels"`
	LinkedNotes  int `json:"linkedNotes"`

	Langs           []string `json:"langs,omitempty"`
	GraphAuthority  float64  `json:"graphAuthority,omitempty"`
	GraphHub        float64  `json:"graphHub,omitempty"`
	AnchorPageRank  float64  `json:"anchorPageRank,omitempty"`
	GraphSignalHint float64  `json:"-"`

	TopRelated []CodeRelatednessEdge `json:"topRelated"`

	// HotFiles highlights individual files that drive cross-package coupling.
	HotFiles []CodeRelatednessFileHotspot `json:"hotFiles,omitempty"`
}

type CodeRelatednessEdge struct {
	To                 string `json:"to"`
	Score              int    `json:"score"`
	ImportsBothWays    int    `json:"importsBothWays"`
	CallsBothWays      int    `json:"callsBothWays"`
	SharedAnchorLabels int    `json:"sharedAnchorLabels"`

	// CallsTo counts cross-package callsites from this package to the destination package.
	CallsTo int `json:"callsTo,omitempty"`
}

type CodeRelatednessCouplingEdge struct {
	FromPkg string `json:"fromPkg"`
	ToPkg   string `json:"toPkg"`

	CallsTo  int  `json:"callsTo"`
	Imports  bool `json:"imports"`
	BothWays bool `json:"bothWays"`

	ImportFiles    int                                 `json:"importFiles,omitempty"`
	ImportStmts    int                                 `json:"importStmts,omitempty"`
	TopImportFiles []CodeRelatednessCouplingCallerFile `json:"topImportFiles,omitempty"`

	TopCallees     []CodeRelatednessCouplingCallee     `json:"topCallees,omitempty"`
	TopCallerFiles []CodeRelatednessCouplingCallerFile `json:"topCallerFiles,omitempty"`
}

type CodeRelatednessCouplingCallee struct {
	FQN      string   `json:"fqn"`
	Calls    int      `json:"calls"`
	Kind     string   `json:"kind,omitempty"`
	File     string   `json:"file,omitempty"`
	Exported bool     `json:"exported,omitempty"`
	Anchors  []string `json:"anchors,omitempty"`
	Mentions int      `json:"mentions,omitempty"`
	Links    int      `json:"links,omitempty"`
}

type CodeRelatednessCouplingCallerFile struct {
	File  string `json:"file"`
	Calls int    `json:"calls"`
}

type CodeRelatednessFileHotspot struct {
	File            string   `json:"file"`
	CallsOutOfPkg   int      `json:"callsOutOfPkg"`
	DistinctCallees int      `json:"distinctCalleePkgs"`
	CalleePkgs      []string `json:"calleePkgs,omitempty"`
}

type CodeRelatednessCallerPkg struct {
	Pkg   string `json:"pkg"`
	Calls int    `json:"calls"`
	Files int    `json:"files"`
}

type CodeRelatednessCallerFile struct {
	File  string `json:"file"`
	Pkg   string `json:"pkg"`
	Calls int    `json:"calls"`
}

type CodeRelatednessSymbol struct {
	Lang string `json:"lang"`
	FQN  string `json:"fqn"`
	Pkg  string `json:"pkg"`
	Name string `json:"name"`

	Calls          int `json:"calls"`
	CallerPackages int `json:"callerPackages"`
	CallerFiles    int `json:"callerFiles"`

	TopCallerPackages []CodeRelatednessCallerPkg  `json:"topCallerPackages,omitempty"`
	TopCallerFiles    []CodeRelatednessCallerFile `json:"topCallerFiles,omitempty"`
	TopCallerShare    float64                     `json:"topCallerShare,omitempty"`

	MatchingAnchors int      `json:"matchingAnchors,omitempty"`
	MatchingNotes   int      `json:"matchingNotes,omitempty"`
	AnchorLabels    []string `json:"anchorLabels,omitempty"`
	NotePaths       []string `json:"notePaths,omitempty"`

	DocMentions int    `json:"docMentions,omitempty"`
	DocLinks    int    `json:"docLinks,omitempty"`
	DocResolved bool   `json:"docResolved,omitempty"`
	DocKind     string `json:"docKind,omitempty"`
	DocPath     string `json:"docPath,omitempty"`

	// DocGap indicates a high-fanin symbol with no documentation coverage.
	// True when the symbol has multiple caller packages but no anchors, notes, or doc links.
	DocGap bool `json:"docGap,omitempty"`
}

type crossCallKey struct {
	callerPkg string
	calleePkg string
}

type symKey struct {
	lang codeanchor.Lang
	pkg  string
	name string
}

type symAgg struct {
	calls       int
	callerPkgs  map[string]struct{}
	callerFiles map[string]struct{}
	callsByPkg  map[string]int
	callsByFile map[string]int
}

type fileHotspotAgg struct {
	calls      int
	calleePkgs map[string]struct{}
}
