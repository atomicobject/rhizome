package web

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

const defaultNodeProjectionCacheEntries = 128

type nodeProjectionCache struct {
	mu         sync.Mutex
	max        int
	entries    map[string]*cachedNodeProjectionNote
	generation uint64
}

type cachedNodeProjectionNote struct {
	schemaHash  string
	modTime     time.Time
	snapshot    *ontology.DocumentSnapshot
	projections map[string]*ontology.NodeProjection
	warmed      bool
	warming     bool
	accessedAt  time.Time
}

func newNodeProjectionCache(max int) *nodeProjectionCache {
	if max <= 0 {
		max = defaultNodeProjectionCacheEntries
	}
	return &nodeProjectionCache{max: max, entries: map[string]*cachedNodeProjectionNote{}}
}

func (c *nodeProjectionCache) Projection(ctx context.Context, vaultDef obsidian.VaultDefinition, noteReader obsidian.NoteReader, schema *ontology.Schema, ref ontology.NodeRef) (*ontology.NodeProjection, error) {
	if c == nil {
		return ontology.ProjectNode(ctx, vaultDef, noteReader, schema, ref)
	}
	ref = noderead.NormalizeRef(ref)
	notePath := strings.TrimSpace(ref.NotePath)
	if notePath == "" {
		return nil, fmt.Errorf("node ref note path is required")
	}
	if noteReader == nil {
		return nil, fmt.Errorf("note reader is required")
	}
	schemaHash := ontologySchemaHash(schema)
	modTime, _ := noteReader.GetModTime(vaultDef, notePath)
	projectionKey := nodeProjectionRefKey(ref)

	c.mu.Lock()
	generation := c.generation
	entry := c.entries[notePath]
	if entry != nil && entry.matches(schemaHash, modTime) {
		entry.accessedAt = time.Now()
		if projection, ok := entry.projections[projectionKey]; ok {
			c.mu.Unlock()
			return projection, nil
		}
		if entry.snapshot != nil {
			snapshot := entry.snapshot
			c.mu.Unlock()
			projection, err := ontology.ProjectNodeFromSnapshot(snapshot, schema, ref)
			if err != nil {
				return nil, err
			}
			c.storeProjection(notePath, schemaHash, modTime, snapshot, projection, generation)
			return projection, nil
		}
	}
	c.mu.Unlock()

	snapshot, err := ontology.LoadDocumentSnapshot(ctx, vaultDef, noteReader, notePath)
	if err != nil {
		return nil, err
	}
	if modTime.IsZero() {
		modTime = snapshot.ModTime
	}
	projection, err := ontology.ProjectNodeFromSnapshot(snapshot, schema, ref)
	if err != nil {
		return nil, err
	}
	c.storeProjection(notePath, schemaHash, modTime, snapshot, projection, generation)
	return projection, nil
}

func (c *nodeProjectionCache) StartWarmNote(ctx context.Context, vaultDef obsidian.VaultDefinition, noteReader obsidian.NoteReader, schema *ontology.Schema, notePath string) {
	if c == nil {
		return
	}
	notePath = strings.TrimSpace(notePath)
	if notePath == "" || schema == nil || noteReader == nil {
		return
	}
	schemaHash := ontologySchemaHash(schema)
	modTime, _ := noteReader.GetModTime(vaultDef, notePath)

	c.mu.Lock()
	entry := c.entries[notePath]
	if entry != nil && entry.matches(schemaHash, modTime) && (entry.warmed || entry.warming) {
		c.mu.Unlock()
		return
	}
	if entry == nil || !entry.matches(schemaHash, modTime) {
		entry = &cachedNodeProjectionNote{
			schemaHash:  schemaHash,
			modTime:     modTime,
			projections: map[string]*ontology.NodeProjection{},
			accessedAt:  time.Now(),
		}
		c.entries[notePath] = entry
		c.pruneLocked()
	}
	entry.warming = true
	snapshot := entry.snapshot
	c.mu.Unlock()

	go func() {
		if snapshot == nil {
			loaded, err := ontology.LoadDocumentSnapshot(ctx, vaultDef, noteReader, notePath)
			if err != nil {
				c.finishWarm(notePath, entry, schemaHash, modTime, nil, nil, false)
				return
			}
			snapshot = loaded
			if modTime.IsZero() {
				modTime = loaded.ModTime
			}
		}
		refs := warmProjectionRefs(snapshot)
		projections, err := ontology.ProjectNodesFromSnapshot(snapshot, schema, refs)
		if err != nil {
			c.finishWarm(notePath, entry, schemaHash, modTime, snapshot, nil, false)
			return
		}
		c.finishWarm(notePath, entry, schemaHash, modTime, snapshot, projections, true)
	}()
}

