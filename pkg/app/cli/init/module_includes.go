package init

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/fileio"
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
	if slices.Equal(includes, cfg.Notes.Includes) {
		return refreshModuleGuidance(root, *cfg)
	}
	cfg.Notes.Includes = includes
	if err := repoconfig.SaveLocalConfig(root, *cfg); err != nil {
		return err
	}
	return refreshModuleGuidance(root, *cfg)
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

func refreshModuleGuidance(root string, cfg repoconfig.LocalConfig) error {
	section, err := renderRhizomeMd(cfg)
	if err != nil {
		return err
	}
	record, _ := loadGeneratedFiles(root)
	changed := false
	for _, rel := range []string{"AGENTS.md", "CLAUDE.md"} {
		if rel == "CLAUDE.md" && (claudeDocIncludesAgentsMd(filepath.Join(root, rel)) || sameFile(filepath.Join(root, "AGENTS.md"), filepath.Join(root, rel))) {
			continue
		}
		file := filepath.Join(root, rel)
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err = checkManagedFences(rel, string(data)); err != nil {
			return err
		}
		body, found := managedBlockBody(string(data), "rhizome")
		if !found {
			continue
		}
		key := rel + "#rhizome"
		if previous, recorded := record.Written[key]; recorded && previous != contentFingerprint([]byte(body)) {
			continue
		}
		next := replaceDelimitedBlock(string(data), managedRhizomeBlockStart, managedRhizomeBlockEnd, buildManagedRhizomeBlock(section))
		if next == string(data) {
			continue
		}
		if err = fileio.WriteFileAtomicPreservingMode(file, []byte(next), 0644); err != nil {
			return err
		}
		record.recordWritten(key, contentFingerprint([]byte(strings.TrimSpace(section))))
		changed = true
	}
	if changed {
		return record.save(root)
	}
	return nil
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
