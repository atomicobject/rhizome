package init

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

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

// planTemplateScaffold adds every starter file outside the agent folders:
// starter docs (team-owned, created only), ontology schemas, saved queries,
// and views. Target path collisions across templates are rejected.
//
// Docs:
// - [[init-template-architecture#^spec-0039-us2-ac1]]
// - [[init-starter-workflow#^SPEC-0038-US3-AC2]]
func planTemplateScaffold(projectRoot string, templates []string, refreshDocs bool, plan *filePlan) error {
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
		ontologyFiles, err := loadStarterOntologyTemplates(template)
		if err != nil {
			return err
		}
		for _, file := range ontologyFiles {
			// WHY: each template names its schema after itself so several
			// starters land distinct .graphql files that the loader merges.
			target := filepath.ToSlash(filepath.Join(".rhizome", "ontology", file.Path))
			fresh, err := register(target, file.Content, template)
			if err != nil {
				return err
			}
			if !fresh {
				continue
			}
			content, err := ontologyWithInstalledIdentifiers(projectRoot, target, file.Content)
			if err != nil {
				return err
			}
			plan.addFile(generatedFile{rel: target, content: content, fingerprint: ontologyFingerprint})
		}
		recipeFiles, err := loadStarterQueryRecipeTemplates(template)
		if err != nil {
			return err
		}
		viewFiles, err := loadStarterViewTemplates(template)
		if err != nil {
			return err
		}
		for _, group := range []struct {
			dir   string
			files []starterTemplateFile
		}{{".rhizome/query-recipes", recipeFiles}, {".rhizome/views", viewFiles}} {
			for _, file := range group.files {
				target := group.dir + "/" + filepath.ToSlash(file.Path)
				fresh, err := register(target, file.Content, template)
				if err != nil {
					return err
				}
				if fresh {
					plan.addFile(generatedFile{rel: target, content: file.Content})
				}
			}
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

// planRemovedStarterFiles removes the saved queries and views of starters
// that are neither active nor unlinked, when the record shows Rhizome wrote
// them. Team-owned docs stay, and so does the schema, because existing notes
// may still use its types.
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
		recipes, err := loadStarterQueryRecipeTemplates(id)
		if err != nil {
			return err
		}
		views, err := loadStarterViewTemplates(id)
		if err != nil {
			return err
		}
		for _, group := range []struct {
			dir   string
			files []starterTemplateFile
		}{{".rhizome/query-recipes", recipes}, {".rhizome/views", views}} {
			for _, file := range group.files {
				target := group.dir + "/" + filepath.ToSlash(file.Path)
				if _, recorded := record.Written[target]; !recorded || planned[target] {
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
// schema but never rekey an identifier pool.
func ontologyWithInstalledIdentifiers(projectRoot, rel string, upstream []byte) ([]byte, error) {
	existing, exists, err := readIfExists(filepath.Join(projectRoot, filepath.FromSlash(rel)))
	if err != nil || !exists {
		return upstream, err
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
