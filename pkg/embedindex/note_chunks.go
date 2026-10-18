package embedindex

import (
	"context"

	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func sameNoteSet(existing []embeddings.NoteFileInfo, paths []string) bool {
	if len(existing) != len(paths) {
		return false
	}
	have := make(map[string]struct{}, len(existing))
	for _, n := range existing {
		if string(n.ID) == "" {
			return false
		}
		have[string(n.ID)] = struct{}{}
	}
	for _, p := range paths {
		p = filepath.ToSlash(strings.TrimSpace(p))
		if p == "" {
			return false
		}
		if _, ok := have[p]; !ok {
			return false
		}
	}
	return true
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = filepath.ToSlash(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}

func buildNoteFileInfo(id embeddings.NoteID, path string, sections []codeanchor.IntelDocSection, titles map[string]string) embeddings.NoteFileInfo {
	maxUpdated := int64(0)
	size := int64(0)
	for _, sec := range sections {
		if sec.UpdatedAt > maxUpdated {
			maxUpdated = sec.UpdatedAt
		}
		size += int64(len(sec.Content))
	}
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if t := titles[path]; t != "" {
		title = t
	}
	return embeddings.NoteFileInfo{
		ID:    id,
		Path:  path,
		Title: title,
		Size:  size,
		Mtime: time.Unix(maxUpdated, 0),
	}
}

func buildNoteFileInfoFromMeta(id embeddings.NoteID, path string, sections []codeanchor.IntelDocSectionMeta, titles map[string]string) embeddings.NoteFileInfo {
	maxUpdated := int64(0)
	size := int64(0)
	for _, sec := range sections {
		if sec.UpdatedAt > maxUpdated {
			maxUpdated = sec.UpdatedAt
		}
		if sec.EndByte > size {
			size = sec.EndByte
		}
	}
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if t := titles[path]; t != "" {
		title = t
	}
	return embeddings.NoteFileInfo{
		ID:    id,
		Path:  path,
		Title: title,
		Size:  size,
		Mtime: time.Unix(maxUpdated, 0),
	}
}

func buildSectionChunks(noteTitle string, sections []codeanchor.IntelDocSection, maxBytes int) []embeddings.ChunkInput {
	// Build hierarchical breadcrumbs based on heading levels.
	type levelTitle struct {
		level int64
		title string
	}
	var stack []levelTitle

	out := make([]embeddings.ChunkInput, 0, len(sections))
	for idx, sec := range sections {
		title := strings.TrimSpace(sec.Title)
		level := sec.Level
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			stack = stack[:len(stack)-1]
		}
		if title != "" {
			stack = append(stack, levelTitle{level: level, title: title})
		}

		breadcrumbParts := make([]string, 0, 1+len(stack))
		if strings.TrimSpace(noteTitle) != "" {
			breadcrumbParts = append(breadcrumbParts, strings.TrimSpace(noteTitle))
		}
		for _, lt := range stack {
			if lt.title != "" {
				breadcrumbParts = append(breadcrumbParts, lt.title)
			}
		}
		breadcrumb := strings.Join(breadcrumbParts, " > ")

		text := buildChunkText(noteTitle, title, sec.Content, maxBytes)
		out = append(out, embeddings.NewChunkInput(idx, text, breadcrumb, title))
	}
	return out
}

func buildChunkText(noteTitle, heading, body string, maxBytes int) string {
	var b strings.Builder
	if strings.TrimSpace(noteTitle) != "" {
		b.WriteString("# ")
		b.WriteString(strings.TrimSpace(noteTitle))
		b.WriteString("\n\n")
	}
	if strings.TrimSpace(heading) != "" {
		b.WriteString("## ")
		b.WriteString(strings.TrimSpace(heading))
		b.WriteString("\n\n")
	}
	body = strings.TrimSpace(body)
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	text := b.String()
	if maxBytes <= 0 {
		maxBytes = defaultSectionMaxBytes
	}
	if len(text) > maxBytes {
		text = text[:maxBytes]
	}
	return strings.TrimSpace(text)
}

func hasChunkStragglers(prev map[int]string, desired []int) bool {
	if len(prev) == 0 {
		return false
	}
	if len(prev) != len(desired) {
		// Quick signal: any mismatch in count means we likely have stale or missing chunk rows.
		return true
	}
	desiredSet := make(map[int]struct{}, len(desired))
	for _, idx := range desired {
		desiredSet[idx] = struct{}{}
	}
	for idx := range prev {
		if _, ok := desiredSet[idx]; !ok {
			return true
		}
	}
	return false
}

func (s *NoteSyncer) planChunkUpdates(ctx context.Context, noteID embeddings.NoteID, desired []embeddings.ChunkInput, prevHashes map[int]string) ([]embeddings.ChunkInput, []embeddings.Embedding, []embeddings.ChunkInput, []string, error) {
	indexingperf.AddCount(ctx, indexingperf.NoteplanChunksDesired, int64(len(desired)))
	started := time.Now()
	defer func() {
		indexingperf.ObserveLatency(ctx, indexingperf.NoteplanPlanChunkUpdates, time.Since(started))
	}()
	// Fast-path: if all desired indices match hashes and we don't need to update breadcrumbs/headings,
	// we can skip loading stored embeddings (expensive) and return no work.
	allMatch := true
	for _, ch := range desired {
		if prevHashes[ch.Index] != ch.Hash {
			allMatch = false
			break
		}
	}
	if allMatch {
		indexingperf.AddCount(ctx, indexingperf.NoteplanReuseSamePosition, int64(len(desired)))
		// Without loading stored chunks, we conservatively assume metadata is stable.
		return nil, nil, nil, nil, nil
	}

	storedChunks, err := observeNoteMetric(ctx, indexingperf.NoteplanLoadStoredChunks, func() ([]embeddings.StoredChunk, error) {
		return s.Index.NoteChunks(ctx, noteID)
	})
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load stored chunks %s: %w", noteID, err)
	}
	byIndex := make(map[int]embeddings.StoredChunk, len(storedChunks))
	for _, ch := range storedChunks {
		byIndex[ch.Index] = ch
	}

	// Build a best-effort hash -> embedding map so we can reuse embeddings when chunk indices shift.
	embByHash := make(map[string]embeddings.Embedding, len(prevHashes))
	for idx, hash := range prevHashes {
		if hash == "" {
			continue
		}
		if _, ok := embByHash[hash]; ok {
			continue
		}
		if stored, ok := byIndex[idx]; ok && len(stored.Embedding) > 0 {
			embByHash[hash] = stored.Embedding
		}
	}
	for _, ch := range desired {
		if _, ok := embByHash[ch.Hash]; ok {
			continue
		}
		cacheLookupStarted := time.Now()
		cached, ok, err := s.Index.EmbeddingByHash(ctx, ch.Hash)
		indexingperf.ObserveLatency(ctx, indexingperf.NoteplanCacheLookup, time.Since(cacheLookupStarted))
		if err == nil && ok && len(cached) > 0 {
			indexingperf.AddCount(ctx, indexingperf.NoteplanHashCacheHit, 1)
			embByHash[ch.Hash] = cached
		} else if err != nil {
			indexingperf.AddCount(ctx, indexingperf.NoteplanHashCacheError, 1)
			return nil, nil, nil, nil, err
		} else {
			indexingperf.AddCount(ctx, indexingperf.NoteplanHashCacheMiss, 1)
		}
	}

	var reuseChunks []embeddings.ChunkInput
	var reuseVecs []embeddings.Embedding
	var embedChunks []embeddings.ChunkInput
	var embedTexts []string

	for _, want := range desired {
		prevHash := prevHashes[want.Index]
		if prevHash == want.Hash {
			// Hash matches at this index. If the stored metadata differs, rewrite without re-embedding.
			if stored, ok := byIndex[want.Index]; ok && len(stored.Embedding) > 0 {
				indexingperf.AddCount(ctx, indexingperf.NoteplanReuseSamePosition, 1)
				if stored.Breadcrumb != want.Breadcrumb || stored.Heading != want.Heading {
					indexingperf.AddCount(ctx, indexingperf.NoteplanReuseMetadataRewrite, 1)
					reuseChunks = append(reuseChunks, stripChunkText(want))
					reuseVecs = append(reuseVecs, stored.Embedding)
				}
				continue
			}
			// Hash row exists but we couldn't load an embedding (dims mismatch or missing row) – re-embed.
			indexingperf.AddCount(ctx, indexingperf.NoteplanEmbedReasonMissingVector, 1)
			embedChunks = append(embedChunks, want)
			embedTexts = append(embedTexts, want.Text)
			continue
		}

		// Index/hash mismatch: try to reuse an existing embedding by hash (supports reordering/inserts).
		if vec := embByHash[want.Hash]; len(vec) > 0 {
			indexingperf.AddCount(ctx, indexingperf.NoteplanReuseContentHash, 1)
			reuseChunks = append(reuseChunks, stripChunkText(want))
			reuseVecs = append(reuseVecs, vec)
			continue
		}

		indexingperf.AddCount(ctx, indexingperf.NoteplanEmbedReasonNewHash, 1)
		embedChunks = append(embedChunks, want)
		embedTexts = append(embedTexts, want.Text)
	}

	indexingperf.AddCount(ctx, indexingperf.NoteplanChunksRewrite, int64(len(reuseChunks)))
	indexingperf.AddCount(ctx, indexingperf.NoteplanChunksEmbed, int64(len(embedChunks)))
	return reuseChunks, reuseVecs, embedChunks, embedTexts, nil
}

