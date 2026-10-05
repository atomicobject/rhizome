package cmd

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func liveNodeLinkApplier(vaultDef obsidian.VaultDefinition, metadata notemeta.Indexer) func(context.Context, string, ontology.LinkTargetRequest) (ontology.LinkTargetResult, error) {
	return func(ctx context.Context, schemaHash string, request ontology.LinkTargetRequest) (ontology.LinkTargetResult, error) {
		return indexing.ApplyNodeLinkTargets(ctx, indexing.NodeLinkApplyRequest{VaultDef: vaultDef, NoteMetadata: metadata, NoteReader: &obsidian.Note{}, SchemaHash: schemaHash, LinkTarget: request})
	}
}
