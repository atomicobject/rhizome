package ontology

import (
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
	"time"
)

func buildOntologyNodeCatalogReadModel(schema *ontoschema.Schema, results []noteSyncResult, linkResolver OntologyLinkTargetResolver) (readmodel.NodeReadModel, error) {
	if schema == nil || len(results) == 0 {
		return readmodel.NodeReadModel{}, nil
	}
	var paths []string
	var nodes []readmodel.NodeRow
	var fields []readmodel.FieldValueRow
	var dependencies []readmodel.LinkDependencyRow
	now := time.Now().Unix()
	for _, result := range results {
		if strings.TrimSpace(result.path) != "" {
			paths = append(paths, result.path)
		}
		projected := result.projected
		if projected == nil {
			continue
		}
		readModel, err := buildIntelOntologyNodeReadModel(schema, projected.root, projected.resolver, now, BuildIntelOntologyNodeReadModelOptions{LinkResolver: linkResolver})
		if err != nil {
			return readmodel.NodeReadModel{}, err
		}
		paths = append(paths, readModel.NotePaths...)
		nodes = append(nodes, readModel.Nodes...)
		fields = append(fields, readModel.FieldValues...)
		dependencies = append(dependencies, readModel.LinkDependencies...)
	}
	return readmodel.NodeReadModel{
		NotePaths:        normalizeStringSet(paths),
		Nodes:            nodes,
		FieldValues:      fields,
		LinkDependencies: dependencies,
	}, nil
}

func ontologyTypeResolverFromSyncResults(results []noteSyncResult, externalTypes map[string]string) func(string) string {
	resolved := make(map[string]string, len(results)+len(externalTypes))
	for path, typeName := range externalTypes {
		if strings.TrimSpace(path) != "" && strings.TrimSpace(typeName) != "" {
			resolved[path] = strings.TrimSpace(typeName)
		}
	}
	for _, result := range results {
		typeName := ""
		if result.assessment != nil {
			typeName = strings.TrimSpace(result.assessment.ResolvedType)
		}
		if typeName == "" && result.noteType != nil {
			typeName = strings.TrimSpace(result.noteType.TypeName)
		}
		if strings.TrimSpace(result.path) != "" && typeName != "" {
			resolved[result.path] = typeName
		}
	}
	return func(path string) string {
		return strings.TrimSpace(resolved[path])
	}
}
