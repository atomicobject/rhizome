package ontology

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"path"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

func planOntologySync(ctx context.Context, store *semdb.Store, changedPaths, deletedPaths []string) (_ *syncPlan, err error) {
	phaseCtx := indexingperf.WithPhase(ctx, "plan_ontology")
	done := indexingperf.StartSpan(phaseCtx, "plan_ontology")
	defer func() { done(err) }()

	changedPaths = normalizeStringSet(changedPaths)
	deletedPaths = normalizeStringSet(deletedPaths)
	touched := append([]string(nil), changedPaths...)
	touched = append(touched, deletedPaths...)
	touched = normalizeStringSet(touched)
	if len(touched) == 0 {
		return &syncPlan{ForcePaths: map[string]struct{}{}}, nil
	}

	affected := make(map[string]struct{}, len(touched)*3)
	// Dependency work must survive membership in metadata's expanded candidate set.
	forcePaths := make(map[string]struct{}, len(touched)*3)
	for _, path := range touched {
		affected[path] = struct{}{}
	}

	if len(changedPaths) > 0 {
		rows, err := store.GraphDocNoteEdgesForPaths(phaseCtx, changedPaths)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			affected[row.SrcPath] = struct{}{}
			affected[row.DstPath] = struct{}{}
			forcePaths[row.SrcPath] = struct{}{}
			forcePaths[row.DstPath] = struct{}{}
		}
	}

	edges, err := store.OntologyEdgesForPaths(phaseCtx, touched, true, "", 0)
	if err != nil {
		return nil, err
	}
	for _, edge := range edges {
		affected[edge.SrcPath] = struct{}{}
		affected[edge.DstPath] = struct{}{}
		forcePaths[edge.SrcPath] = struct{}{}
		forcePaths[edge.DstPath] = struct{}{}
	}

	// Renames surface as a (deletedPath, changedPath) pair where the deleted
	// side carried unresolved link dependencies keyed by old basename/alias.
	// Feed both sides into target_input_norm planning so dependents like
	// `[[alice]]` get re-evaluated when alice.md disappears.
	dependencyPaths := append([]string(nil), changedPaths...)
	dependencyPaths = append(dependencyPaths, deletedPaths...)
	aliases, err := notemeta.LoadAliasMap(phaseCtx, store)
	if err != nil {
		return nil, err
	}
	linkDependencySources, err := store.OntologyNodeLinkDependencySources(phaseCtx, touched, ontologyDependencyTargetInputNorms(dependencyPaths, aliases))
	if err != nil {
		return nil, err
	}
	for _, sourcePath := range linkDependencySources {
		affected[sourcePath] = struct{}{}
		forcePaths[sourcePath] = struct{}{}
	}

	deletedSet := make(map[string]struct{}, len(deletedPaths))
	for _, path := range deletedPaths {
		deletedSet[path] = struct{}{}
	}
	loadPaths := make([]string, 0, len(affected))
	replacePaths := make([]string, 0, len(affected))
	for path := range affected {
		if _, deleted := deletedSet[path]; deleted {
			delete(forcePaths, path)
			continue
		}
		loadPaths = append(loadPaths, path)
		replacePaths = append(replacePaths, path)
	}
	sort.Strings(loadPaths)
	sort.Strings(replacePaths)
	return &syncPlan{
		LoadPaths:    loadPaths,
		ReplacePaths: replacePaths,
		DeletePaths:  deletedPaths,
		ForcePaths:   forcePaths,
	}, nil
}

func ontologyDependencyTargetInputNorms(paths []string, aliasesByPath map[string][]string) []string {
	seen := make(map[string]struct{})
	for _, notePath := range paths {
		notePath = strings.TrimSpace(notePath)
		if notePath == "" {
			continue
		}
		for _, value := range []string{
			notePath,
			strings.TrimSuffix(notePath, path.Ext(notePath)),
			strings.TrimSuffix(path.Base(notePath), path.Ext(notePath)),
		} {
			if norm := readmodel.LinkDependencyKey(value); norm != "" {
				seen[norm] = struct{}{}
			}
		}
		for _, alias := range aliasesByPath[notePath] {
			if norm := readmodel.LinkDependencyKey(alias); norm != "" {
				seen[norm] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func prepareOntologyJobs(plan *syncPlan, schema *ontoschema.Schema, docsByPath map[string]*noteDoc, metaByPath map[string]semdb.NoteMetadataRow, states map[string]semdb.OntologyNoteStateRow, graphByPath map[string][]semdb.GraphDocEdge, materializationVersion int) ([]noteSyncJob, map[string]struct{}, int, error) {
	jobs := make([]noteSyncJob, 0, len(plan.LoadPaths))
	skipped := 0
	for _, path := range plan.LoadPaths {
		doc := docsByPath[path]
		if doc == nil {
			continue
		}
		assessment := resolveNoteAssessment(doc, schema)
		fingerprint := ontologyFingerprint(doc, metaByPath[path], schema.Hash, graphByPath[path])
		if _, forced := plan.ForcePaths[path]; !forced && materializationVersion == OntologyMaterializationVersion {
			if state, ok := states[path]; ok && state.SchemaHash == schema.Hash && state.InputFingerprint == fingerprint {
				skipped++
				continue
			}
		}
		projected, err := projectResolvedNote(doc, schema, assessment)
		if err != nil {
			return nil, nil, 0, err
		}
		jobs = append(jobs, noteSyncJob{
			projected:   projected,
			doc:         doc,
			assessment:  assessment,
			fingerprint: fingerprint,
			meta:        metaByPath[path],
		})
	}
	replaceSet := make(map[string]struct{}, len(jobs))
	for _, job := range jobs {
		replaceSet[job.doc.Path] = struct{}{}
	}
	return jobs, replaceSet, skipped, nil
}
