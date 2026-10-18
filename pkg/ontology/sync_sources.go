package ontology

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func loadNoteDocsFromStoreByPaths(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store, notePaths []string) (map[string]*noteDoc, map[string]semdb.NoteMetadataRow, error) {
	rowsByPath, err := store.CurrentNoteMetadataRowsByPaths(ctx, notePaths)
	if err != nil {
		return nil, nil, err
	}
	sources, err := noteMetadata.LoadNoteSourceSnapshots(ctx, vaultDef, noteMgr, store, notePaths)
	if err != nil {
		return nil, nil, err
	}
	docsByPath := make(map[string]*noteDoc, len(sources))
	for _, source := range sources {
		doc, err := projectNoteSource(source)
		if err != nil {
			return nil, nil, err
		}
		docsByPath[doc.Path] = doc
	}
	return docsByPath, rowsByPath, nil
}
