package perfworkload

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

const IndexFilename = "search-perf.sqlite"

type FixtureCounts struct {
	Owners     int `json:"owners"`
	CodeOwners int `json:"codeOwners"`
	DocOwners  int `json:"docOwners"`
	Chunks     int `json:"chunks"`
	Embeddings int `json:"embeddings"`
	GraphEdges int `json:"graphEdges"`
}

func Prepare(ctx context.Context, root string, manifest Manifest) (FixtureCounts, error) {
	if err := manifest.Validate(); err != nil {
		return FixtureCounts{}, err
	}
	if err := requireEmptyDestination(root); err != nil {
		return FixtureCounts{}, err
	}
	if err := os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755); err != nil {
		return FixtureCounts{}, err
	}
	store, err := semdb.Open(IndexPath(root))
	if err != nil {
		return FixtureCounts{}, err
	}
	defer func() {
		if store != nil {
			_ = store.Close()
		}
	}()

	if err := populateOwners(ctx, root, store, manifest); err != nil {
		return FixtureCounts{}, err
	}
	if err := populateNoteGraph(ctx, store, manifest); err != nil {
		return FixtureCounts{}, err
	}
	if err := populateChunks(ctx, store, manifest); err != nil {
		return FixtureCounts{}, err
	}
	if err := store.SetIndexerVersion(ctx, codeanchor.IndexerVersion); err != nil {
		return FixtureCounts{}, err
	}
	if err := store.SetPackMetadata(ctx, codeanchor.PackMetadata{
		ConfigHash: "search-perf-v1", ModelHash: manifest.Embeddings.Model,
		AlgoVersion: "search-perf-v1", IndexerVersion: codeanchor.IndexerVersion,
	}); err != nil {
		return FixtureCounts{}, err
	}
	counts, err := ValidateFixture(ctx, store, manifest)
	if err != nil {
		return FixtureCounts{}, err
	}
	// Vector tables are dimension-specific and created after the base schema.
	// Reopening through the writer validates the completed physical schema and
	// publishes the proof required by production read-only runtimes.
	if err := store.Close(); err != nil {
		return FixtureCounts{}, err
	}
	store = nil
	validated, err := semdb.Open(IndexPath(root))
	if err != nil {
		return FixtureCounts{}, fmt.Errorf("finalize workload schema proof: %w", err)
	}
	if err := validated.Close(); err != nil {
		return FixtureCounts{}, err
	}
	return counts, nil
}

func IndexPath(root string) string { return filepath.Join(root, ".rhizome", IndexFilename) }

func requireEmptyDestination(root string) error {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("workload destination must be empty: %s", root)
	}
	return nil
}

func populateOwners(ctx context.Context, root string, store *semdb.Store, manifest Manifest) error {
	const batchSize = 250
	for start := 0; start < manifest.Owners.Code; start += batchSize {
		end := min(start+batchSize, manifest.Owners.Code)
		batch := codeanchor.CodePersistenceBatch{}
		for i := start; i < end; i++ {
			path, id, symbol, text := codeOwner(i)
			if err := writeFixtureFile(root, path, "package workload\n\n// "+text+"\nfunc "+symbol+"() {}\n"); err != nil {
				return err
			}
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
			anchor := codeanchor.IntelAnchor{AnchorID: id, Lang: codeanchor.LangGo, Kind: "function", Path: path, Symbol: symbol, FQN: "workload." + symbol, StartLine: 4, EndLine: 4, Fingerprint: hash}
			edges := make([]codeanchor.IntelEdge, 0, manifest.Graph.EdgesPerCodeOwner)
			for distance := 1; distance <= manifest.Graph.EdgesPerCodeOwner && i-distance >= 0; distance++ {
				_, targetID, _, _ := codeOwner(i - distance)
				edges = append(edges, codeanchor.IntelEdge{SrcID: id, DstID: targetID, Kind: "calls"})
			}
			batch.Metas = append(batch.Metas, codeanchor.FileMeta{Path: path, Lang: codeanchor.LangGo, Hash: hash, ParseStatus: codeanchor.ParseOK})
			batch.IntelReps = append(batch.IntelReps, codeanchor.IntelCodeFileReplace{Path: path, Anchors: []codeanchor.IntelAnchor{anchor}, Edges: edges, FTSRows: []codeanchor.IntelFTSRow{{ItemType: "anchor", ItemID: id, Path: path, Title: symbol, Body: text}}})
		}
		if err := store.ApplyCodePersistenceBatch(ctx, batch); err != nil {
			return fmt.Errorf("persist code owners %d-%d: %w", start, end, err)
		}
	}
	for start := 0; start < manifest.Owners.Prose; start += batchSize {
		end := min(start+batchSize, manifest.Owners.Prose)
		notes := make([]codeanchor.NoteWithKeepLabels, 0, end-start)
		reps := make([]codeanchor.IntelDocReplace, 0, end-start)
		for i := start; i < end; i++ {
			path, id, title, text := proseOwner(i)
			content := "# " + title + "\n\n" + text + "\n"
			if err := writeFixtureFile(root, path, content); err != nil {
				return err
			}
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
			notes = append(notes, codeanchor.NoteWithKeepLabels{Note: codeanchor.Note{Path: path, Title: title}, ContentHash: hash, IndexerVersion: codeanchor.IndexerVersion, Mtime: time.Now().Unix()})
			section := codeanchor.IntelDocSection{SectionID: id, Path: path, Title: title, Level: 1, Content: text, Fingerprint: hash}
			reps = append(reps, codeanchor.IntelDocReplace{Path: path, Sections: []codeanchor.IntelDocSection{section}, FTSRows: []codeanchor.IntelFTSRow{{ItemType: "doc_section", ItemID: id, Path: path, Title: title, Body: text}}})
		}
		if _, err := store.UpsertNotesWithCleanupBatch(ctx, notes); err != nil {
			return fmt.Errorf("persist prose metadata %d-%d: %w", start, end, err)
		}
		if err := store.ReplaceIntelDocSectionsBatch(ctx, reps); err != nil {
			return fmt.Errorf("persist prose owners %d-%d: %w", start, end, err)
		}
	}
	return nil
}

