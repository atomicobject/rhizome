package obsidian

import "context"

// ContextNoteReader is an optional extension for note readers that can stop a
// filesystem or remote read when the health check is cancelled.
type ContextNoteReader interface {
	NoteReader
	GetContentsContext(context.Context, VaultDefinition, string) (string, error)
}

// ContextNoteListReader is an optional extension for note readers that can
// stop note discovery when the health check is cancelled.
type ContextNoteListReader interface {
	NoteReader
	GetNotesListContext(context.Context, VaultDefinition) ([]string, error)
}

// NotesListContext preserves cancellation for both context-aware and legacy readers.
func NotesListContext(ctx context.Context, vaultDef VaultDefinition, note NoteReader) ([]string, error) {
	if reader, ok := note.(ContextNoteListReader); ok {
		return reader.GetNotesListContext(ctx, vaultDef)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	allNotes, err := note.GetNotesList(vaultDef)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return allNotes, nil
}
