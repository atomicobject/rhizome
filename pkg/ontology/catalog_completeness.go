package ontology

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
)

// PublishedCatalogPaths returns selected sources whose catalog matches the
// current metadata and schema. An intentionally empty projection is complete.
func PublishedCatalogPaths(ctx context.Context, store *semdb.Store, metadata map[string]semdb.NoteMetadataRow, schemaHash string) (map[string]bool, error) {
	paths := make([]string, 0, len(metadata))
	for path := range metadata {
		paths = append(paths, path)
	}
	assessments, err := store.OntologyAssessmentsByPaths(ctx, paths)
	if err != nil {
		return nil, err
	}
	nodes, err := store.OntologyNodesByPaths(ctx, paths)
	if err != nil {
		return nil, err
	}
	catalog := catalogNodeWitnesses(nodes)
	staleSchema := make(map[string]bool)
	for _, node := range nodes {
		if node.SchemaHash != schemaHash {
			staleSchema[node.NotePath] = true
		}
	}
	published := make(map[string]bool, len(paths))
	for path, meta := range metadata {
		row, ok := assessments[path]
		if !ok || row.SchemaHash != schemaHash || staleSchema[path] {
			continue
		}
		assessment, err := AssessmentFromJSON(row.AssessmentJSON)
		if err != nil || assessment == nil || assessment.NotePath != path || assessment.SourceContentHash == "" || assessment.SourceContentHash != meta.ContentHash || assessment.CatalogNodeCount == nil {
			continue
		}
		witness := catalogWitnessForPath(catalog, path)
		published[path] = *assessment.CatalogNodeCount == witness.Count && assessment.CatalogNodeDigest == witness.Digest
	}
	return published, nil
}

type catalogWitness struct {
	Count  int
	Digest string
}

// Witness canonical projection output rather than infer completeness from the
// stored rows being checked. Sorting makes the witness independent of write order.
func catalogNodeWitnesses(nodes []codeanchor.IntelOntologyNode) map[string]catalogWitness {
	identities := make(map[string][]string)
	for _, node := range nodes {
		identity, _ := json.Marshal([]string{
			node.NodeID, node.NotePath, node.NodeKind, node.TypeName,
			node.ParentNodeID, node.ParentTypeName, node.SourceLocator,
			node.Fragment, node.BlockID, node.LocatorStatus, node.StructuralFingerprint,
		})
		identities[node.NotePath] = append(identities[node.NotePath], string(identity))
	}
	witnesses := make(map[string]catalogWitness, len(identities))
	for path, rows := range identities {
		sort.Strings(rows)
		hash := sha256.New()
		for _, row := range rows {
			_, _ = hash.Write([]byte(row + "\n"))
		}
		witnesses[path] = catalogWitness{Count: len(rows), Digest: hex.EncodeToString(hash.Sum(nil))}
	}
	return witnesses
}

func catalogWitnessForPath(witnesses map[string]catalogWitness, path string) catalogWitness {
	if witness, ok := witnesses[path]; ok {
		return witness
	}
	empty := sha256.Sum256(nil)
	return catalogWitness{Digest: hex.EncodeToString(empty[:])}
}