func populateChunks(ctx context.Context, store *semdb.Store, manifest Manifest) error {
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: manifest.Embeddings.Provider, Model: manifest.Embeddings.Model, Dimensions: manifest.Embeddings.Dimensions})
	const ownerBatch = 200
	for start := 0; start < manifest.Owners.Total; start += ownerBatch {
		end := min(start+ownerBatch, manifest.Owners.Total)
		ownerIDs := make([]string, 0, end-start)
		chunks := make([]codeanchor.IntelChunk, 0, (end-start)*manifest.Owners.ChunksPerOwner)
		texts := make([]string, 0, cap(chunks))
		for owner := start; owner < end; owner++ {
			ownerID, ownerType, heading, text := ownerIdentity(owner, manifest.Owners.Code)
			ownerIDs = append(ownerIDs, ownerID)
			for ord := 0; ord < manifest.Owners.ChunksPerOwner; ord++ {
				chunkID := fmt.Sprintf("chunk-%06d-%02d", owner, ord)
				chunkText := text
				if ord > 0 {
					chunkText = fmt.Sprintf("%s detail %02d", text, ord)
				}
				hash := fmt.Sprintf("%x", sha256.Sum256([]byte(chunkText)))
				chunks = append(chunks, codeanchor.IntelChunk{ChunkID: chunkID, OwnerID: ownerID, OwnerType: ownerType, Ord: ord, Granularity: map[bool]string{true: "symbol", false: "section"}[ownerType == "anchor"], Breadcrumb: heading, Heading: heading, ContentHash: hash})
				texts = append(texts, chunkText)
			}
		}
		if err := store.ReplaceIntelChunks(ctx, ownerIDs, chunks); err != nil {
			return fmt.Errorf("persist chunks %d-%d: %w", start, end, err)
		}
		vectors, err := provider.EmbedTexts(ctx, texts)
		if err != nil {
			return err
		}
		rows := make(map[string]embeddings.Embedding, len(chunks))
		for i := range chunks {
			rows[chunks[i].ChunkID] = vectors[i]
		}
		if err := store.UpsertEmbeddings(ctx, rows); err != nil {
			return fmt.Errorf("persist embeddings %d-%d: %w", start, end, err)
		}
	}
	return nil
}

