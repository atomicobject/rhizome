package retrieval

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// GraphSourcePolicy controls whether graph retrieval may fall back to live
// vault discovery when persisted graph projections are unavailable.
type GraphSourcePolicy uint8

const (
	GraphSourceAllowFilesystemFallback GraphSourcePolicy = iota
	GraphSourceIndexedOnly
)

// GraphRetriever expands note seeds through the legacy note graph boundary:
// persisted doc scores/edges when fresh, otherwise Obsidian wikilink graph analysis.
// It intentionally emits note-level candidates only; ontology node diffusion lives
// in OntologyRetriever and GraphPPRRetriever.
type GraphRetriever struct {
	VaultDef     obsidian.VaultDefinition
	NoteReader   obsidian.NoteReader
	Store        *semdb.Store
	Options      obsidian.GraphAnalysisOptions
	SourcePolicy GraphSourcePolicy

	NeighborLimit      int
	SameCommunityLimit int
}

func (r *GraphRetriever) Name() string { return "graph" }

func (r *GraphRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if len(spec.Seeds) == 0 {
		return nil, nil
	}
	if r.VaultDef.BasePath() == "" {
		return nil, nil
	}
	if r.SourcePolicy != GraphSourceIndexedOnly && r.NoteReader == nil {
		return nil, nil
	}

	hasNoteSeeds := false
	for _, seed := range spec.Seeds {
		if seed.Kind == knowledge.KindNote || seed.Kind == knowledge.KindNoteChunk {
			hasNoteSeeds = true
			break
		}
	}
	if !hasNoteSeeds {
		return nil, nil
	}

	if r.Store != nil {
		if out, ok, err := r.retrieveFromStore(ctx, spec); err != nil {
			return nil, err
		} else if ok {
			return out, nil
		}
	}

	analysis, err := r.graph(ctx)
	if err != nil {
		return nil, err
	}
	if analysis == nil || len(analysis.Nodes) == 0 {
		return nil, nil
	}

	neighborLimit := r.NeighborLimit
	if neighborLimit <= 0 {
		neighborLimit = 15
	}
	communityLimit := r.SameCommunityLimit
	if communityLimit <= 0 {
		communityLimit = 25
	}

	communitiesByID := make(map[string]obsidian.CommunitySummary, len(analysis.Communities))
	for _, c := range analysis.Communities {
		communitiesByID[c.ID] = c
	}

	var out []search.Candidate
	seen := make(map[string]struct{})

	for _, seed := range spec.Seeds {
		if seed.Kind != knowledge.KindNote && seed.Kind != knowledge.KindNoteChunk {
			continue
		}
		seedPath, ok := cleanTypedNotePath(seed.ID)
		if !ok {
			continue
		}

		node, ok := analysis.Nodes[seedPath]
		if !ok {
			continue
		}

		neighborsAdded := 0
		for _, nb := range node.Neighbors {
			if neighborsAdded >= neighborLimit {
				break
			}
			nb, valid := cleanTypedNotePath(nb)
			if !valid {
				continue
			}
			if nb == "" || nb == seedPath {
				continue
			}
			key := "note:" + nb
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			title := nodeTitle(nb)
			if n, ok := analysis.Nodes[nb]; ok && strings.TrimSpace(n.Title) != "" {
				title = n.Title
			}
			out = append(out, search.Candidate{
				Handle: knowledge.NoteHandle(nb),
				Owner:  knowledge.NoteHandle(nb),
				Evidence: []search.Evidence{{
					Type:     "graph_proximity",
					RawScore: 1.0,
					Source:   "pkg/obsidian/graph",
					Details: map[string]string{
						"seed": seedPath,
						"kind": "neighbor",
					},
				}},
				Type:       "note",
				NoteID:     nb,
				Path:       nb,
				Title:      title,
				ChunkIndex: -1,
			})
			neighborsAdded++
		}

		if node.Community == "" {
			continue
		}
		comm, ok := communitiesByID[node.Community]
		if !ok {
			continue
		}
		added := 0
		for _, member := range comm.Nodes {
			if added >= communityLimit {
				break
			}
			member, ok = cleanTypedNotePath(member)
			if !ok {
				continue
			}
			if member == "" || member == seedPath {
				continue
			}
			key := "note:" + member
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			title := nodeTitle(member)
			if n, ok := analysis.Nodes[member]; ok && strings.TrimSpace(n.Title) != "" {
				title = n.Title
			}
			out = append(out, search.Candidate{
				Handle: knowledge.NoteHandle(member),
				Owner:  knowledge.NoteHandle(member),
				Evidence: []search.Evidence{{
					Type:     "same_community",
					RawScore: 0.7,
					Source:   "pkg/obsidian/graph",
					Details: map[string]string{
						"seed":      seedPath,
						"community": node.Community,
					},
				}},
				Type:       "note",
				NoteID:     member,
				Path:       member,
				Title:      title,
				ChunkIndex: -1,
			})
			added++
		}
	}

	return out, nil
}

