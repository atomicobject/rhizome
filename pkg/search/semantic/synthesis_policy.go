package semantic

import "github.com/atomicobject/rhizome/pkg/anchors"

// SynthesisPolicy determines how intel anchors + source text are turned into embedding chunks.
// Implementations must be deterministic and safe for concurrent use.
type SynthesisPolicy interface {
	// BuildModuleChunks returns module/file chunks for a file path.
	BuildModuleChunks(path string, lang codeanchor.Lang, anchors []codeanchor.IntelAnchor, fileContent []byte) []SemanticChunk

	// BuildAnchorChunks returns chunks for a specific non-module anchor.
	// Must always include a primary chunk with Index=0.
	BuildAnchorChunks(anchor codeanchor.IntelAnchor, fileContent []byte, relatedTitles []string, context SymbolChunkContext) []SemanticChunk
}

// CallSensitiveSynthesisPolicy reports whether chunk text for a module or anchor
// depends on derived cross-file call hints. Policies that don't implement this
// are treated conservatively when call lookups are enabled.
type CallSensitiveSynthesisPolicy interface {
	ModuleChunkUsesCalls(path string, lang codeanchor.Lang, anchors []codeanchor.IntelAnchor, fileContent []byte) bool
	AnchorChunkUsesCalls(anchor codeanchor.IntelAnchor, fileContent []byte, relatedTitles []string) bool
}

// AnchorIndexDecider lets policies suppress low-signal direct anchor surfaces.
// Suppressed anchors are still planned as prune-only tasks when stale chunks
// exist, so policy changes remove old vectors deterministically.
type AnchorIndexDecider interface {
	ShouldIndexAnchor(anchor codeanchor.IntelAnchor) bool
}

type PolicyOptions struct {
	Budget ChunkBudget

	MaxPrimaryChars int
	ShortBodyLines  int
}
