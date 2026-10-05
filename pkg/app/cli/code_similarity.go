package actions

import (
	"context"
	"fmt"
	"sort"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semstore "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

const (
	defaultCodeSimilarityLimit     = 25
	defaultCodeSimilarityCandidate = 50
)

var (
	functionSimilarityKinds      = []string{"function", "func", "method"}
	functionSimilarityGranular   = []string{"signature_body", "signature_doc"}
	typeSimilarityKinds          = []string{"type", "struct", "interface", "class", "enum"}
	typeSimilarityGranularities  = []string{"decl_full", "decl_trunc"}
	codeSimilarityOwnerType      = "anchor"
	codeSimilarityMissingWarning = "No code similarity embeddings available; check that code embeddings are configured and enabled, then run `rzm index`."
)

// CodeSimilarityIntel is the indexed code/semantic surface used by CodeSimilarity.
type CodeSimilarityIntel interface {
	IntelAnchorMetas(ctx context.Context) ([]codeanchor.IntelAnchorMeta, error)
	IntelAnchorsByIDs(ctx context.Context, anchorIDs []string) (map[string]codeanchor.IntelAnchor, error)
	IntelChunksByOwners(ctx context.Context, ownerIDs []string) ([]codeanchor.IntelChunk, error)
	EmbeddingsByChunkIDs(ctx context.Context, chunkIDs []string) (map[string]embeddings.Embedding, error)
	SearchEmbeddings(ctx context.Context, query embeddings.Embedding, k int, filters semstore.EmbeddingSearchFilters) ([]semstore.ScoredChunk, int, error)
	SymbolRefsByPaths(ctx context.Context, paths []string) (map[string][]codeanchor.SymbolRefRow, error)
	Ancestors(ctx context.Context, fqn string) ([]string, error)
}

type CodeSimilarityOptions struct {
	VaultPath string
	Roots     []string
	Limit     int
}

type CodeSimilarityReport struct {
	VaultPath         string              `json:"vaultPath,omitempty"`
	Roots             []string            `json:"roots"`
	SimilarFunctions  []CodeSimilarityRow `json:"similarFunctions"`
	OverlappingTypes  []CodeSimilarityRow `json:"overlappingTypes"`
	Warnings          []string            `json:"warnings,omitempty"`
	SourceAnchorCount int                 `json:"sourceAnchorCount"`
}

type CodeSimilarityRow struct {
	Source         CodeSimilarityAnchor `json:"source"`
	Match          CodeSimilarityAnchor `json:"match"`
	EmbeddingScore float64              `json:"embeddingScore"`
	CombinedScore  float64              `json:"combinedScore"`
	SharedSignals  []string             `json:"sharedSignals,omitempty"`
	Reasons        []string             `json:"reasons,omitempty"`
}

type CodeSimilarityAnchor struct {
	AnchorID  string `json:"anchorId"`
	Lang      string `json:"lang,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Symbol    string `json:"symbol,omitempty"`
	FQN       string `json:"fqn,omitempty"`
	Path      string `json:"path,omitempty"`
	StartLine int64  `json:"startLine,omitempty"`
	EndLine   int64  `json:"endLine,omitempty"`
}

type codeSimilarityGroup struct {
	kinds        []string
	granular     []string
	weightStruct bool
}

type codeSimilarityCandidate struct {
	row          CodeSimilarityRow
	sourceID     string
	matchID      string
	matchChunkID string
}

// CodeSimilarity reports semantically similar function-like anchors and structurally overlapping type-like anchors.
func CodeSimilarity(ctx context.Context, intel CodeSimilarityIntel, opts CodeSimilarityOptions) (CodeSimilarityReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if intel == nil {
		return CodeSimilarityReport{}, fmt.Errorf("intel store is required")
	}
	if opts.Limit <= 0 {
		opts.Limit = defaultCodeSimilarityLimit
	}
	roots := normalizeSimilarityRoots(opts.Roots)
	report := CodeSimilarityReport{
		VaultPath:        opts.VaultPath,
		Roots:            roots,
		SimilarFunctions: []CodeSimilarityRow{},
		OverlappingTypes: []CodeSimilarityRow{},
	}

	metas, err := intel.IntelAnchorMetas(ctx)
	if err != nil {
		return report, err
	}
	sourceIDs := sourceAnchorIDsForRoots(metas, roots)
	report.SourceAnchorCount = len(sourceIDs)
	if len(sourceIDs) == 0 {
		report.Warnings = append(report.Warnings, "No code anchors found under report path (run `rzm index` or choose a different --path).")
		return report, nil
	}

	anchors, err := intel.IntelAnchorsByIDs(ctx, sourceIDs)
	if err != nil {
		return report, err
	}
	sourceIDs = filterSourceIDsWithAnchors(sourceIDs, anchors)
	sourceSet := make(map[string]struct{}, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		sourceSet[sourceID] = struct{}{}
	}
	if len(sourceIDs) == 0 {
		report.Warnings = append(report.Warnings, "No code anchors found under report path (run `rzm index` or choose a different --path).")
		return report, nil
	}

	sourceChunks, err := intel.IntelChunksByOwners(ctx, sourceIDs)
	if err != nil {
		return report, err
	}
	refs, ancestors := collectSimilaritySignals(ctx, intel, anchors)
	var functionEmbeddingsAvailable bool
	report.SimilarFunctions, functionEmbeddingsAvailable, err = runSimilarityGroup(ctx, intel, anchors, sourceSet, sourceChunks, refs, ancestors, opts.Limit, codeSimilarityGroup{
		kinds:    functionSimilarityKinds,
		granular: functionSimilarityGranular,
	})
	if err != nil {
		return report, err
	}
	var typeEmbeddingsAvailable bool
	report.OverlappingTypes, typeEmbeddingsAvailable, err = runSimilarityGroup(ctx, intel, anchors, sourceSet, sourceChunks, refs, ancestors, opts.Limit, codeSimilarityGroup{
		kinds:        typeSimilarityKinds,
		granular:     typeSimilarityGranularities,
		weightStruct: true,
	})
	if err != nil {
		return report, err
	}
	if len(report.SimilarFunctions) == 0 && len(report.OverlappingTypes) == 0 && !functionEmbeddingsAvailable && !typeEmbeddingsAvailable {
		report.Warnings = append(report.Warnings, codeSimilarityMissingWarning)
	}
	return report, nil
}

func runSimilarityGroup(ctx context.Context, intel CodeSimilarityIntel, anchors map[string]codeanchor.IntelAnchor, sourceSet map[string]struct{}, sourceChunks []codeanchor.IntelChunk, refs map[string]map[string]struct{}, ancestors map[string]map[string]struct{}, limit int, group codeSimilarityGroup) ([]CodeSimilarityRow, bool, error) {
	sourceChunksByOwner := primaryChunksByOwner(sourceChunks, group.granular)
	sourceChunkIDs := make([]string, 0, len(sourceChunksByOwner))
	for ownerID, chunk := range sourceChunksByOwner {
		if !kindIn(anchors[ownerID].Kind, group.kinds) {
			continue
		}
		sourceChunkIDs = append(sourceChunkIDs, chunk.ChunkID)
	}
	sourceVectors, err := intel.EmbeddingsByChunkIDs(ctx, sourceChunkIDs)
	if err != nil {
		return nil, false, err
	}
	if len(sourceVectors) == 0 {
		return nil, false, nil
	}

	candidateLimit := limit * 8
	if candidateLimit < defaultCodeSimilarityCandidate {
		candidateLimit = defaultCodeSimilarityCandidate
	}

	var candidates []codeSimilarityCandidate
	seen := make(map[string]struct{})
	embeddingsAvailable := false
	for sourceID, sourceChunk := range sourceChunksByOwner {
		source := anchors[sourceID]
		if !kindIn(source.Kind, group.kinds) {
			continue
		}
		vec := sourceVectors[sourceChunk.ChunkID]
		if len(vec) == 0 {
			continue
		}
		embeddingsAvailable = true
		scored, _, err := intel.SearchEmbeddings(ctx, vec, candidateLimit, semstore.EmbeddingSearchFilters{
			Kinds:       group.kinds,
			Granularity: group.granular,
			OwnerTypes:  []string{codeSimilarityOwnerType},
		})
		if err != nil {
			return nil, embeddingsAvailable, err
		}
		var missingIDs []string
		for _, sc := range scored {
			if sc.OwnerID == "" || sc.OwnerID == sourceID || sc.OwnerType != codeSimilarityOwnerType {
				continue
			}
			if _, ok := anchors[sc.OwnerID]; !ok {
				missingIDs = append(missingIDs, sc.OwnerID)
			}
		}
		if len(missingIDs) > 0 {
			fetched, err := intel.IntelAnchorsByIDs(ctx, missingIDs)
			if err != nil {
				return nil, embeddingsAvailable, err
			}
			for id, anchor := range fetched {
				anchors[id] = anchor
			}
			moreRefs, moreAncestors := collectSimilaritySignals(ctx, intel, fetched)
			mergeSignalMaps(refs, moreRefs)
			mergeSignalMaps(ancestors, moreAncestors)
		}
		for _, sc := range scored {
			if sc.OwnerID == "" || sc.OwnerID == sourceID || sc.OwnerType != codeSimilarityOwnerType {
				continue
			}
			match, ok := anchors[sc.OwnerID]
			if !ok || !kindIn(match.Kind, group.kinds) {
				continue
			}
			key := codeSimilarityPairKey(sourceID, sc.OwnerID)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			rowSource, rowMatch := canonicalSimilarityPair(source, match, sourceSet)
			row := buildCodeSimilarityRow(rowSource, rowMatch, sc.Score, refs[rowSource.FQN], refs[rowMatch.FQN], ancestors[rowSource.FQN], ancestors[rowMatch.FQN], group.weightStruct)
			candidates = append(candidates, codeSimilarityCandidate{row: row, sourceID: rowSource.AnchorID, matchID: rowMatch.AnchorID, matchChunkID: sc.ChunkID})
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].row.CombinedScore != candidates[j].row.CombinedScore {
			return candidates[i].row.CombinedScore > candidates[j].row.CombinedScore
		}
		if candidates[i].row.EmbeddingScore != candidates[j].row.EmbeddingScore {
			return candidates[i].row.EmbeddingScore > candidates[j].row.EmbeddingScore
		}
		if candidates[i].sourceID != candidates[j].sourceID {
			return candidates[i].sourceID < candidates[j].sourceID
		}
		return candidates[i].matchID < candidates[j].matchID
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	out := make([]CodeSimilarityRow, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.row)
	}
	return out, embeddingsAvailable, nil
}

// canonicalSimilarityPair gives each undirected pair a stable public
// orientation. Ranking still uses the same embedding and structural signals.
func canonicalSimilarityPair(a, b codeanchor.IntelAnchor, sourceSet map[string]struct{}) (codeanchor.IntelAnchor, codeanchor.IntelAnchor) {
	_, aIsSource := sourceSet[a.AnchorID]
	_, bIsSource := sourceSet[b.AnchorID]
	if aIsSource != bIsSource {
		if aIsSource {
			return a, b
		}
		return b, a
	}
	if compareSimilarityAnchor(a, b) <= 0 {
		return a, b
	}
	return b, a
}

func compareSimilarityAnchor(a, b codeanchor.IntelAnchor) int {
	if a.Path != b.Path {
		if a.Path < b.Path {
			return -1
		}
		return 1
	}
	if a.StartByte != b.StartByte {
		if a.StartByte < b.StartByte {
			return -1
		}
		return 1
	}
	if a.StartLine != b.StartLine {
		if a.StartLine < b.StartLine {
			return -1
		}
		return 1
	}
	if a.AnchorID < b.AnchorID {
		return -1
	}
	if a.AnchorID > b.AnchorID {
		return 1
	}
	return 0
}
