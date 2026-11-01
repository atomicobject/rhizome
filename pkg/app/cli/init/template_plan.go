package init

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// inferTemplateChoices preserves the user's previously applied workflow bundles
// on reruns. Prefer explicit config state; fall back to legacy scaffold inference
// so older repos migrate forward automatically.
//
// Docs: [[init-starter-workflow#^spec-0038-us3]]
func inferTemplateChoices(projectRoot string, localCfg *repoconfig.LocalConfig) ([]string, error) {
	if localCfg != nil && len(localCfg.WorkflowTemplates) > 0 {
		return normalizeTemplateNames(localCfg.WorkflowTemplates)
	}
	if projectRoot == "" {
		return nil, nil
	}
	// A recorded workflow state with no templates is a choice of no workflow;
	// starter docs left on disk must not bring the workflow back.
	if _, err := os.Stat(filepath.Join(projectRoot, ".rhizome", "workflows.yml")); err == nil {
		return nil, nil
	}
	candidates := map[string][]string{
		templateAgenticEngineering: {
			filepath.Join(projectRoot, "docs", "engineering", "testing-policy.md"),
			filepath.Join(projectRoot, ".agents", "skills", templateAgenticEngineering, "SKILL.md"),
			filepath.Join(projectRoot, ".claude", "skills", templateAgenticEngineering, "SKILL.md"),
			filepath.Join(projectRoot, "docs", "specs", "process", "development-loop.md"),
			filepath.Join(projectRoot, ".agents", "skills", "specify", "SKILL.md"),
			filepath.Join(projectRoot, ".claude", "skills", "specify", "SKILL.md"),
		},
		templateComplexDomain: {
			filepath.Join(projectRoot, ".agents", "skills", "complex-domain", "SKILL.md"),
			filepath.Join(projectRoot, ".claude", "skills", "complex-domain", "SKILL.md"),
			filepath.Join(projectRoot, ".rhizome", "ontology", "complex-domain.graphql"),
			filepath.Join(projectRoot, filepath.FromSlash(assetroots.ModuleDir(templateComplexDomain, assetroots.Ontology)), "complex-domain.graphql"),
		},
	}
	var detected []string
	for _, template := range []string{templateAgenticEngineering, templateComplexDomain} {
		for _, candidate := range candidates[template] {
			if _, err := os.Stat(candidate); err == nil {
				detected = append(detected, template)
				break
			}
		}
	}
	if len(detected) == 0 {
		return nil, nil
	}
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return nil, err
	}
	detected, err = normalizeTemplateNamesWithRegistry(detected, registry)
	if err != nil {
		return nil, err
	}
	return removeInferredTemplateDependencies(detected, registry), nil
}

func removeInferredTemplateDependencies(detected []string, registry map[string]starterTemplateMetadata) []string {
	if len(detected) < 2 {
		return detected
	}
	detectedSet := stringSet(detected)
	requiredByDetected := map[string]bool{}
	var markRequires func(id string)
	markRequires = func(id string) {
		meta, ok := registry[id]
		if !ok {
			return
		}
		for _, dep := range meta.Requires {
			if requiredByDetected[dep] {
				continue
			}
			requiredByDetected[dep] = true
			markRequires(dep)
		}
	}
	for _, id := range detected {
		markRequires(id)
	}
	out := make([]string, 0, len(detected))
	for _, id := range detected {
		if detectedSet[id] && !requiredByDetected[id] {
			out = append(out, id)
		}
	}
	return out
}

// starterModuleFile is one file of a starter's module: where it installs
// and, for an asset kind with a flat folder, the flat path init installed it
// at before modules.
type starterModuleFile struct {
	target  string
	legacy  string
	kind    assetroots.Kind
	content []byte
}

// starterModuleFiles lists a starter's module files with their install
// paths under `.rhizome/modules/<id>/` ([[rhizome-modules#^SPEC-0130-US1]]).
func starterModuleFiles(template string) ([]starterModuleFile, error) {
	files, err := loadStarterModuleTemplates(template)
	if err != nil {
		return nil, err
	}
	out := make([]starterModuleFile, 0, len(files))
	for _, file := range files {
		f := starterModuleFile{target: assetroots.ModuleRoot(template) + "/" + file.Path, content: file.Content}
		if _, kind, sub, ok := assetroots.Classify(f.target); ok {
			f.kind = kind
			if flat := assetroots.FlatDir(kind); flat != "" {
				f.legacy = flat + "/" + sub
			}
		}
		out = append(out, f)
	}
	return out, nil
}

// keptFlatCopy is an edited flat copy of a starter file that init keeps, so
// it does not install the module copy that would duplicate it.
type keptFlatCopy struct {
	flat   string
	module string
}

