package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// NewPersistedNoteFacts preserves projected metadata and context bodies from
// one durable fact snapshot. It does not parse note syntax or read source files.
func NewPersistedNoteFacts(facts semdb.DurableNoteFacts) (NoteFacts, error) {
	set := NoteFacts{byPath: make(map[string]NoteFact, len(facts.MetadataRows))}
	hasPayload := map[string]bool{}
	for path, row := range facts.MetadataRows {
		entry := NoteFact{Format: noteformat.FormatID(row.FormatID)}
		if raw, ok := facts.ProjectionPayloads[path]; ok {
			var payload struct {
				Frontmatter map[string]any
				InlineProps map[string][]string
				Tags        []string
			}
			decoder := json.NewDecoder(strings.NewReader(raw))
			decoder.UseNumber()
			if err := decoder.Decode(&payload); err != nil {
				return NoteFacts{}, fmt.Errorf("decode projected note metadata %q: %w", path, err)
			}
			entry.Frontmatter = payload.Frontmatter
			for key, value := range entry.Frontmatter {
				entry.Frontmatter[key] = restoreMetadataNumbers(value)
			}
			entry.InlineProps = payload.InlineProps
			entry.Tags = payload.Tags
			hasPayload[path] = true
		}
		set.byPath[path] = entry
	}
	for _, tag := range facts.Tags {
		entry, ok := set.byPath[tag.NotePath]
		if !ok || hasPayload[tag.NotePath] {
			continue
		}
		entry.Tags = append(entry.Tags, tag.TagNorm)
		set.byPath[tag.NotePath] = entry
	}
	for _, row := range facts.SearchRegions {
		entry, ok := set.byPath[row.NotePath]
		if !ok {
			continue
		}
		region := noteformat.SearchRegionFact{Origin: noteformat.SearchRegionOrigin(row.Origin), Kind: noteformat.SearchRegionKind(row.Kind), Text: row.Text, MediaType: row.MediaType}
		if row.RangePresent {
			region.Range = noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: row.StartByte, EndByte: row.EndByte}}
		}
		entry.SearchRegions = append(entry.SearchRegions, region)
		if row.Origin == string(noteformat.SearchRegionAuthored) && row.StartByte == 0 && row.EndByte == len(row.Text) && row.RangePresent {
			entry.Content = row.Text
		}
		set.byPath[row.NotePath] = entry
	}
	return set, nil
}

func restoreMetadataNumbers(value any) any {
	switch current := value.(type) {
	case json.Number:
		if n, err := strconv.ParseInt(string(current), 10, 64); err == nil {
			return int(n)
		}
		if n, err := strconv.ParseFloat(string(current), 64); err == nil {
			return n
		}
	case []any:
		for i, v := range current {
			current[i] = restoreMetadataNumbers(v)
		}
	case map[string]any:
		for key, v := range current {
			current[key] = restoreMetadataNumbers(v)
		}
	}
	return value
}

type persistedNoteFacts struct {
	ctx         context.Context
	indexer     notemeta.Indexer
	store       *semdb.Store
	mu          sync.Mutex
	cache       map[string]NoteFact
	attempted   map[string]bool
	inventory   []string
	inventoried bool
}

// LazyPersistedNoteFacts decodes only metadata requested by this caller. Its
// request-local cache shares successful reads and failures across renderers.
func LazyPersistedNoteFacts(ctx context.Context, indexer notemeta.Indexer, store *semdb.Store) NoteFacts {
	return NoteFacts{persisted: &persistedNoteFacts{ctx: ctx, indexer: indexer, store: store, cache: map[string]NoteFact{}, attempted: map[string]bool{}}}
}

func (p *persistedNoteFacts) lookup(raw string) (NoteFact, bool) {
	path := paths.NormalizeNotePath(raw).String()
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.attempted[path] {
		p.attempted[path] = true
		facts, err := p.indexer.UsableNoteFactsByPaths(p.ctx, p.store, []string{path})
		if err == nil {
			loaded, err := NewPersistedNoteFacts(facts)
			if err == nil {
				if entry, ok := loaded.byPath[path]; ok {
					p.cache[path] = entry
				}
			}
		}
	}
	return (NoteFacts{byPath: p.cache}).LookupFact(path)
}

func (p *persistedNoteFacts) paths() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.inventoried {
		p.inventoried = true
		inventory, err := p.store.CurrentNoteMetadataPaths(p.ctx)
		if err == nil {
			rows, err := p.indexer.UsableNoteMetadataRowsByPaths(p.ctx, p.store, inventory)
			if err == nil {
				for _, path := range inventory {
					if _, ok := rows[path]; ok {
						p.inventory = append(p.inventory, path)
					}
				}
			}
		}
	}
	return append([]string(nil), p.inventory...)
}
