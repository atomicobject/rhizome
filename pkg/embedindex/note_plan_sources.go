package embedindex

import (
	"context"

	"fmt"
	"path/filepath"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type notePlanSources struct {
	byPath        map[string][]codeanchor.IntelDocSection
	metaByPath    map[string][]codeanchor.IntelDocSectionMeta
	latestUpdated int64
	changedPaths  map[string]bool
	usingMeta     bool
	paths         []string
	typedPaths    []string
}

func (s *NoteSyncer) loadNotePlanSources(ctx context.Context, sourceHighWaterUnix int64) (notePlanSources, error) {
	var err error
	var sections []codeanchor.IntelDocSection
	var metaSections []codeanchor.IntelDocSectionMeta
	var byPath map[string][]codeanchor.IntelDocSection
	var metaByPath map[string][]codeanchor.IntelDocSectionMeta
	var latestUpdated int64
	changedPaths := make(map[string]bool) // Tracks notes with updated_at > lastSyncUnix
	usingMeta := false
	if metaSource, ok := s.Intel.(NoteIntelMetaSource); ok {
		s.progressf("Loading intel doc section metadata")
		metaSections, err = observeNoteMetric(ctx, indexingperf.NoteplanLoadDocSectionMeta, func() ([]codeanchor.IntelDocSectionMeta, error) {
			return metaSource.IntelDocSectionMetas(ctx)
		})
		if err != nil {
			return notePlanSources{}, fmt.Errorf("read intel doc sections: %w", err)
		}
		metaByPath = make(map[string][]codeanchor.IntelDocSectionMeta)
		for _, sec := range metaSections {
			path := filepath.ToSlash(sec.Path)
			if strings.TrimSpace(path) == "" {
				continue
			}
			if sec.UpdatedAt > latestUpdated {
				latestUpdated = sec.UpdatedAt
			}
			if sec.UpdatedAt > sourceHighWaterUnix {
				changedPaths[path] = true
			}
			metaByPath[path] = append(metaByPath[path], sec)
		}
		usingMeta = true
	} else {
		s.progressf("Loading intel doc sections")
		sections, err = observeNoteMetric(ctx, indexingperf.NoteplanLoadDocSections, func() ([]codeanchor.IntelDocSection, error) {
			return s.Intel.IntelDocSections(ctx)
		})
		if err != nil {
			return notePlanSources{}, fmt.Errorf("read intel doc sections: %w", err)
		}
		byPath = make(map[string][]codeanchor.IntelDocSection)
		for _, sec := range sections {
			path := filepath.ToSlash(sec.Path)
			if strings.TrimSpace(path) == "" {
				continue
			}
			if sec.UpdatedAt > latestUpdated {
				latestUpdated = sec.UpdatedAt
			}
			if sec.UpdatedAt > sourceHighWaterUnix {
				changedPaths[path] = true
			}
			byPath[path] = append(byPath[path], sec)
		}
	}

	var paths []string
	if usingMeta {
		paths = make([]string, 0, len(metaByPath))
		for p := range metaByPath {
			paths = append(paths, p)
		}
	} else {
		paths = make([]string, 0, len(byPath))
		for p := range byPath {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	rawPaths, typedPaths, err := s.rawNoteEmbeddingPaths(ctx, paths)
	if err != nil {
		return notePlanSources{}, err
	}
	paths = rawPaths
	indexingperf.AddCount(ctx, indexingperf.NoteplanRawEligiblePaths, int64(len(rawPaths)))
	indexingperf.AddCount(ctx, indexingperf.NoteplanRawSkippedPaths, int64(len(typedPaths)))

	return notePlanSources{byPath: byPath, metaByPath: metaByPath, latestUpdated: latestUpdated, changedPaths: changedPaths, usingMeta: usingMeta, paths: paths, typedPaths: typedPaths}, nil
}
