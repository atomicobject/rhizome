package init

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// EnsureModuleSourceIncludes adds exact source paths without replacing a person's
// other include rules. LocalConfig's patch writer preserves unrelated values.
func EnsureModuleSourceIncludes(ctx context.Context, root string, sources []string, formats noteformat.Registry) error {
	if len(sources) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	cfg, err := repoconfig.LoadLocalConfig(root)
	if err != nil {
		return err
	}
	if cfg == nil {
		return fmt.Errorf("repository configuration is missing; run rzm init")
	}
	includes := append([]string(nil), cfg.Notes.Includes...)
	if len(includes) == 0 {
		includes = []string{"**/*.md"}
	}
	for _, p := range sources {
		if p == "" || p == "." || filepath.IsAbs(p) || filepath.ToSlash(filepath.Clean(p)) != p || p == ".." || strings.HasPrefix(p, "../") || strings.ContainsAny(p, `\*?[]`) {
			return fmt.Errorf("module source must be an exact repository-relative path: %q", p)
		}
		if err := checkModuleSourceLocation(root, p); err != nil {
			return err
		}
		if !moduleSourceIncluded(p, includes, formats) {
			includes = append(includes, p)
		}
	}
	changed := !slices.Equal(includes, cfg.Notes.Includes)
	cfg.Notes.Includes = includes
	guidance, err := prepareModuleGuidance(root, *cfg)
	if err != nil {
		return err
	}
	if changed {
		if err := repoconfig.SaveLocalConfig(root, *cfg); err != nil {
			return err
		}
	}
	_, err = guidance.apply(nil)
	return err
}

func checkModuleSourceLocation(root, source string) error {
	for candidate := filepath.Join(root, filepath.FromSlash(source)); ; candidate = filepath.Dir(candidate) {
		_, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return err
		}
		if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
			return fmt.Errorf("module source escapes repository: %s", source)
		}
		return nil
	}
}

func prepareModuleGuidance(root string, cfg repoconfig.LocalConfig) (*preparedFiles, error) {
	section, err := renderRhizomeMd(cfg)
	if err != nil {
		return nil, err
	}
	record, _ := loadGeneratedFiles(root)
	plan := &filePlan{}
	for _, rel := range []string{"AGENTS.md", "CLAUDE.md"} {
		file := filepath.Join(root, rel)
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err = checkManagedFences(rel, string(data)); err != nil {
			return nil, err
		}
		if rel == "CLAUDE.md" && (claudeDocIncludesAgentsMd(file) || sameFile(filepath.Join(root, "AGENTS.md"), file)) {
			continue
		}
		if _, found := managedBlockBody(string(data), "rhizome"); !found {
			continue
		}
		plan.docs = append(plan.docs, generatedDoc{
			rel: rel, blocks: []managedDocBlock{{key: "rhizome", content: section}},
			preserve: staleTemplateBlocks(string(data), generatedDoc{}),
		})
	}
	return classifyFilePlan(root, plan, record)
}

func moduleSourceIncluded(source string, includes []string, formats noteformat.Registry) bool {
	provider, found := formats.ProviderForPath(paths.RelPath(source))
	if !found {
		return false
	}
	descriptor := provider.Descriptor()
	for _, pattern := range includes {
		hiddenCovered := true
		for _, part := range strings.Split(source, "/") {
			if strings.HasPrefix(part, ".") && !strings.Contains(pattern, part+"/") {
				hiddenCovered = false
			}
		}
		if !hiddenCovered {
			continue
		}
		if descriptor.OwnershipPolicy == noteformat.OwnershipDefault && pathMatchesAnyGlob(source, []string{pattern}) {
			return true
		}
		if notediscovery.MatchesExplicitInclude(pattern, paths.RelPath(source), descriptor.Extensions) {
			return true
		}
	}
	return false
}
