package init

import (
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type resolvedManagedTemplateSet struct {
	resolvedTemplateSet
	Ejected []string
}

type StarterEjectionPlan struct {
	Target                string
	Cascade               bool
	Blocked               bool
	BlockingDependents    []string
	Ejected               []string
	DefaultAddonEjections []string
	RemainingManaged      []string
}

func resolveManagedTemplateSet(explicit []string, addons obsidian.WorkflowTemplateAddons, management obsidian.WorkflowTemplateManagement, registry map[string]starterTemplateMetadata) (resolvedManagedTemplateSet, error) {
	explicit, err := normalizeTemplateNamesWithRegistry(explicit, registry)
	if err != nil {
		return resolvedManagedTemplateSet{}, err
	}
	ejected := stringSet(normalizeMetadataIDs(management.Ejected))
	managedExplicit := removeTemplateIDs(explicit, ejected)
	managedAddons := obsidian.WorkflowTemplateAddons{
		Enabled:  removeTemplateIDs(addons.Enabled, ejected),
		Disabled: normalizeMetadataIDs(addons.Disabled),
	}
	resolved, err := resolveTemplateSet(managedExplicit, managedAddons, registry)
	if err != nil {
		return resolvedManagedTemplateSet{}, err
	}
	for _, id := range resolved.Effective {
		if ejected[id] {
			return resolvedManagedTemplateSet{}, fmt.Errorf("ejected template %q is required by a managed starter", id)
		}
	}
	return resolvedManagedTemplateSet{
		resolvedTemplateSet: resolved,
		Ejected:             sortedStringSet(ejected),
	}, nil
}

func planStarterEjectionWithRegistry(cfg obsidian.LocalConfig, target string, cascade bool, registry map[string]starterTemplateMetadata) (StarterEjectionPlan, error) {
	targets, err := normalizeTemplateNamesWithRegistry([]string{target}, registry)
	if err != nil {
		return StarterEjectionPlan{}, err
	}
	if len(targets) != 1 {
		return StarterEjectionPlan{}, fmt.Errorf("starter ejection requires a starter id")
	}
	target = targets[0]

	current, err := resolveManagedTemplateSet(cfg.WorkflowTemplates, cfg.WorkflowTemplateAddons, cfg.WorkflowTemplateManagement, registry)
	if err != nil {
		return StarterEjectionPlan{}, err
	}
	currentSet := stringSet(current.Effective)
	if !currentSet[target] {
		if stringSet(current.Ejected)[target] {
			return StarterEjectionPlan{
				Target:           target,
				Cascade:          cascade,
				Ejected:          current.Ejected,
				RemainingManaged: current.Effective,
			}, nil
		}
		return StarterEjectionPlan{}, fmt.Errorf("starter %q is not currently managed", target)
	}

	dependents := managedDependentsOf(target, current.Effective, registry)
	if len(dependents) > 0 && !cascade {
		return StarterEjectionPlan{
			Target:             target,
			Cascade:            false,
			Blocked:            true,
			BlockingDependents: dependents,
			Ejected:            current.Ejected,
			RemainingManaged:   current.Effective,
		}, nil
	}

	ejected := stringSet(current.Ejected)
	ejected[target] = true
	for _, dependent := range dependents {
		ejected[dependent] = true
	}

	defaultAddons := defaultAddonEjections(ejected, currentSet, cfg.WorkflowTemplates, registry)
	for _, addon := range defaultAddons {
		ejected[addon] = true
	}

	nextCfg := cfg
	nextCfg.WorkflowTemplateManagement.Ejected = sortedStringSet(ejected)
	next, err := resolveManagedTemplateSet(nextCfg.WorkflowTemplates, nextCfg.WorkflowTemplateAddons, nextCfg.WorkflowTemplateManagement, registry)
	if err != nil {
		return StarterEjectionPlan{}, err
	}

	return StarterEjectionPlan{
		Target:                target,
		Cascade:               cascade,
		Ejected:               next.Ejected,
		DefaultAddonEjections: defaultAddons,
		RemainingManaged:      next.Effective,
	}, nil
}

func applyStarterEjectionPlan(cfg *obsidian.LocalConfig, plan StarterEjectionPlan) bool {
	if cfg == nil || plan.Blocked {
		return false
	}
	before := cfg.WorkflowTemplateManagement.Ejected
	cfg.WorkflowTemplateManagement.Ejected = cloneTemplates(plan.Ejected)
	return !reflect.DeepEqual(before, cfg.WorkflowTemplateManagement.Ejected)
}

func restoreStarterManagement(cfg *obsidian.LocalConfig, starter string) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return false, err
	}
	starters, err := normalizeTemplateNamesWithRegistry([]string{starter}, registry)
	if err != nil {
		return false, err
	}
	if len(starters) != 1 {
		return false, fmt.Errorf("restore management requires a starter id")
	}
	remove := map[string]bool{starters[0]: true}
	before := cfg.WorkflowTemplateManagement.Ejected
	cfg.WorkflowTemplateManagement.Ejected = removeTemplateIDs(cfg.WorkflowTemplateManagement.Ejected, remove)
	return !reflect.DeepEqual(before, cfg.WorkflowTemplateManagement.Ejected), nil
}

