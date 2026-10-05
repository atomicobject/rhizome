package notemeta

import (
	"context"
	"fmt"
	pathpkg "path"
	"slices"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
)

type PublishedMetadataPreparation struct {
	canonicalPaths []string
	projectable    []string
	descriptors    map[string]noteformat.Descriptor
	sealedEntries  []projectedNoteEntry
	sealedPaths    map[string]struct{}
}

// FatalPaths returns projectable sealed sources whose provider reported a
// blocking projection result. Callers must not send these sources to syntax
// specific ingestion after ownership has cleared their prior evidence.
func (p PublishedMetadataPreparation) FatalPaths() []paths.NotePath {
	result := make([]paths.NotePath, 0)
	for _, entry := range p.sealedEntries {
		if entry.Projection.Status == noteformat.ProjectionStatusFatal {
			result = append(result, entry.Source.Path())
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

// OwnershipTransitions converts already-prepared current and blocking-fatal
// projections into durable source-only ownership states. It exposes no provider
// facts, and does not project or read source bytes again. Callers submit the
// result through the ownership writer control before destination ingestion.
func (p PublishedMetadataPreparation) OwnershipTransitions(observedAt int64) ([]semdb.OwnershipTransition, error) {
	if observedAt < 0 {
		return nil, fmt.Errorf("published projection observation time must not be negative")
	}
	transitions := make([]semdb.OwnershipTransition, 0)
	for _, entry := range p.sealedEntries {
		status, diagnostic, err := preparedOwnershipProjectionState(entry)
		if err != nil {
			return nil, err
		}
		if status == "" {
			continue
		}
		transitions = append(transitions, semdb.OwnershipTransition{
			Path:   entry.Source.Path().String(),
			Target: semdb.OwnershipTargetNote,
			Note: &semdb.NoteSourceState{
				Title:             preparedOwnershipTitle(entry),
				FormatID:          string(entry.Source.Format()),
				ContentHash:       entry.Source.ContentHash(),
				Mtime:             entry.Source.Mtime(),
				Size:              entry.Source.Size(),
				ProviderVersion:   entry.Projection.ProviderVersion,
				ProjectionVersion: entry.Projection.ProjectionVersion,
				Status:            status,
				DiagnosticCode:    diagnostic.Code,
				DiagnosticDetail:  diagnostic.Message,
				ObservedAt:        observedAt,
			},
		})
	}
	sort.Slice(transitions, func(left, right int) bool { return transitions[left].Path < transitions[right].Path })
	return transitions, nil
}

func preparedOwnershipProjectionState(entry projectedNoteEntry) (semdb.NoteProjectionStatus, noteformat.Diagnostic, error) {
	switch entry.Projection.Status {
	case noteformat.ProjectionStatusCurrent:
		return semdb.NoteProjectionStatusCurrent, noteformat.Diagnostic{}, nil
	case noteformat.ProjectionStatusFatal:
		diagnostic, ok := blockingProjectionDiagnostic(entry.Projection)
		if !ok {
			return "", noteformat.Diagnostic{}, fmt.Errorf("fatal published projection for %q has no blocking diagnostic", entry.Source.Path())
		}
		return semdb.NoteProjectionStatusFatal, diagnostic, nil
	default:
		return "", noteformat.Diagnostic{}, nil
	}
}

func preparedOwnershipTitle(entry projectedNoteEntry) string {
	if title := strings.TrimSpace(entry.Entry.Title); title != "" {
		return title
	}
	return publishedFallbackTitle(entry.Source.Path())
}

func blockingProjectionDiagnostic(projection noteformat.Projection) (noteformat.Diagnostic, bool) {
	for _, diagnostic := range projection.Diagnostics {
		if diagnostic.Blocking && strings.TrimSpace(diagnostic.Code) != "" && strings.TrimSpace(diagnostic.Message) != "" {
			return diagnostic, true
		}
	}
	return noteformat.Diagnostic{}, false
}

func publishedFallbackTitle(notePath paths.NotePath) string {
	base := pathpkg.Base(notePath.String())
	return strings.TrimSuffix(base, pathpkg.Ext(base))
}

// PreparePublishedMetadata projects supplied sealed sources once. It does not
// read a store or the filesystem, so full-index orchestration can use its
// provider status before Markdown-only ingestion and reuse the result later.
func (i Indexer) PreparePublishedMetadata(
	ctx context.Context,
	notePaths []paths.NotePath,
	sealed map[paths.NotePath]noteformat.AuthoredSource,
) (PublishedMetadataPreparation, error) {
	if err := ctx.Err(); err != nil {
		return PublishedMetadataPreparation{}, err
	}
	formats, err := i.runtime()
	if err != nil {
		return PublishedMetadataPreparation{}, err
	}
	canonicalPaths, err := canonicalPublishedNotePaths(notePaths)
	if err != nil {
		return PublishedMetadataPreparation{}, err
	}
	projectable, descriptors, err := publishedPathDescriptors(formats, canonicalPaths)
	if err != nil {
		return PublishedMetadataPreparation{}, err
	}
	sealedEntries, sealedPaths, err := i.projectSealedPublishedSources(ctx, formats, canonicalPaths, projectable, sealed)
	if err != nil {
		return PublishedMetadataPreparation{}, err
	}
	return PublishedMetadataPreparation{
		canonicalPaths: canonicalPaths,
		projectable:    projectable,
		descriptors:    descriptors,
		sealedEntries:  sealedEntries,
		sealedPaths:    sealedPaths,
	}, nil
}

// BuildPublishedMetadataDelta derives the complete metadata publication delta
// for an explicit, complete note-owner path set. Projectable sources are read
// only from the supplied canonical paths (or reused from sealed snapshots).
// Descriptor-only sources retain their already-published durable identity and
// never enter provider projection or the note-link target cache.
//
// The returned delta is pure with respect to Store. Coordinators submit it
// through the shared writer lane after ownership transitions have published

func validatePublishedMetadataPreparation(formats noteformat.Runtime, preparation PublishedMetadataPreparation) error {
	if preparation.canonicalPaths == nil || preparation.descriptors == nil || preparation.sealedPaths == nil {
		return fmt.Errorf("published metadata preparation is required")
	}
	projectable, descriptors, err := publishedPathDescriptors(formats, preparation.canonicalPaths)
	if err != nil {
		return err
	}
	if len(projectable) != len(preparation.projectable) || len(descriptors) != len(preparation.descriptors) {
		return fmt.Errorf("published metadata preparation does not match selected paths")
	}
	for index, notePath := range projectable {
		if preparation.projectable[index] != notePath {
			return fmt.Errorf("published metadata preparation does not match selected paths")
		}
	}
	for notePath, descriptor := range descriptors {
		prepared, ok := preparation.descriptors[notePath]
		if !ok || !samePublishedDescriptor(prepared, descriptor) {
			return fmt.Errorf("published metadata preparation does not match selected providers")
		}
	}
	for _, entry := range preparation.sealedEntries {
		if err := validatePublishedSource(formats, entry.Source.Path(), entry.Source); err != nil {
			return err
		}
		if _, ok := preparation.sealedPaths[entry.Source.Path().String()]; !ok {
			return fmt.Errorf("published metadata preparation is missing a sealed source path")
		}
	}
	return nil
}

func samePublishedDescriptor(left, right noteformat.Descriptor) bool {
	return left.ID == right.ID &&
		left.ProviderVersion == right.ProviderVersion &&
		left.ProjectionVersion == right.ProjectionVersion &&
		left.OwnershipPolicy == right.OwnershipPolicy &&
		slices.Equal(left.Extensions, right.Extensions) &&
		slices.Equal(left.Capabilities.Values(), right.Capabilities.Values())
}

func publishedDurableFatalRows(ctx context.Context, store Store, formats noteformat.Runtime, pathsList []string) ([]semdb.NoteMetadataRow, map[string]struct{}, error) {
	if len(pathsList) == 0 {
		return nil, map[string]struct{}{}, nil
	}
	provider, ok := store.(durableMetadataRowsProvider)
	if !ok {
		return nil, nil, fmt.Errorf("note metadata store does not expose durable source rows")
	}
	rowsByPath, err := provider.DurableNoteMetadataRowsByPaths(ctx, pathsList)
	if err != nil {
		return nil, nil, err
	}
	rows := make([]semdb.NoteMetadataRow, 0)
	handled := make(map[string]struct{})
	for _, notePath := range pathsList {
		row, found := rowsByPath[notePath]
		if !found || !publishedFatalProjection(formats, row) {
			continue
		}
		rows = append(rows, row)
		handled[notePath] = struct{}{}
	}
	return rows, handled, nil
}

func publishedFatalProjection(formats noteformat.Runtime, row semdb.NoteMetadataRow) bool {
	descriptor, selected := selectedProviderDescriptorForRow(formats, row)
	if !selected || !formats.CanProject(descriptor.ID) {
		return false
	}
	return row.Projection.Status == semdb.NoteProjectionStatusFatal &&
		row.ContentHash == "" &&
		row.Projection.SourceContentHash == "" &&
		row.Projection.ProviderVersion == descriptor.ProviderVersion &&
		row.Projection.ProjectionVersion == descriptor.ProjectionVersion &&
		row.Projection.DiagnosticCode != "" &&
		row.Projection.DiagnosticDetail != ""
}