func stripChunkText(in embeddings.ChunkInput) embeddings.ChunkInput {
	in.Text = ""
	return in
}

func buildIntelChunksForSections(noteTitle string, sections []codeanchor.IntelDocSection, now int64) (ownerIDs []string, chunks []codeanchor.IntelChunk) {
	if len(sections) == 0 {
		return nil, nil
	}
	// Keep ordering stable and consistent with embedding chunk indices.
	sort.Slice(sections, func(i, j int) bool {
		if sections[i].StartByte == sections[j].StartByte {
			return sections[i].SectionID < sections[j].SectionID
		}
		return sections[i].StartByte < sections[j].StartByte
	})
	desired := buildSectionChunks(noteTitle, sections, 0)
	ownerIDs = make([]string, 0, len(sections))
	chunks = make([]codeanchor.IntelChunk, 0, len(sections))
	for i, sec := range sections {
		secID := strings.TrimSpace(sec.SectionID)
		if secID == "" {
			continue
		}
		ownerIDs = append(ownerIDs, secID)
		var breadcrumb, heading, hash string
		if i >= 0 && i < len(desired) {
			breadcrumb = desired[i].Breadcrumb
			heading = desired[i].Heading
			hash = desired[i].Hash
		}
		if hash == "" {
			hash = strings.TrimSpace(sec.Fingerprint)
		}
		chunks = append(chunks, codeanchor.IntelChunk{
			ChunkID:     codeanchor.IntelChunkID(secID, 0, "section"),
			OwnerID:     secID,
			OwnerType:   "doc_section",
			ChunkFamily: codeanchor.IntelChunkFamilyAuthoredSection,
			Ord:         0,
			Granularity: "section",
			Breadcrumb:  breadcrumb,
			Heading:     heading,
			ContentHash: hash,
			StartByte:   sec.StartByte,
			EndByte:     sec.EndByte,
			UpdatedAt:   now,
		})
	}
	return ownerIDs, chunks
}