func (r *GraphRetriever) graph(ctx context.Context) (*obsidian.GraphAnalysis, error) {
	opts := r.Options
	if r.SourcePolicy == GraphSourceIndexedOnly {
		started := time.Now()
		snapshot, err := notemeta.LoadReadyPersistedGraphSnapshot(ctx, r.Store)
		addTiming(ctx, search.TimingEvent{
			Name: "graph.snapshot.load", Kind: "graph", Started: started,
			Duration: time.Since(started), Status: timingStatus(err), Err: timingErr(err),
		})
		if err != nil {
			return nil, fmt.Errorf("load indexed graph snapshot: %w", err)
		}
		if snapshot == nil {
			search.AddRuntimeWarning(ctx, search.Warning{
				Code:    "indexed_graph_unavailable",
				Kind:    "index_unavailable",
				Source:  "graph",
				Message: "Indexed graph metadata is unavailable; graph expansion was skipped without scanning the vault.",
			})
			return nil, nil
		}
		started = time.Now()
		analysis := obsidian.ComputeGraphAnalysisFromSnapshot(snapshot, opts)
		addTiming(ctx, search.TimingEvent{
			Name: "graph.snapshot.analyze", Kind: "graph", Started: started,
			Duration: time.Since(started), Status: "ok",
		})
		return analysis, nil
	}
	started := time.Now()
	if _, snapshotBacked := r.NoteReader.(obsidian.NoteEntriesProvider); !snapshotBacked {
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpRepoWalks, 1)
	}
	a, err := obsidian.ComputeGraphAnalysis(r.VaultDef, r.NoteReader, opts)
	addTiming(ctx, search.TimingEvent{
		Name: "graph.filesystem", Kind: "graph", Started: started,
		Duration: time.Since(started), Status: timingStatus(err), Err: timingErr(err),
	})
	if err != nil {
		return nil, fmt.Errorf("compute graph analysis: %w", err)
	}
	return a, nil
}

