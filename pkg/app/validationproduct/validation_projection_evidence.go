package validationproduct

import ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

import (
	"context"
	"errors"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func markValidationCodeAnchorsFresh(ctx context.Context, store *semdb.Store, result *ValidationProjectionResult) error {
	summary, err := store.UntouchedIndexDomainsSummary(ctx)
	if err != nil {
		return err
	}
	selectors, err := store.CodeAnchorSelectorSummary(ctx)
	if err != nil {
		return err
	}
	result.Freshness[ProjectionDomainCodeAnchors] = ProjectionFreshness{
		State:      ProjectionFresh,
		Hash:       codeAnchorFreshnessHash(summary.CodeAnchors, selectors),
		Generation: summary.CodeAnchors.Generation,
	}
	return nil
}

func codeAnchorFreshnessHash(matches semdb.PersistedDomainSummary, selectors semdb.PersistedContentSummary) string {
	return fmt.Sprintf(
		"matches:%d;generation:%d;selectors:%d:%s",
		matches.Count, matches.Generation, selectors.Count, selectors.Hash,
	)
}

func populateValidationProjectionEvidence(ctx context.Context, vaultDef obsidian.VaultDefinition, store *semdb.Store, result *ValidationProjectionResult) error {
	metadataState, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return err
	}
	if metadataState.Ready {
		evidence := ProjectionFreshness{State: ProjectionFresh, Hash: metadataState.NotesHash, Generation: metadataState.LoadedAt}
		result.Freshness[ProjectionDomainMetadata] = evidence
	}
	ontologyState, err := store.GetOntologySchemaState(ctx)
	if err != nil {
		return err
	}
	if ontologyState.Ready && ontologyState.MaterializationVersion == ontology.OntologyMaterializationVersion {
		result.Freshness[ProjectionDomainOntology] = ProjectionFreshness{
			State:      ProjectionFresh,
			Hash:       fmt.Sprintf("%s;materialization:%d", ontologyState.SchemaHash, ontologyState.MaterializationVersion),
			Generation: ontologyState.LoadedAt,
		}
	}
	if err := populateUntouchedDomainEvidence(ctx, store, result.Freshness); err != nil {
		return err
	}
	if result.Runtime == nil {
		result.Runtime = &ontology.Runtime{Store: store, Issues: ontology.ValidationIssuesFromJSON(ontologyState.ErrorJSON)}
		schema, schemaErr := ontoschema.LoadSchema(vaultDef.BasePath())
		if schemaErr == nil && ontologyState.Ready && schema.Hash == ontologyState.SchemaHash && ontologyState.MaterializationVersion == ontology.OntologyMaterializationVersion {
			result.Runtime.Schema = schema
			result.Runtime.Ready = true
		} else if schemaErr != nil && !errors.Is(schemaErr, ontoschema.ErrNoOntologyFiles) {
			return schemaErr
		}
	}
	return nil
}

func populateUntouchedDomainEvidence(ctx context.Context, store *semdb.Store, freshness map[ProjectionDomain]ProjectionFreshness) error {
	summary, err := store.UntouchedIndexDomainsSummary(ctx)
	if err != nil {
		return err
	}
	packMetadata, err := store.GetPackMetadata(ctx)
	if err != nil {
		return err
	}
	codeHash := fmt.Sprintf("count:%d;indexer:%s", summary.Code.Count, packMetadata.IndexerVersion)
	freshness[ProjectionDomainCode] = persistedDomainEvidence(summary.Code.Count > 0 || packMetadata.IndexerVersion != "", summary.Code.Generation, codeHash)
	selectors, err := store.CodeAnchorSelectorSummary(ctx)
	if err != nil {
		return err
	}
	freshness[ProjectionDomainCodeAnchors] = persistedDomainEvidence(
		summary.CodeAnchors.Count > 0 || selectors.Count > 0,
		summary.CodeAnchors.Generation,
		codeAnchorFreshnessHash(summary.CodeAnchors, selectors),
	)

	chunks, err := store.IntelChunksSummary(ctx)
	if err != nil {
		return err
	}
	freshness[ProjectionDomainChunks] = persistedDomainEvidence(chunks.Count > 0, chunks.Generation, fmt.Sprintf("count:%d", chunks.Count))
	embeddings, err := store.EmbeddingsSummary(ctx)
	if err != nil {
		return err
	}
	embeddingHash := fmt.Sprintf("count:%d;model:%s", embeddings.Count, packMetadata.ModelHash)
	freshness[ProjectionDomainEmbeddings] = persistedDomainEvidence(embeddings.Count > 0, embeddings.Generation, embeddingHash)

	graphCount := summary.GraphDocScores.Count + summary.GraphAnchorScores.Count
	graphGeneration := summary.GraphDocScores.Generation
	if summary.GraphAnchorScores.Generation > graphGeneration {
		graphGeneration = summary.GraphAnchorScores.Generation
	}
	freshness[ProjectionDomainGraphScores] = persistedDomainEvidence(graphCount > 0, graphGeneration, fmt.Sprintf("docs:%d;anchors:%d", summary.GraphDocScores.Count, summary.GraphAnchorScores.Count))
	return nil
}

func persistedDomainEvidence(available bool, generation int64, hash string) ProjectionFreshness {
	if !available {
		return ProjectionFreshness{State: ProjectionUnavailable}
	}
	return ProjectionFreshness{State: ProjectionUntouched, Hash: hash, Generation: generation}
}

func stringPathsToNotePaths(input []string) []paths.NotePath {
	out := make([]paths.NotePath, 0, len(input))
	for _, path := range input {
		if normalized := paths.NormalizeNotePath(path); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func notePathsToStrings(input []paths.NotePath) []string {
	out := make([]string, 0, len(input))
	for _, path := range input {
		if normalized := path.String(); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func untouchedProjectionFreshness() map[ProjectionDomain]ProjectionFreshness {
	out := make(map[ProjectionDomain]ProjectionFreshness)
	for _, domain := range []ProjectionDomain{
		ProjectionDomainMetadata,
		ProjectionDomainOntology,
		ProjectionDomainCode,
		ProjectionDomainCodeAnchors,
		ProjectionDomainChunks,
		ProjectionDomainEmbeddings,
		ProjectionDomainGraphScores,
	} {
		out[domain] = ProjectionFreshness{State: ProjectionUntouched}
	}
	return out
}