// persistNoteIntelChunks records one deterministic chunk per doc section for notes being synced.
func (s *NoteSyncer) persistNoteIntelChunks(ctx context.Context, tasks []noteTask) error {
	if s.ChunkWriter == nil {
		return nil
	}

	now := time.Now().Unix()
	ownerSet := make(map[string]struct{}, 256)
	var ownerIDs []string
	var chunks []codeanchor.IntelChunk
	for _, task := range tasks {
		ids, cs := buildIntelChunksForSections(task.payload.info.Title, task.payload.sections, now)
		for _, id := range ids {
			if _, ok := ownerSet[id]; ok {
				continue
			}
			ownerSet[id] = struct{}{}
			ownerIDs = append(ownerIDs, id)
		}
		chunks = append(chunks, cs...)
	}
	if s.WriteQueue != nil {
		return s.WriteQueue.SubmitIntelChunksByFamily(ctx, ownerIDs, codeanchor.IntelChunkFamilyAuthoredSection, chunks)
	}
	if familyWriter, ok := s.ChunkWriter.(ChunkFamilyWriter); ok {
		return familyWriter.ReplaceIntelChunksByFamily(ctx, ownerIDs, codeanchor.IntelChunkFamilyAuthoredSection, chunks)
	}
	return s.ChunkWriter.ReplaceIntelChunks(ctx, ownerIDs, chunks)
}