// planTemplateScaffold adds every starter file outside the agent folders:
// starter docs (team-owned, created only) and each starter's module.
// Target path collisions across templates are rejected.
//
// Docs:
// - [[init-template-architecture#^spec-0039-us2-ac1]]
// - [[init-starter-workflow#^SPEC-0038-US3-AC2]]
// - [[rhizome-modules#^SPEC-0130-US2]]
func planTemplateScaffold(projectRoot string, templates []string, refreshDocs bool, record *generatedFiles, plan *filePlan) error {
	templates, err := normalizeTemplateNames(templates)
	if err != nil {
		return err
	}
	seen := map[string]string{}
	owner := map[string]string{}
	register := func(path string, content []byte, template string) (bool, error) {
		if existing, ok := seen[path]; ok {
			if existing == string(content) {
				return false, nil
			}
			// WHY: two starters writing different bytes to one path would make
			// ownership ambiguous.
			// Docs: [[init-template-architecture#^spec-0039-us2-ac1]]
			return false, fmt.Errorf("template %q collides with template %q at %s", template, owner[path], path)
		}
		seen[path] = string(content)
		owner[path] = template
		return true, nil
	}

	for _, template := range templates {
		moduleFiles, err := starterModuleFiles(template)
		if err != nil {
			return err
		}
		for _, file := range moduleFiles {
			fresh, err := register(file.target, file.content, template)
			if err != nil {
				return err
			}
			if !fresh {
				continue
			}
			fingerprint := contentFingerprint
			if file.kind == assetroots.Ontology {
				fingerprint = ontologyFingerprint
			}
			if file.legacy != "" {
				kept, err := planLegacyFlatCopy(projectRoot, file, fingerprint, record, plan)
				if err != nil {
					return err
				}
				if kept {
					continue
				}
			}
			generated := generatedFile{rel: file.target, content: file.content}
			switch file.kind {
			case assetroots.Ontology:
				if generated.content, err = ontologyWithInstalledIdentifiers(projectRoot, file.content, file.target, file.legacy); err != nil {
					return err
				}
				generated.fingerprint = ontologyFingerprint
			case assetroots.Scripts:
				generated.mode = 0o755
			}
			plan.addFile(generated)
		}

		docs, err := loadStarterTemplates(template)
		if err != nil {
			return err
		}
		for _, file := range docs {
			target := filepath.ToSlash(file.Path)
			fresh, err := register(target, file.Content, template)
			if err != nil {
				return err
			}
			if fresh {
				plan.addFile(generatedFile{rel: target, content: file.Content, teamOwned: true, refresh: refreshDocs})
			}
		}
	}
	return nil
}

// planLegacyFlatCopy handles the flat copy of a module file that init
// installed before modules. An unedited copy is removed once the module copy
// is in place. An edited copy, or one init has no record of writing, stays,
// and kept reports that the module copy must not be installed beside it.
//
// Docs: [[rhizome-modules#^SPEC-0130-US2]]
func planLegacyFlatCopy(projectRoot string, file starterModuleFile, fingerprint func([]byte) string, record *generatedFiles, plan *filePlan) (kept bool, err error) {
	current, exists, err := readIfExists(filepath.Join(projectRoot, filepath.FromSlash(file.legacy)))
	if err != nil {
		return false, err
	}
	written, recorded := record.Written[file.legacy]
	if !exists {
		// Someone moved the flat copy to the module path, as the kept-copy
		// report suggests: its record moves with it, so its edits still count
		// as edits.
		if _, targetRecorded := record.Written[file.target]; recorded && !targetRecorded {
			record.Written[file.target] = written
		}
		return false, nil
	}
	if recorded && fingerprint(current) == written {
		plan.removals = append(plan.removals, generatedRemoval{rel: file.legacy, requires: file.target, fingerprint: fingerprint})
		return false, nil
	}
	plan.keptFlat = append(plan.keptFlat, keptFlatCopy{flat: file.legacy, module: file.target})
	return true, nil
}

// planRemovedStarterFiles removes the module files, other than the schema,
// of starters that are neither active nor unlinked, and the flat copies init
// installed before modules, when the record shows Rhizome wrote them.
// Team-owned docs stay, and so does the schema, because existing notes may
// still use its types.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US7]]
func planRemovedStarterFiles(templates, unlinked []string, record *generatedFiles, plan *filePlan) error {
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return err
	}
	keep := stringSet(normalizeMetadataIDsWithCanonicalIDs(append(append([]string{}, templates...), frozenStarters(unlinked, nil)...)))
	planned := map[string]bool{}
	for _, file := range plan.files {
		planned[file.rel] = true
	}
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if keep[id] {
			continue
		}
		files, err := starterModuleFiles(id)
		if err != nil {
			return err
		}
		for _, file := range files {
			if file.kind == assetroots.Ontology {
				continue
			}
			for _, target := range []string{file.target, file.legacy} {
				if _, recorded := record.Written[target]; target == "" || !recorded || planned[target] {
					continue
				}
				plan.removals = append(plan.removals, generatedRemoval{rel: target})
			}
		}
	}
	return nil
}

// ontologyWithInstalledIdentifiers carries an installed schema's identifier
// allocation contracts into the new version, because init may refresh a
// schema but never rekey an identifier pool. The installed schema is the
// first of rels that exists, so a schema moving into its module keeps the
// contracts of its flat copy.
func ontologyWithInstalledIdentifiers(projectRoot string, upstream []byte, rels ...string) ([]byte, error) {
	for _, rel := range rels {
		if rel == "" {
			continue
		}
		existing, exists, err := readIfExists(filepath.Join(projectRoot, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		if _, err := parseIdentifierAllocationContracts("installed ontology", existing); err != nil {
			// Not a schema Rhizome can read, so there is no contract to carry;
			// the ownership rule decides what happens to the file.
			return upstream, nil
		}
		content, _, err := preserveAdoptedIdentifierStrategies(existing, upstream)
		if err != nil {
			return nil, fmt.Errorf("preserve adopted identifier strategies in %s: %w", rel, err)
		}
		return content, nil
	}
	return upstream, nil
}

// frozenStarters lists ejected starters and the starters they require, minus
// the managed ones. Their files stay exactly as they are, so ejecting never
// removes files an ejected starter relies on.
func frozenStarters(ejected, managed []string) []string {
	ids := normalizeMetadataIDsWithCanonicalIDs(ejected)
	if registry, err := loadStarterTemplateMetadataRegistry(); err == nil {
		if closure, err := resolveTemplateSet(ids, repoconfig.WorkflowTemplateAddons{}, registry); err == nil {
			ids = closure.Effective
		}
	}
	active := stringSet(managed)
	var out []string
	for _, id := range ids {
		if !active[id] {
			out = append(out, id)
		}
	}
	return out
}
