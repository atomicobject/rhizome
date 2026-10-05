package codeanchor

import (
	"context"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/noteformat"
)

// NoteSource is the note-file fact subset anchors need for declaration
// extraction. It is method-shaped so canonical note snapshots can satisfy it
// without making anchors import the note metadata package.
type NoteSource interface {
	NotePathString() string
	NoteFormatID() noteformat.FormatID
	NoteContentString() string
	NoteContentHashString() string
	NoteMtimeUnix() int64
}

type AnchorDeclarations struct {
	Note       Note
	KeepLabels []string
}

// AnchorNoteStore persists the current anchor-note adapter output without
// owning the note's general metadata.
type AnchorNoteStore interface {
	UpsertNoteWithCleanup(ctx context.Context, note Note, keepLabels []string) (AnchorUpsertResult, error)
}

// ExtractAnchorDeclarations consumes note source facts and returns only the
// anchor declarations anchors owns.
func (s *Service) ExtractAnchorDeclarations(ctx context.Context, src NoteSource) (AnchorDeclarations, error) {
	_ = ctx
	if src == nil {
		return AnchorDeclarations{}, fmt.Errorf("note source is required")
	}
	if src.NoteFormatID() != noteformat.FormatID("markdown") {
		return AnchorDeclarations{}, fmt.Errorf("code-anchor declarations require Markdown source, got %q", src.NoteFormatID())
	}
	note, err := ParseNote(src.NotePathString(), src.NoteContentString())
	if err != nil {
		return AnchorDeclarations{}, err
	}
	if err := s.resolvePathLikeAnchors(&note); err != nil {
		return AnchorDeclarations{}, err
	}
	return AnchorDeclarations{Note: note, KeepLabels: keepLabelsForNote(note)}, nil
}

func (s *Service) resolvePathLikeAnchors(note *Note) error {
	if note == nil {
		return nil
	}
	for i := range note.DefinedAnchors {
		switch note.DefinedAnchors[i].Kind {
		case AnchorPath:
			resolved, err := resolvePathPrefix(note.DefinedAnchors[i].PathPrefix, s.vault)
			if err != nil {
				return fmt.Errorf("anchor %q: %w", note.DefinedAnchors[i].Label, err)
			}
			note.DefinedAnchors[i].PathPrefix = resolved
		case AnchorGlob:
			resolved, err := resolveGlobPatterns(note.DefinedAnchors[i].Globs, s.vault)
			if err != nil {
				return fmt.Errorf("anchor %q: %w", note.DefinedAnchors[i].Label, err)
			}
			note.DefinedAnchors[i].Globs = resolved
		}
	}
	return nil
}

func keepLabelsForNote(note Note) []string {
	labels := make([]string, 0, len(note.DefinedAnchors))
	for _, anchor := range note.DefinedAnchors {
		labels = append(labels, anchor.Label)
	}
	return labels
}
