package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func loadExternalOntologyTypes(ctx context.Context, store *semdb.Store, schema *ontoschema.Schema, jobs []noteSyncJob, cache *obsidian.NotePathCache, replaceSet map[string]struct{}, graphRows []semdb.GraphDocEdge) (map[string]string, error) {
	targets := make(map[string]struct{}, len(jobs)*4)
	for _, job := range jobs {
		if job.assessment == nil || job.assessment.ResolvedType == "" {
			if docMatchesAnyGlobalSource(job.doc, schema) {
				globalTargets, err := globalSourceLinkTargets(job.projected, schema, cache)
				if err != nil {
					return nil, err
				}
				for _, targetPath := range globalTargets {
					if _, replacing := replaceSet[targetPath]; replacing {
						continue
					}
					targets[targetPath] = struct{}{}
				}
			}
			continue
		}
		noteType := schema.Types[job.assessment.ResolvedType]
		if noteType != nil {
			for _, field := range noteType.Fields {
				if field.Kind != ontoschema.FieldKindLink {
					continue
				}
				for _, raw := range extractFieldValues(job.doc, field) {
					targetPath, ok := cache.ResolveNote(UnwrapLinkValue(raw))
					if !ok {
						continue
					}
					if _, replacing := replaceSet[targetPath]; replacing {
						continue
					}
					targets[targetPath] = struct{}{}
				}
			}
		}
		if docMatchesAnyGlobalSource(job.doc, schema) {
			globalTargets, err := globalSourceLinkTargets(job.projected, schema, cache)
			if err != nil {
				return nil, err
			}
			for _, targetPath := range globalTargets {
				if _, replacing := replaceSet[targetPath]; replacing {
					continue
				}
				targets[targetPath] = struct{}{}
			}
		}
	}
	for _, row := range graphRows {
		for _, path := range []string{row.SrcPath, row.DstPath} {
			if _, replacing := replaceSet[path]; replacing {
				continue
			}
			targets[path] = struct{}{}
		}
	}

	paths := make([]string, 0, len(targets))
	for path := range targets {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	rows, err := store.OntologyTypesByPaths(indexingperf.WithPhase(ctx, "load_ontology_inputs"), paths)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for path, row := range rows {
		out[path] = row.TypeName
	}
	return out, nil
}

func globalSourceLinkTargets(note *projectedNote, schema *ontoschema.Schema, cache *obsidian.NotePathCache) ([]string, error) {
	if note == nil || note.resolver == nil || cache == nil {
		return nil, nil
	}
	resolver := note.resolver
	refs := resolver.globalSourceNodeRefs()
	seenRefs := map[string]struct{}{}
	seenTargets := map[string]struct{}{}
	var out []string
	var walk func(NodeRef) error
	walk = func(ref NodeRef) error {
		key := ref.String()
		if _, ok := seenRefs[key]; ok {
			return nil
		}
		seenRefs[key] = struct{}{}
		projection, err := resolver.projectCurrent(ref)
		if err != nil {
			return err
		}
		if projection == nil || projection.Type == nil {
			return nil
		}
		for _, field := range projection.Type.Fields {
			if field == nil || field.Kind != ontoschema.FieldKindLink {
				continue
			}
			for _, raw := range projection.Fields[field.Name].Values {
				targetPath, ok := cache.ResolveNote(UnwrapLinkValue(raw))
				if !ok {
					continue
				}
				if _, exists := seenTargets[targetPath]; exists {
					continue
				}
				seenTargets[targetPath] = struct{}{}
				out = append(out, targetPath)
			}
		}
		for _, binding := range projection.Fields {
			for _, childRef := range binding.SectionNodes {
				if err := walk(childRef); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, ref := range refs {
		if err := walk(ref); err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

func computeOntologyJobs(ctx context.Context, jobs []noteSyncJob, schema *ontoschema.Schema, cache *obsidian.NotePathCache, graphByPath map[string][]semdb.GraphDocEdge, externalTypes map[string]string) (_ []noteSyncResult, err error) {
	if len(jobs) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	phaseCtx := indexingperf.WithPhase(ctx, "compute_ontology")
	done := indexingperf.StartSpan(phaseCtx, "compute_ontology")
	defer func() { done(err) }()

	computedTypes := make(map[string]string, len(jobs))
	docTypes := make(map[string]*ontoschema.NoteType, len(jobs))
	for _, job := range jobs {
		if job.assessment == nil || job.assessment.ResolvedType == "" {
			continue
		}
		computedTypes[job.doc.Path] = job.assessment.ResolvedType
		docTypes[job.doc.Path] = schema.Types[job.assessment.ResolvedType]
	}
	resolveType := func(path string) string {
		if v := strings.TrimSpace(computedTypes[path]); v != "" {
			return v
		}
		return strings.TrimSpace(externalTypes[path])
	}

	workerCount := min(max(runtime.GOMAXPROCS(0), 2), len(jobs))
	jobCh := make(chan noteSyncJob)
	resultCh := make(chan noteSyncResult, len(jobs))
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				res, err := computeOntologyJob(job, schema, cache, graphByPath[job.doc.Path], docTypes[job.doc.Path], resolveType)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				resultCh <- res
			}
		}()
	}

	canceled := false
	var computeErr error
	for _, job := range jobs {
		select {
		case jobCh <- job:
		case <-ctx.Done():
			canceled = true
		case computeErr = <-errCh:
		}
		if canceled || computeErr != nil {
			break
		}
	}
	close(jobCh)
	wg.Wait()
	close(resultCh)
	if computeErr != nil {
		return nil, computeErr
	}
	if canceled {
		return nil, ctx.Err()
	}

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	results := make([]noteSyncResult, 0, len(jobs))
	for res := range resultCh {
		results = append(results, res)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].path < results[j].path })
	indexingperf.AddCount(phaseCtx, indexingperf.OntologyNotesAssessed, int64(len(results)))
	return results, nil
}

func computeOntologyJob(job noteSyncJob, schema *ontoschema.Schema, cache *obsidian.NotePathCache, incidentRows []semdb.GraphDocEdge, noteType *ontoschema.NoteType, resolveType func(string) string) (noteSyncResult, error) {
	now := time.Now().Unix()
	res := noteSyncResult{path: job.doc.Path, projected: job.projected}
	assessment := NoteAssessment{NotePath: job.doc.Path}
	if job.assessment != nil {
		assessment = *job.assessment
	}
	res.assessment = &assessment
	res.row = &semdb.OntologyNoteAssessmentRow{
		NotePath:     job.doc.Path,
		DeclaredType: assessment.DeclaredType,
		ResolvedType: assessment.ResolvedType,
		SchemaHash:   schema.Hash,
		UpdatedAt:    now,
	}
	res.noteState = &semdb.OntologyNoteStateRow{
		NotePath:         job.doc.Path,
		InputFingerprint: job.fingerprint,
		SchemaHash:       schema.Hash,
		ResolvedType:     assessment.ResolvedType,
		UpdatedAt:        now,
	}
	nodeEdges, err := buildNodeScopedEdges(job.projected.resolver, job.projected.root, schema, cache, resolveType, now)
	if err != nil {
		return noteSyncResult{}, err
	}
	res.edges = append(res.edges, nodeEdges...)
	if job.doc.Snapshot != nil {
		res.issues = append(res.issues, validationIssuesFromBlockIDs(job.doc, schema)...)
		res.issues = append(res.issues, validateEmbeddedFieldsWithResolver(job.doc.Snapshot, schema, job.projected.root, job.projected.resolver)...)
	}
	if assessment.ResolvedType == "" || noteType == nil {
		assessment.Issues = append(assessment.Issues, res.issues...)
		sortValidationIssues(assessment.Issues)
		res.relationCnt = len(res.edges)
		setAssessmentRow(res.row, assessment)
		return res, nil
	}
	resolvedDoc := *job.doc
	resolvedDoc.TypeName = assessment.ResolvedType

	res.noteType = &semdb.OntologyNoteTypeRow{
		NotePath:   job.doc.Path,
		TypeName:   assessment.ResolvedType,
		SchemaHash: schema.Hash,
		UpdatedAt:  now,
	}
	fieldAssessments := make([]FieldAssessment, 0, len(noteType.Fields))
	relationAssessments := make([]RelationAssessment, 0, len(noteType.Fields))
	relationByName := make(map[string]*RelationAssessment, len(noteType.Fields))
	structuralPairs := make(map[string]struct{})
	for _, field := range noteType.Fields {
		switch field.Kind {
		case ontoschema.FieldKindLink:
			fieldAssessment, relationAssessment, fieldEdges, fieldIssues := assessLinkField(&resolvedDoc, field, cache, resolveType, schema, now)
			fieldAssessments = append(fieldAssessments, fieldAssessment)
			relationAssessments = append(relationAssessments, relationAssessment)
			relationByName[field.Name] = &relationAssessments[len(relationAssessments)-1]
			res.edges = append(res.edges, fieldEdges...)
			res.issues = append(res.issues, fieldIssues...)
			for _, edge := range fieldEdges {
				structuralPairs[edge.SrcPath+"\x00"+edge.DstPath] = struct{}{}
			}
		case ontoschema.FieldKindNeighbor, ontoschema.FieldKindReverse:
			fieldAssessments = append(fieldAssessments, emptyFieldAssessment(field))
			relationAssessments = append(relationAssessments, emptyRelationAssessment(field))
			relationByName[field.Name] = &relationAssessments[len(relationAssessments)-1]
		case ontoschema.FieldKindSection:
			fieldAssessment, fieldIssues := assessSectionField(&resolvedDoc, noteType, field, schema)
			fieldAssessments = append(fieldAssessments, fieldAssessment)
			res.issues = append(res.issues, fieldIssues...)
		default:
			fieldAssessment, fieldIssues := assessScalarField(&resolvedDoc, noteType, field, schema)
			fieldAssessments = append(fieldAssessments, fieldAssessment)
			res.issues = append(res.issues, fieldIssues...)
		}
	}
	ambientEdges := buildAmbientEdgesForSync(job.doc.Path, assessment.ResolvedType, incidentRows, structuralPairs, resolveType, schema, now)
	res.edges = append(res.edges, ambientEdges...)
	for _, edge := range res.edges {
		relation := relationByName[edge.RelationName]
		if relation == nil {
			continue
		}
		target := RelationTarget{
			Path:       edge.DstPath,
			TypeName:   edge.DstType,
			Provenance: edge.Provenance,
			Structural: edge.Structural,
		}
		if relationHasTarget(relation.Targets, edge) {
			continue
		}
		relation.Present = true
		relation.Targets = append(relation.Targets, target)
	}
	assessment.Fields = fieldAssessments
	assessment.Relations = relationAssessments
	customIssues := semanticValidationIssues(&resolvedDoc, noteType, &assessment, schema)
	res.issues = append(res.issues, customIssues...)
	assessment.Issues = append(assessment.Issues, res.issues...)
	sortValidationIssues(assessment.Issues)
	res.relationCnt = len(res.edges)
	setAssessmentRow(res.row, assessment)
	return res, nil
}
