package web

import (
	"context"
	"fmt"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
)

type exactNoteReadSnapshot struct {
	admittedPaths map[string]struct{}
	facts         semdb.DurableNoteFacts
}

func (s *Server) exactNoteFactsFromStore(ctx context.Context, store *semdb.Store, paths []string) (semdb.DurableNoteFacts, error) {
	if store == nil {
		return semdb.DurableNoteFacts{MetadataRows: map[string]semdb.NoteMetadataRow{}}, nil
	}
	facts, err := s.noteMetadata.UsableNoteFactsByPaths(ctx, store, paths)
	if err != nil {
		return semdb.DurableNoteFacts{}, err
	}
	for path := range facts.MetadataRows {
		if !s.exactNotePathOwned(path) {
			delete(facts.MetadataRows, path)
		}
	}
	properties := facts.PropertyValues[:0]
	for _, row := range facts.PropertyValues {
		if _, ok := facts.MetadataRows[row.NotePath]; ok {
			properties = append(properties, row)
		}
	}
	facts.PropertyValues = properties
	tags := facts.Tags[:0]
	for _, row := range facts.Tags {
		if _, ok := facts.MetadataRows[row.NotePath]; ok {
			tags = append(tags, row)
		}
	}
	facts.Tags = tags
	return facts, nil
}

func requireAdmittedExactPaths(paths []string, admitted map[string]struct{}) error {
	for _, path := range paths {
		if _, ok := admitted[path]; !ok {
			return fmt.Errorf("exact note path %q was not admitted for this request", path)
		}
	}
	return nil
}

func (snapshot *exactNoteReadSnapshot) metadataRows(ctx context.Context, paths []string) (map[string]semdb.NoteMetadataRow, error) {
	if snapshot == nil {
		return map[string]semdb.NoteMetadataRow{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := requireAdmittedExactPaths(paths, snapshot.admittedPaths); err != nil {
		return nil, err
	}
	rows := make(map[string]semdb.NoteMetadataRow, len(paths))
	for _, path := range paths {
		if row, ok := snapshot.facts.MetadataRows[path]; ok {
			rows[path] = row
		}
	}
	return rows, nil
}

func (snapshot *exactNoteReadSnapshot) propertyValues(ctx context.Context, paths []string, properties []string, source semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error) {
	if snapshot == nil {
		return []semdb.NotePropertyValueRow{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := requireAdmittedExactPaths(paths, snapshot.admittedPaths); err != nil {
		return nil, err
	}
	pathSet := stringSet(paths)
	propertySet := stringSetLower(properties)
	filterProperties := len(properties) > 0
	if filterProperties && len(propertySet) == 0 {
		return []semdb.NotePropertyValueRow{}, nil
	}
	rows := make([]semdb.NotePropertyValueRow, 0, len(snapshot.facts.PropertyValues))
	for _, row := range snapshot.facts.PropertyValues {
		if _, ok := pathSet[row.NotePath]; !ok {
			continue
		}
		if filterProperties {
			if _, ok := propertySet[strings.ToLower(strings.TrimSpace(row.PropertyName))]; !ok {
				continue
			}
		}
		if source != 0 && row.Source != source {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (snapshot *exactNoteReadSnapshot) tags(ctx context.Context, paths []string) ([]semdb.NoteTagRow, error) {
	if snapshot == nil {
		return []semdb.NoteTagRow{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := requireAdmittedExactPaths(paths, snapshot.admittedPaths); err != nil {
		return nil, err
	}
	pathSet := stringSet(paths)
	rows := make([]semdb.NoteTagRow, 0, len(snapshot.facts.Tags))
	for _, row := range snapshot.facts.Tags {
		if _, ok := pathSet[row.NotePath]; ok {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func stringSetLower(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}