func (c *nodeProjectionCache) InvalidatePaths(paths []string) {
	if c == nil || len(paths) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	for _, path := range paths {
		delete(c.entries, strings.TrimSpace(path))
	}
}

func (c *nodeProjectionCache) InvalidateAll() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	c.entries = map[string]*cachedNodeProjectionNote{}
}

func (s *Server) invalidateNodeProjectionCacheForPaths(paths []string) {
	if s == nil || s.nodeProjectionCache == nil {
		return
	}
	s.nodeProjectionCache.InvalidatePaths(paths)
}

func (s *Server) invalidateNodeProjectionCache() {
	if s == nil || s.nodeProjectionCache == nil {
		return
	}
	s.nodeProjectionCache.InvalidateAll()
}

func (c *nodeProjectionCache) storeProjection(notePath, schemaHash string, modTime time.Time, snapshot *ontology.DocumentSnapshot, projection *ontology.NodeProjection, generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Invalidation must also discard loads that were already in flight.
	if c.generation != generation {
		return
	}
	entry := c.entries[notePath]
	if entry != nil && !entry.matches(schemaHash, modTime) {
		if entry.schemaHash == schemaHash && entry.modTime.After(modTime) {
			return
		}
		entry = nil
	}
	if entry == nil {
		entry = &cachedNodeProjectionNote{
			schemaHash:  schemaHash,
			modTime:     modTime,
			projections: map[string]*ontology.NodeProjection{},
			accessedAt:  time.Now(),
		}
		c.entries[notePath] = entry
		c.pruneLocked()
	}
	entry.snapshot = snapshot
	entry.accessedAt = time.Now()
	if projection != nil {
		entry.projections[nodeProjectionRefKey(projection.Ref)] = projection
	}
}

func (c *nodeProjectionCache) finishWarm(notePath string, expected *cachedNodeProjectionNote, schemaHash string, modTime time.Time, snapshot *ontology.DocumentSnapshot, projections []*ontology.NodeProjection, warmed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entries[notePath]
	if entry != expected || entry == nil || !entry.matches(schemaHash, modTime) {
		return
	}
	entry.warming = false
	entry.warmed = warmed
	entry.snapshot = snapshot
	entry.accessedAt = time.Now()
	if entry.projections == nil {
		entry.projections = map[string]*ontology.NodeProjection{}
	}
	for _, projection := range projections {
		if projection == nil {
			continue
		}
		entry.projections[nodeProjectionRefKey(projection.Ref)] = projection
	}
}

func (c *nodeProjectionCache) pruneLocked() {
	if c.max <= 0 || len(c.entries) <= c.max {
		return
	}
	var oldestPath string
	var oldest time.Time
	for path, entry := range c.entries {
		if oldestPath == "" || entry.accessedAt.Before(oldest) {
			oldestPath = path
			oldest = entry.accessedAt
		}
	}
	delete(c.entries, oldestPath)
}

func (e *cachedNodeProjectionNote) matches(schemaHash string, modTime time.Time) bool {
	if e == nil || e.schemaHash != schemaHash {
		return false
	}
	return e.modTime.Equal(modTime)
}

func warmProjectionRefs(snapshot *ontology.DocumentSnapshot) []ontology.NodeRef {
	if snapshot == nil {
		return nil
	}
	refs := []ontology.NodeRef{{NotePath: snapshot.NotePath, Kind: ontology.NodeKindNote}}
	for _, node := range snapshot.SectionsByID {
		if node == nil {
			continue
		}
		refs = append(refs, ontology.NodeRef{NotePath: snapshot.NotePath, NodeID: node.ID})
	}
	return refs
}

func nodeProjectionRefKey(ref ontology.NodeRef) string {
	ref = noderead.NormalizeRef(ref)
	kind := ref.Kind
	if kind == "" && strings.TrimSpace(ref.Fragment) == "" && strings.TrimSpace(ref.NodeID) == "" && strings.TrimSpace(ref.Structural) == "" {
		kind = ontology.NodeKindNote
	}
	return strings.Join([]string{
		strings.TrimSpace(ref.NotePath),
		strings.TrimSpace(ref.Fragment),
		strings.TrimSpace(ref.NodeID),
		string(kind),
		strings.TrimSpace(ref.Structural),
	}, "|")
}

func ontologySchemaHash(schema *ontology.Schema) string {
	if schema == nil {
		return ""
	}
	return strings.TrimSpace(schema.Hash)
}
