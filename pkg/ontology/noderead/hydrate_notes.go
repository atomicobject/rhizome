package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"sort"
	"strings"
)

func (s *Scope) recordsForPaths(ctx context.Context, pathsList []string, profile HydrateProfile) ([]NodeRecord, error) {
	pathsList = normalizeStrings(pathsList)
	if len(pathsList) == 0 {
		return []NodeRecord{}, nil
	}
	missing := make([]string, 0, len(pathsList))
	for _, path := range pathsList {
		if s.recordKnown[path] {
			s.diagnostics.RecordHits++
			continue
		}
		missing = append(missing, path)
	}
	if len(missing) > 0 {
		var records []NodeRecord
		var err error
		if s.service.Store != nil {
			records, err = s.recordsFromMetadata(ctx, missing)
		}
		if canceled := readCancellation(ctx, err); canceled != nil {
			return nil, canceled
		}
		if err != nil {
			return nil, err
		}

		if overlayRecords, err := s.overlayNoteRecordsLocked(ctx, missing); err != nil {
			return nil, err
		} else if s.hasOverlay() {
			byPath := make(map[string]NodeRecord, len(records)+len(overlayRecords))
			for _, record := range records {
				byPath[record.Path] = record
			}
			for path := range s.overlayTouchedPathSetLocked(ctx) {
				delete(byPath, path)
			}
			for _, record := range overlayRecords {
				byPath[record.Path] = record
			}
			records = records[:0]
			for _, path := range missing {
				if record, ok := byPath[path]; ok {
					records = append(records, record)
				}
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, record := range records {
			s.recordByPath[record.Path] = record
		}
		for _, path := range missing {
			s.recordKnown[path] = true
		}
		s.diagnostics.RecordLoads++
	}
	if profileNeedsContent(profile) {
		if err := s.ensureContentForPaths(ctx, pathsList); err != nil {
			return nil, err
		}
	}
	out := make([]NodeRecord, 0, len(pathsList))
	for _, path := range pathsList {
		if record, ok := s.recordByPath[path]; ok {
			out = append(out, cloneNodeRecord(record))
		}
	}
	return out, nil
}

func (s *Scope) ensureContentForPaths(ctx context.Context, pathsList []string) error {
	if s == nil || s.service == nil || (s.service.NoteReader == nil && !s.hasOverlay()) {
		return nil
	}
	missing := make([]string, 0, len(pathsList))
	for _, path := range pathsList {
		if s.contentKnown[path] {
			s.diagnostics.ContentHits++
			continue
		}
		missing = append(missing, path)
	}
	if len(missing) == 0 {
		return nil
	}
	for _, path := range missing {
		if err := ctx.Err(); err != nil {
			return err
		}
		content, ok := s.overlayContent(path)
		if !ok && s.overlayHasPath(path) {
			s.contentKnown[path] = true
			continue
		}
		if _, exists := s.recordByPath[path]; !exists {
			s.contentKnown[path] = true
			continue
		}
		if !ok {
			if s.service.NoteReader == nil {
				continue
			}
			var err error
			content, err = s.service.NoteReader.GetContents(s.service.VaultDef, path)
			if canceled := readCancellation(ctx, err); canceled != nil {
				return canceled
			}
			if err != nil {
				return err
			}
		}
		record := s.recordByPath[path]
		record.Content = content
		s.recordByPath[path] = record
		s.contentKnown[path] = true
	}
	s.diagnostics.ContentLoads++
	return nil
}

func profileNeedsContent(profile HydrateProfile) bool {
	return profile == HydrateContent
}

func (s *Scope) recordsFromMetadata(ctx context.Context, pathsList []string) ([]NodeRecord, error) {
	if err := s.loadPathMetadataLocked(ctx, pathsList); err != nil {
		return nil, err
	}
	rowsByPath := make(map[string]semdb.NoteMetadataRow, len(pathsList))
	for _, path := range pathsList {
		if row, ok := s.noteRowByPath[path]; ok {
			rowsByPath[path] = row
		}
	}
	if len(rowsByPath) == 0 {
		return nil, nil
	}
	knownPaths := make([]string, 0, len(rowsByPath))
	seenPaths := make(map[string]struct{}, len(rowsByPath))
	for _, path := range pathsList {
		if _, ok := rowsByPath[path]; !ok {
			continue
		}
		if _, duplicate := seenPaths[path]; duplicate {
			continue
		}
		seenPaths[path] = struct{}{}
		knownPaths = append(knownPaths, path)
	}
	loadProperties := s.service.Store.CurrentNotePropertyValues
	if s.service.ExactNotePropertyValues != nil {
		loadProperties = s.service.ExactNotePropertyValues
	}
	propRows, err := loadProperties(ctx, knownPaths, nil, 0)
	if err != nil {
		return nil, err
	}
	loadTags := s.service.Store.CurrentNoteTags
	if s.service.ExactNoteTags != nil {
		loadTags = s.service.ExactNoteTags
	}
	tagRows, err := loadTags(ctx, knownPaths)
	if err != nil {
		return nil, err
	}
	typeRows, err := s.typesByPathsLocked(ctx, knownPaths)
	if err != nil {
		return nil, err
	}
	if err := s.ensureIssueFlagsLocked(ctx, knownPaths); err != nil {
		return nil, err
	}
	recordsByPath := make(map[string]NodeRecord, len(rowsByPath))
	for _, path := range pathsList {
		row, ok := rowsByPath[path]
		if !ok {
			continue
		}
		recordsByPath[path] = NodeRecord{
			Ref:                    ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote},
			Path:                   path,
			Title:                  firstNonEmptyString(row.Title, noteTitle(path, nil)),
			Frontmatter:            map[string]any{},
			InlineProps:            map[string][]string{},
			Tags:                   []string{},
			UpdatedAt:              row.Mtime,
			Format:                 noteformat.FormatID(row.FormatID),
			SourceRepresentation:   ontology.SourceRepresentationUTF8,
			EvidenceRepresentation: ontology.EvidenceRepresentationProviderProjection,
			Capabilities:           s.sourceCapabilities(row),
		}
	}
	frontmatterLists := make(map[string]map[string][]semdb.NotePropertyValueRow)
	for _, row := range propRows {
		record, ok := recordsByPath[row.NotePath]
		if !ok {
			continue
		}
		switch row.Source {
		case semdb.NotePropertySourceInline:
			record.InlineProps[row.PropertyName] = append(record.InlineProps[row.PropertyName], row.ValueText)
		default:
			if row.IsList {
				if frontmatterLists[row.NotePath] == nil {
					frontmatterLists[row.NotePath] = make(map[string][]semdb.NotePropertyValueRow)
				}
				frontmatterLists[row.NotePath][row.PropertyName] = append(frontmatterLists[row.NotePath][row.PropertyName], row)
				recordsByPath[row.NotePath] = record
				continue
			}
			record.Frontmatter[row.PropertyName] = propertyValue(row)
		}
		recordsByPath[row.NotePath] = record
	}
	for path, byName := range frontmatterLists {
		record, ok := recordsByPath[path]
		if !ok {
			continue
		}
		for name, rows := range byName {
			sort.Slice(rows, func(i, j int) bool { return rows[i].ListOrdinal < rows[j].ListOrdinal })
			values := make([]any, 0, len(rows))
			for _, row := range rows {
				if semdb.IsEmptyListSentinel(row) {
					continue
				}
				values = append(values, propertyValue(row))
			}
			record.Frontmatter[name] = values
		}
		recordsByPath[path] = record
	}
	for _, row := range tagRows {
		record, ok := recordsByPath[row.NotePath]
		if !ok {
			continue
		}
		record.Tags = append(record.Tags, row.TagNorm)
		recordsByPath[row.NotePath] = record
	}
	out := make([]NodeRecord, 0, len(pathsList))
	for _, path := range pathsList {
		record, ok := recordsByPath[path]
		if !ok {
			continue
		}
		record.TypeName = strings.TrimSpace(typeRows[path].TypeName)
		if record.TypeName == "" {
			record.TypeName = stringValue(record.Frontmatter[ontoschema.TypeFieldName()])
		}
		record.Ref.TypeName = record.TypeName
		record.HasIssues = s.issueByPath[path]
		out = append(out, record)
	}
	if err := s.hydrateSectionSummaries(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}