func populateNoteGraph(ctx context.Context, store *semdb.Store, manifest Manifest) error {
	loadedAt := time.Now().UnixNano()
	updatedAt := (loadedAt + int64(time.Second) - 1) / int64(time.Second)
	notes := make([]semdb.NoteMetadataRow, 0, manifest.Owners.Prose)
	edges := make([]semdb.GraphDocEdgeRow, 0, manifest.Owners.Prose*manifest.Graph.EdgesPerNoteOwner)
	scores := make([]semdb.GraphDocScore, 0, manifest.Owners.Prose)
	for i := 0; i < manifest.Owners.Prose; i++ {
		path, _, title, text := proseOwner(i)
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
		notes = append(notes, semdb.NoteMetadataRow{Path: path, Title: title, ContentHash: hash, IndexerVersion: codeanchor.IndexerVersion, Mtime: updatedAt, Size: int64(len(text)), IndexedAt: loadedAt, FormatID: "markdown", Projection: semdb.NoteProjectionState{ProviderVersion: "search-perf-v1", ProjectionVersion: "search-perf-v1", Status: semdb.NoteProjectionStatusCurrent, SourceContentHash: hash, UpdatedAt: updatedAt}})
		for distance := 1; distance <= manifest.Graph.EdgesPerNoteOwner; distance++ {
			targetPath, _, _, _ := proseOwner((i + distance) % manifest.Owners.Prose)
			edges = append(edges, semdb.GraphDocEdgeRow{SrcPath: path, DstPath: targetPath, Kind: semdb.GraphDocEdgeKindWikilink})
		}
		scores = append(scores, semdb.GraphDocScore{DocPath: path, DocType: "note", Hub: 0.5, Authority: float64(manifest.Owners.Prose-i) / float64(manifest.Owners.Prose), Community: fmt.Sprintf("domain-%02d", i%20), Inbound: manifest.Graph.EdgesPerNoteOwner, Outbound: manifest.Graph.EdgesPerNoteOwner, UpdatedAt: updatedAt})
	}
	if err := store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{State: semdb.NoteMetadataState{NotesHash: "search-perf-notes-v1", RawNotesHash: "search-perf-notes-v1", LoadedAt: loadedAt, Ready: true}, Notes: notes, WikilinkEdges: edges}); err != nil {
		return fmt.Errorf("persist note graph metadata: %w", err)
	}
	if err := store.ReplaceGraphDocScores(ctx, scores); err != nil {
		return fmt.Errorf("persist note graph scores: %w", err)
	}
	return nil
}

func ValidateFixture(ctx context.Context, store *semdb.Store, manifest Manifest) (FixtureCounts, error) {
	var counts FixtureCounts
	queries := []struct {
		query string
		dest  *int
	}{
		{"SELECT COUNT(*) FROM intel_code_anchors", &counts.CodeOwners},
		{"SELECT COUNT(*) FROM intel_doc_sections", &counts.DocOwners},
		{"SELECT COUNT(*) FROM intel_chunks", &counts.Chunks},
		{"SELECT COUNT(*) FROM intel_embeddings", &counts.Embeddings},
		{"SELECT (SELECT COUNT(*) FROM intel_edges) + (SELECT COUNT(*) FROM graph_doc_edges)", &counts.GraphEdges},
	}
	for _, item := range queries {
		if err := store.DB().QueryRowContext(ctx, item.query).Scan(item.dest); err != nil {
			return FixtureCounts{}, err
		}
	}
	counts.Owners = counts.CodeOwners + counts.DocOwners
	if counts.Owners != manifest.Owners.Total || counts.CodeOwners != manifest.Owners.Code || counts.DocOwners != manifest.Owners.Prose || counts.Chunks < 50_000 || counts.Chunks != counts.Embeddings {
		return FixtureCounts{}, fmt.Errorf("fixture counts do not match manifest: %+v", counts)
	}
	minimumEdges := manifest.Owners.Code*manifest.Graph.EdgesPerCodeOwner - manifest.Graph.EdgesPerCodeOwner*(manifest.Graph.EdgesPerCodeOwner+1)/2 + manifest.Owners.Prose*manifest.Graph.EdgesPerNoteOwner
	if counts.GraphEdges < minimumEdges {
		return FixtureCounts{}, fmt.Errorf("fixture graph has %d edges, want at least %d", counts.GraphEdges, minimumEdges)
	}
	return counts, nil
}

func codeOwner(i int) (path, id, symbol, text string) {
	domain := i % 20
	path = fmt.Sprintf("src/domain-%02d/owner_%04d.go", domain, i)
	id = fmt.Sprintf("anchor-%06d", i)
	symbol = fmt.Sprintf("Owner%04d", i)
	text = fmt.Sprintf("durable workload topic %04d domain %02d implementation", i, domain)
	if i%500 == 42 {
		text += " shared mixed workload cohort 0042"
	}
	return
}

func proseOwner(i int) (path, id, title, text string) {
	domain := i % 20
	path = fmt.Sprintf("notes/domain-%02d/guide_%04d.md", domain, i)
	id = fmt.Sprintf("section-%06d", i)
	title = fmt.Sprintf("Guide%04d", i)
	text = fmt.Sprintf("durable workload guide topic %04d domain %02d explanation", i, domain)
	if i%500 == 42 {
		text += " shared mixed workload cohort 0042"
	}
	return
}

func ownerIdentity(owner, codeOwners int) (id, ownerType, heading, text string) {
	if owner < codeOwners {
		_, id, heading, text = codeOwner(owner)
		return id, "anchor", heading, text
	}
	_, id, heading, text = proseOwner(owner - codeOwners)
	return id, "doc_section", heading, text
}

func writeFixtureFile(root, rel, content string) error {
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