func applyStarterManagementOptions(cfg *obsidian.LocalConfig, opts RunOptions, out io.Writer) (changeSet, error) {
	changes := changeSet{}
	if cfg == nil {
		return changes, nil
	}
	if strings.TrimSpace(opts.Eject) != "" && strings.TrimSpace(opts.Restore) != "" {
		return changes, fmt.Errorf("--eject and --restore cannot be combined")
	}
	if targets := splitList(opts.Eject); len(targets) > 0 {
		registry, err := loadStarterTemplateMetadataRegistry()
		if err != nil {
			return changes, err
		}
		requested, err := normalizeTemplateNamesWithRegistry(targets, registry)
		if err != nil {
			return changes, err
		}
		// A starter that others depend on may need its dependents ejected
		// first, so retry what fails until no request makes progress.
		pending := requested
		for len(pending) > 0 {
			var retry []string
			var lastErr error
			for _, target := range pending {
				plan, err := planStarterEjectionWithRegistry(*cfg, target, false, registry)
				if err == nil && plan.Blocked {
					// Naming every dependent in the same request ejects them together.
					var missing []string
					for _, dependent := range plan.BlockingDependents {
						if !contains(requested, dependent) {
							missing = append(missing, dependent)
						}
					}
					if len(missing) > 0 {
						return changes, fmt.Errorf("cannot eject %q while %s depend on it; eject them together, for example --eject %s", plan.Target, strings.Join(missing, ", "), strings.Join(append(append([]string{}, missing...), plan.Target), ","))
					}
					plan, err = planStarterEjectionWithRegistry(*cfg, target, true, registry)
				}
				if err != nil {
					retry, lastErr = append(retry, target), err
					continue
				}
				if applyStarterEjectionPlan(cfg, plan) {
					changes.mark(sectionWorkflowMgmt)
					if out != nil {
						fmt.Fprintf(out, "Ejected: %s (files stay; Rhizome stops updating them)\n", strings.Join(plan.Ejected, ", "))
					}
				}
			}
			if len(retry) == len(pending) {
				// What is left may no longer be managed because an ejected
				// starter was the only one that needed it; record it as
				// ejected so its files stay frozen too.
				current, err := resolveManagedTemplateSet(cfg.WorkflowTemplates, cfg.WorkflowTemplateAddons, cfg.WorkflowTemplateManagement, registry)
				if err != nil {
					return changes, err
				}
				managed := stringSet(current.Effective)
				for _, target := range retry {
					if managed[target] {
						return changes, lastErr
					}
				}
				cfg.WorkflowTemplateManagement.Ejected = sortedStringSet(stringSet(append(append([]string{}, cfg.WorkflowTemplateManagement.Ejected...), retry...)))
				changes.mark(sectionWorkflowMgmt)
				break
			}
			pending = retry
		}
	}
	if strings.TrimSpace(opts.Restore) != "" {
		changed, err := restoreStarterManagement(cfg, opts.Restore)
		if err != nil {
			return changes, err
		}
		if changed {
			changes.mark(sectionWorkflowMgmt)
			if out != nil {
				fmt.Fprintf(out, "Restored: %s (Rhizome updates it again)\n", strings.TrimSpace(opts.Restore))
			}
		}
	}
	return changes, nil
}

func managedDependentsOf(target string, managed []string, registry map[string]starterTemplateMetadata) []string {
	managedSet := stringSet(managed)
	var dependents []string
	for _, id := range managed {
		if id == target {
			continue
		}
		if templateRequires(id, target, managedSet, registry) {
			dependents = append(dependents, id)
		}
	}
	sort.Strings(dependents)
	return dependents
}

func templateRequires(id, target string, managed map[string]bool, registry map[string]starterTemplateMetadata) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(current string) bool {
		if seen[current] {
			return false
		}
		seen[current] = true
		meta, ok := registry[current]
		if !ok {
			return false
		}
		for _, dep := range meta.Requires {
			if dep == target {
				return true
			}
			if managed[dep] && walk(dep) {
				return true
			}
		}
		return false
	}
	return walk(id)
}

func defaultAddonEjections(ejected, currentManaged map[string]bool, explicit []string, registry map[string]starterTemplateMetadata) []string {
	explicitSet := stringSet(normalizeMetadataIDs(explicit))
	candidates := map[string]bool{}
	for starter := range ejected {
		meta, ok := registry[starter]
		if !ok {
			continue
		}
		for _, addon := range meta.ActivatesByDefault {
			if currentManaged[addon] && !explicitSet[addon] {
				candidates[addon] = true
			}
		}
	}

	var out []string
	for addon := range candidates {
		if hasManagedDefaultActivator(addon, ejected, currentManaged, registry) {
			continue
		}
		out = append(out, addon)
	}
	sort.Strings(out)
	return out
}

func hasManagedDefaultActivator(addon string, ejected, currentManaged map[string]bool, registry map[string]starterTemplateMetadata) bool {
	for starter := range currentManaged {
		if ejected[starter] {
			continue
		}
		meta, ok := registry[starter]
		if !ok {
			continue
		}
		for _, candidate := range meta.ActivatesByDefault {
			if candidate == addon {
				return true
			}
		}
	}
	return false
}
