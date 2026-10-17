package ontology

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"sort"
	"strconv"
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
func catalogNodeWitnesses(nodes []readmodel.NodeRow) map[string]catalogWitness {
	identities := make(map[string][]string)
	for _, node := range nodes {
		node.UpdatedAt = 0
		identity, _ := json.Marshal(node)
		identities[node.NotePath] = append(identities[node.NotePath], string(identity))
	}
	return catalogWitnesses(identities)
}

func catalogFieldWitnesses(fields []readmodel.FieldValueRow) map[string]catalogWitness {
	values := make(map[string][]string)
	for _, field := range fields {
		field.UpdatedAt = 0
		real := "<nil>"
		if field.ValueReal != nil {
			value := *field.ValueReal
			if value == 0 {
				value = 0
			}
			real = strconv.FormatFloat(value, 'g', -1, 64)
			field.ValueReal = nil
		}
		encoded, _ := json.Marshal(struct {
			Field readmodel.FieldValueRow
			Real  string
		}{field, real})
		values[field.NotePath] = append(values[field.NotePath], string(encoded))
	}
	return catalogWitnesses(values)
}

func catalogWitnesses(identities map[string][]string) map[string]catalogWitness {
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

// These rows drive relation queries and future incremental invalidation. Their
// witness includes intentionally empty outputs and excludes publication time.
func ontologyOutputWitnesses(types []semdb.OntologyNoteTypeRow, edges []semdb.OntologyEdgeRow, dependencies []readmodel.LinkDependencyRow) map[string]catalogWitness {
	rows := make(map[string]map[string]string)
	add := func(path, key string, row any) {
		if rows[path] == nil {
			rows[path] = make(map[string]string)
		}
		encoded, _ := json.Marshal(row)
		rows[path][key] = string(encoded)
	}
	for _, row := range types {
		row.UpdatedAt = 0
		add(row.NotePath, "type", row)
	}
	for _, row := range edges {
		row.UpdatedAt = 0
		key := "edge\x00" + row.SrcNodeID + "\x00" + row.RelationName + "\x00" + row.DstPath + "\x00" + row.DstNodeID + "\x00" + row.Provenance
		add(row.SrcPath, key, row)
	}
	for _, row := range dependencies {
		row.UpdatedAt = 0
		key := "dependency\x00" + row.NodeID + "\x00" + row.FieldName + "\x00" + row.TargetInputNorm
		add(row.SourceNotePath, key, row)
	}
	values := make(map[string][]string, len(rows))
	for path, keyed := range rows {
		for key, row := range keyed {
			values[path] = append(values[path], key+"\x00"+row)
		}
	}
	return catalogWitnesses(values)
}

func ontologyPoliciesMatch(schema *ontoschema.Schema, rows []semdb.OntologyTypePolicyRow) bool {
	canonical := func(rows []semdb.OntologyTypePolicyRow) catalogWitness {
		values := map[string][]string{}
		for _, row := range rows {
			row.UpdatedAt = 0
			encoded, _ := json.Marshal(row)
			values[""] = append(values[""], string(encoded))
		}
		return catalogWitnessForPath(catalogWitnesses(values), "")
	}
	return canonical(rows) == canonical(buildTypePolicyRows(schema, 0))
}

func setOntologyOutputWitnesses(results []noteSyncResult, dependencies []readmodel.LinkDependencyRow) {
	var types []semdb.OntologyNoteTypeRow
	var edges []semdb.OntologyEdgeRow
	for _, result := range results {
		if result.noteType != nil {
			types = append(types, *result.noteType)
		}
		edges = append(edges, result.edges...)
	}
	witnesses := ontologyOutputWitnesses(types, edges, dependencies)
	for _, result := range results {
		if result.assessment != nil {
			witness := catalogWitnessForPath(witnesses, result.path)
			result.assessment.OutputRowCount = &witness.Count
			result.assessment.OutputRowDigest = witness.Digest
		}
	}
}
