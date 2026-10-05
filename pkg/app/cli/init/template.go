package init

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"gopkg.in/yaml.v3"
)

const (
	templateCore        = "core"
	templateActionItems = "action-items"
	// Retained as a migration marker for repositories that already selected it.
	templateProjectKB          = "project-kb"
	templateAgenticEngineering = "agentic-engineering"
	templateComplexDomain      = "complex-domain"
	legacyTemplateSpecDriven   = "spec-driven"
)

type templateCause string

const (
	templateCauseExplicit     templateCause = "explicit"
	templateCauseRequired     templateCause = "required"
	templateCauseDefaultAddon templateCause = "default-addon"
)

type starterTemplateMetadata struct {
	ID                  string   `yaml:"id"`
	Name                string   `yaml:"name,omitempty"`
	Requires            []string `yaml:"requires,omitempty"`
	ActivatesByDefault  []string `yaml:"activatesByDefault,omitempty"`
	InstalledAssetTypes []string `yaml:"installedAssetTypes,omitempty"`
}

type resolvedTemplateSet struct {
	Explicit  []string
	Effective []string
	Causes    map[string]templateCause
}

func normalizeTemplateList(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "none") {
		return nil, nil
	}
	return normalizeTemplateNames(strings.Split(trimmed, ","))
}

func normalizeTemplateNames(names []string) ([]string, error) {
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return nil, err
	}
	return normalizeTemplateNamesWithRegistry(names, registry)
}

func normalizeTemplateNamesWithRegistry(names []string, registry map[string]starterTemplateMetadata) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, raw := range names {
		tpl := canonicalTemplateID(raw)
		switch tpl {
		case "", "none":
			continue
		default:
			if _, ok := registry[tpl]; !ok {
				if tpl == templateProjectKB {
					return nil, fmt.Errorf("project-kb starter has been removed; choose a supported --workflow or update .rhizome/workflows.yml")
				}
				return nil, fmt.Errorf("unknown template %q", raw)
			}
		}
		if _, ok := seen[tpl]; ok {
			continue
		}
		seen[tpl] = struct{}{}
		out = append(out, tpl)
	}
	sort.Strings(out)
	return out, nil
}

// canonicalTemplateID accepts the retired spec-driven identifier only at
// migration boundaries. Callers always persist the returned canonical id.
func canonicalTemplateID(raw string) string {
	id := strings.ToLower(strings.TrimSpace(raw))
	if id == legacyTemplateSpecDriven {
		return templateAgenticEngineering
	}
	return id
}

func loadStarterTemplateMetadataRegistry() (map[string]starterTemplateMetadata, error) {
	root := path.Join("templates", "starters")
	entries, err := fs.ReadDir(agentHelperTemplates, root)
	if err != nil {
		if errorsIsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list starter templates under %q: %w", root, err)
	}
	registry := make(map[string]starterTemplateMetadata, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		metaPath := path.Join(root, id, "template.yaml")
		content, err := fs.ReadFile(agentHelperTemplates, metaPath)
		if err != nil {
			if errorsIsNotExist(err) {
				registry[id] = starterTemplateMetadata{ID: id}
				continue
			}
			return nil, fmt.Errorf("read starter metadata %q: %w", metaPath, err)
		}
		var meta starterTemplateMetadata
		if err := yaml.Unmarshal(content, &meta); err != nil {
			return nil, fmt.Errorf("parse starter metadata %q: %w", metaPath, err)
		}
		meta.ID = strings.ToLower(strings.TrimSpace(firstNonEmpty(meta.ID, id)))
		if meta.ID != id {
			return nil, fmt.Errorf("starter metadata %q declares id %q", metaPath, meta.ID)
		}
		meta.Requires = normalizeMetadataIDs(meta.Requires)
		meta.ActivatesByDefault = normalizeMetadataIDs(meta.ActivatesByDefault)
		if _, ok := registry[meta.ID]; ok {
			return nil, fmt.Errorf("duplicate starter template id %q", meta.ID)
		}
		registry[meta.ID] = meta
	}
	return registry, nil
}

func errorsIsNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeMetadataIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := canonicalTemplateID(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func resolveTemplateSet(explicit []string, addons obsidian.WorkflowTemplateAddons, registry map[string]starterTemplateMetadata) (resolvedTemplateSet, error) {
	explicit, err := normalizeTemplateNamesWithRegistry(explicit, registry)
	if err != nil {
		return resolvedTemplateSet{}, err
	}
	disabled := stringSet(normalizeMetadataIDs(addons.Disabled))
	enabled := normalizeMetadataIDs(addons.Enabled)
	for _, id := range enabled {
		if disabled[id] {
			return resolvedTemplateSet{}, fmt.Errorf("template addon %q cannot be both enabled and disabled", id)
		}
	}
	causes := map[string]templateCause{}
	var effective []string
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var add func(id string, cause templateCause) error
	add = func(id string, cause templateCause) error {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			return nil
		}
		meta, ok := registry[id]
		if !ok {
			return fmt.Errorf("unknown template %q", id)
		}
		if disabled[id] && cause == templateCauseRequired {
			return fmt.Errorf("required dependency %q cannot be disabled", id)
		}
		if disabled[id] && cause == templateCauseDefaultAddon {
			return nil
		}
		if visiting[id] {
			return fmt.Errorf("template dependency cycle involving %q", id)
		}
		if visited[id] {
			if causes[id] != templateCauseExplicit && cause == templateCauseExplicit {
				causes[id] = templateCauseExplicit
			}
			return nil
		}
		visiting[id] = true
		for _, dep := range meta.Requires {
			if err := add(dep, templateCauseRequired); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		causes[id] = cause
		effective = append(effective, id)
		return nil
	}
	for _, id := range explicit {
		if disabled[id] {
			return resolvedTemplateSet{}, fmt.Errorf("explicit template %q cannot be disabled", id)
		}
		if err := add(id, templateCauseExplicit); err != nil {
			return resolvedTemplateSet{}, err
		}
	}
	for _, id := range enabled {
		if err := add(id, templateCauseExplicit); err != nil {
			return resolvedTemplateSet{}, err
		}
	}
	for i := 0; i < len(effective); i++ {
		meta := registry[effective[i]]
		for _, addon := range meta.ActivatesByDefault {
			if err := add(addon, templateCauseDefaultAddon); err != nil {
				return resolvedTemplateSet{}, err
			}
		}
	}
	return resolvedTemplateSet{
		Explicit:  cloneTemplates(explicit),
		Effective: effective,
		Causes:    causes,
	}, nil
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func sortedStringSet(values map[string]bool) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func cloneTemplates(templates []string) []string {
	if len(templates) == 0 {
		return nil
	}
	return append([]string(nil), templates...)
}

func resolveEffectiveTemplatesWithOptions(explicit []string, localCfg *obsidian.LocalConfig, suppressDefaultAddons bool) ([]string, error) {
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return nil, err
	}
	addons := obsidian.WorkflowTemplateAddons{}
	if localCfg != nil {
		addons = localCfg.WorkflowTemplateAddons
	}
	if suppressDefaultAddons {
		addons.Disabled = append(cloneTemplates(addons.Disabled), defaultAddonsForTemplates(explicit, registry)...)
	}
	management := obsidian.WorkflowTemplateManagement{}
	if localCfg != nil {
		management = localCfg.WorkflowTemplateManagement
	}
	resolved, err := resolveManagedTemplateSet(explicit, addons, management, registry)
	if err != nil {
		return nil, err
	}
	return resolved.Effective, nil
}

func persistDefaultWorkflowTemplateAddons(localCfg *obsidian.LocalConfig, explicit []string, suppressDefaultAddons bool) (bool, error) {
	if localCfg == nil || suppressDefaultAddons {
		return false, nil
	}
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return false, err
	}
	defaults := defaultAddonsForTemplates(explicit, registry)
	if len(defaults) == 0 {
		return false, nil
	}
	before := localCfg.WorkflowTemplateAddons
	disabled := stringSet(normalizeMetadataIDs(localCfg.WorkflowTemplateAddons.Disabled))
	enabled := stringSet(normalizeMetadataIDs(localCfg.WorkflowTemplateAddons.Enabled))
	ejected := stringSet(normalizeMetadataIDs(localCfg.WorkflowTemplateManagement.Ejected))
	for _, id := range defaults {
		if disabled[id] || ejected[id] {
			continue
		}
		enabled[id] = true
	}
	localCfg.WorkflowTemplateAddons.Enabled = sortedStringSet(enabled)
	localCfg.WorkflowTemplateAddons.Disabled = normalizeMetadataIDs(localCfg.WorkflowTemplateAddons.Disabled)
	return !reflect.DeepEqual(before, localCfg.WorkflowTemplateAddons), nil
}

func reconcileExplicitWorkflowTemplateAddons(localCfg *obsidian.LocalConfig, explicit []string) bool {
	if localCfg == nil || len(explicit) == 0 {
		return false
	}
	before := localCfg.WorkflowTemplateAddons
	explicitSet := stringSet(normalizeMetadataIDs(explicit))
	localCfg.WorkflowTemplateAddons.Enabled = removeTemplateIDs(localCfg.WorkflowTemplateAddons.Enabled, explicitSet)
	localCfg.WorkflowTemplateAddons.Disabled = removeTemplateIDs(localCfg.WorkflowTemplateAddons.Disabled, explicitSet)
	return !reflect.DeepEqual(before, localCfg.WorkflowTemplateAddons)
}

func removeTemplateIDs(values []string, remove map[string]bool) []string {
	if len(values) == 0 || len(remove) == 0 {
		return normalizeMetadataIDs(values)
	}
	var out []string
	for _, value := range normalizeMetadataIDs(values) {
		if !remove[value] {
			out = append(out, value)
		}
	}
	return out
}

func defaultAddonsForTemplates(explicit []string, registry map[string]starterTemplateMetadata) []string {
	if len(explicit) == 0 {
		return nil
	}
	explicitSet := stringSet(normalizeMetadataIDs(explicit))
	seen := map[string]bool{}
	var out []string
	for _, id := range explicit {
		meta, ok := registry[id]
		if !ok {
			continue
		}
		for _, addon := range meta.ActivatesByDefault {
			if explicitSet[addon] {
				continue
			}
			if seen[addon] {
				continue
			}
			seen[addon] = true
			out = append(out, addon)
		}
	}
	sort.Strings(out)
	return out
}

func suppressDefaultAddonsForExistingConfig(layout DetectedLayout, localCfg obsidian.LocalConfig) bool {
	return layout.HasExistingConfig &&
		len(layout.ExistingLocal.WorkflowTemplates) > 0 &&
		!layout.HasWorkflowTemplateAddons &&
		isEmptyWorkflowTemplateAddons(localCfg.WorkflowTemplateAddons)
}

// inferTemplateChoices preserves the user's previously applied workflow bundles
// on reruns. Prefer explicit config state; fall back to legacy scaffold inference
// so older repos migrate forward automatically.
//
// Docs: [[init-starter-workflow#^spec-0038-us3]]
func inferTemplateChoices(projectRoot string, localCfg *obsidian.LocalConfig) ([]string, error) {
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
		if closure, err := resolveTemplateSet(ids, obsidian.WorkflowTemplateAddons{}, registry); err == nil {
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
