package mcp

import (
	"context"
	"os"
	"sync"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

type semanticQueryReadCacheKey struct{}

type semanticQueryReadCache struct {
	mu           sync.Mutex
	entries      map[string]*semanticQueryReadCacheEntry
	chunkEntries map[string]*semanticQueryChunkCacheEntry
}

type semanticQueryReadCacheEntry struct {
	ready chan struct{}
	data  []byte
	err   error
}

type semanticQueryChunkCacheEntry struct {
	ready   chan struct{}
	content []byte
}

func withSemanticQueryReadCache(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if semanticQueryReadCacheFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, semanticQueryReadCacheKey{}, &semanticQueryReadCache{
		entries:      map[string]*semanticQueryReadCacheEntry{},
		chunkEntries: map[string]*semanticQueryChunkCacheEntry{},
	})
}

func semanticQueryReadCacheFromContext(ctx context.Context) *semanticQueryReadCache {
	if ctx == nil {
		return nil
	}
	cache, _ := ctx.Value(semanticQueryReadCacheKey{}).(*semanticQueryReadCache)
	return cache
}

const (
	semanticReadNote = "note"
	semanticReadCode = "code"
)

func readSemanticQueryFile(ctx context.Context, kind, path string) ([]byte, error) {
	if cache := semanticQueryReadCacheFromContext(ctx); cache != nil {
		return cache.read(ctx, kind, path)
	}
	recordSemanticQueryRead(ctx, kind)
	return os.ReadFile(path)
}

func (c *semanticQueryReadCache) read(ctx context.Context, kind, path string) ([]byte, error) {
	key := kind + "\x00" + path
	c.mu.Lock()
	if entry := c.entries[key]; entry != nil {
		c.mu.Unlock()
		select {
		case <-entry.ready:
			return entry.data, entry.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	entry := &semanticQueryReadCacheEntry{ready: make(chan struct{})}
	c.entries[key] = entry
	c.mu.Unlock()

	recordSemanticQueryRead(ctx, kind)
	entry.data, entry.err = os.ReadFile(path)
	close(entry.ready)
	return entry.data, entry.err
}

func semanticQueryChunkText(ctx context.Context, kind, vaultPath, relPath string, idx int, cache map[string][]embeddings.ChunkInput) string {
	if cache == nil {
		if requestCache := semanticQueryReadCacheFromContext(ctx); requestCache != nil {
			return requestCache.chunkText(ctx, kind, vaultPath, relPath, idx)
		}
	}
	if relPath != "" {
		cached := false
		for _, chunk := range cache[relPath] {
			if chunk.Index == idx {
				cached = true
				break
			}
		}
		if !cached {
			recordSemanticQueryRead(ctx, kind)
		}
	}
	return embeddings.ChunkTextForPath(vaultPath, relPath, idx, cache)
}

func (c *semanticQueryReadCache) chunkText(ctx context.Context, kind, vaultPath, relPath string, idx int) string {
	if relPath == "" {
		return ""
	}
	key := kind + "\x00" + vaultPath + "\x00" + relPath
	c.mu.Lock()
	if entry := c.chunkEntries[key]; entry != nil {
		c.mu.Unlock()
		select {
		case <-entry.ready:
			return embeddings.ChunkTextForContent(relPath, idx, entry.content)
		case <-ctx.Done():
			return ""
		}
	}
	entry := &semanticQueryChunkCacheEntry{ready: make(chan struct{})}
	c.chunkEntries[key] = entry
	c.mu.Unlock()

	full, ok := safeJoinVaultPath(vaultPath, relPath)
	if ok {
		if content, err := c.read(ctx, kind, full); err == nil {
			entry.content = content
		}
	}
	close(entry.ready)
	return embeddings.ChunkTextForContent(relPath, idx, entry.content)
}

func recordSemanticQueryRead(ctx context.Context, kind string) {
	switch kind {
	case semanticReadNote:
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpNoteReads, 1)
	case semanticReadCode:
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpCodeReads, 1)
	}
}