func (r *GraphRetriever) retrieveFromStore(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, bool, error) {
	seedPaths := make([]string, 0, len(spec.Seeds))
	for _, seed := range spec.Seeds {
		if seed.Kind != knowledge.KindNote && seed.Kind != knowledge.KindNoteChunk {
			continue
		}
		path, ok := cleanTypedNotePath(seed.ID)
		if ok {
			seedPaths = append(seedPaths, path)
		}
	}
	if len(seedPaths) == 0 {
		return nil, false, nil
	}

	started := time.Now()
	fresh, err := r.storeGraphFresh(ctx)
	addTiming(ctx, search.TimingEvent{
		Name: "graph.store.freshness", Kind: "graph", Started: started,
		Duration: time.Since(started), Status: timingStatus(err), Err: timingErr(err),
	})
	if err != nil {
		return nil, false, err
	}
	if !fresh {
		return nil, false, nil
	}

	started = time.Now()
	seedScores, err := r.Store.GraphDocScoresByPaths(ctx, seedPaths)
	addTiming(ctx, search.TimingEvent{
		Name: "graph.store.seed_scores", Kind: "graph", Started: started,
		Duration: time.Since(started), Status: timingStatus(err), Err: timingErr(err),
	})
	if err != nil {
		return nil, false, err
	}
	for _, seedPath := range seedPaths {
		if _, ok := seedScores[seedPath]; !ok {
			return nil, false, nil
		}
	}
	started = time.Now()
	edges, err := r.Store.GraphDocNoteEdgesBySources(ctx, seedPaths)
	addTiming(ctx, search.TimingEvent{
		Name: "graph.store.edges", Kind: "graph", Started: started,
		Duration: time.Since(started), Status: timingStatus(err), Err: timingErr(err),
	})
	if err != nil {
		return nil, false, err
	}

	neighborLimit := r.NeighborLimit
	if neighborLimit <= 0 {
		neighborLimit = 15
	}
	communityLimit := r.SameCommunityLimit
	if communityLimit <= 0 {
		communityLimit = 25
	}

	neighborsBySeed := make(map[string][]string, len(seedPaths))
	candidatePaths := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		src, srcOK := cleanTypedNotePath(edge.SrcPath)
		dst, dstOK := cleanTypedNotePath(edge.DstPath)
		if !srcOK || !dstOK || src == dst {
			continue
		}
		neighborsBySeed[src] = append(neighborsBySeed[src], dst)
		candidatePaths[dst] = struct{}{}
	}

	communityIDs := make([]string, 0, len(seedPaths))
	seenCommunity := make(map[string]struct{}, len(seedPaths))
	for _, seedPath := range seedPaths {
		score, ok := seedScores[seedPath]
		if !ok || strings.TrimSpace(score.Community) == "" {
			continue
		}
		if _, ok := seenCommunity[score.Community]; ok {
			continue
		}
		seenCommunity[score.Community] = struct{}{}
		communityIDs = append(communityIDs, score.Community)
	}
	started = time.Now()
	communityMembers, err := r.Store.GraphDocNoteScoresByCommunities(ctx, communityIDs, communityLimit+1)
	addTiming(ctx, search.TimingEvent{
		Name: "graph.store.communities", Kind: "graph", Started: started,
		Duration: time.Since(started), Status: timingStatus(err), Err: timingErr(err),
	})
	if err != nil {
		return nil, false, err
	}
	for _, members := range communityMembers {
		for _, member := range members {
			if path, ok := cleanTypedNotePath(member.DocPath); ok {
				candidatePaths[path] = struct{}{}
			}
		}
	}

	pathsList := make([]string, 0, len(candidatePaths))
	for path := range candidatePaths {
		pathsList = append(pathsList, path)
	}
	started = time.Now()
	rowsByPath, err := r.Store.CurrentNoteMetadataRowsByPaths(ctx, pathsList)
	addTiming(ctx, search.TimingEvent{
		Name: "graph.store.metadata", Kind: "graph", Started: started,
		Duration: time.Since(started), Status: timingStatus(err), Err: timingErr(err),
	})
	if err != nil {
		return nil, false, err
	}

	seen := make(map[string]struct{})
	out := make([]search.Candidate, 0, len(candidatePaths))
	for _, seedPath := range seedPaths {
		neighborsAdded := 0
		for _, nb := range neighborsBySeed[seedPath] {
			if neighborsAdded >= neighborLimit {
				break
			}
			cleaned, ok := cleanTypedNotePath(nb)
			if !ok {
				continue
			}
			nb = cleaned
			if nb == "" || nb == seedPath {
				continue
			}
			key := "note:" + nb
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, buildGraphCandidate("graph_proximity", 1.0, seedPath, "", nb, rowsByPath))
			neighborsAdded++
		}

		score, ok := seedScores[seedPath]
		if !ok || strings.TrimSpace(score.Community) == "" {
			continue
		}
		added := 0
		for _, member := range communityMembers[score.Community] {
			memberPath, ok := cleanTypedNotePath(member.DocPath)
			if !ok {
				continue
			}
			if added >= communityLimit {
				break
			}
			if memberPath == "" || memberPath == seedPath {
				continue
			}
			key := "note:" + memberPath
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, buildGraphCandidate("same_community", 0.7, seedPath, score.Community, memberPath, rowsByPath))
			added++
		}
	}

	return out, true, nil
}

func (r *GraphRetriever) storeGraphFresh(ctx context.Context) (bool, error) {
	if r.Store == nil {
		return false, nil
	}
	noteState, err := r.Store.GetNoteMetadataState(ctx)
	if err != nil {
		return false, err
	}
	if !noteState.Ready || noteState.LoadedAt == 0 {
		return false, nil
	}
	scoreState, err := r.Store.GraphDocScoresSummary(ctx)
	if err != nil {
		return false, err
	}
	if scoreState.NoteCount == 0 || scoreState.NoteUpdatedAt == 0 {
		return false, nil
	}
	requiredUpdatedAt := (noteState.LoadedAt + int64(time.Second) - 1) / int64(time.Second)
	return scoreState.NoteUpdatedAt >= requiredUpdatedAt, nil
}

func buildGraphCandidate(kind string, score float64, seedPath, community, path string, rowsByPath map[string]semdb.NoteMetadataRow) search.Candidate {
	title := nodeTitle(path)
	if row, ok := rowsByPath[path]; ok && strings.TrimSpace(row.Title) != "" {
		title = row.Title
	}
	details := map[string]string{
		"seed": seedPath,
		"kind": "neighbor",
	}
	if kind == "same_community" {
		details = map[string]string{
			"seed":      seedPath,
			"community": community,
		}
	}
	return search.Candidate{
		Handle: knowledge.NoteHandle(path),
		Owner:  knowledge.NoteHandle(path),
		Evidence: []search.Evidence{{
			Type:     kind,
			RawScore: score,
			Source:   "pkg/obsidian/graph",
			Details:  details,
		}},
		Type:       "note",
		NoteID:     path,
		Path:       path,
		Title:      title,
		ChunkIndex: -1,
	}
}

func nodeTitle(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
