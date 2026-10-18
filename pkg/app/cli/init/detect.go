package init

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"gopkg.in/yaml.v3"
)

// VaultKind identifies whether the suggestion is a classic vault path or a collection.
type VaultKind string

const (
	VaultKindClassic    VaultKind = "classic"
	VaultKindCollection VaultKind = "collection"
)

// VaultSuggestion describes the vault configuration derived from heuristics.
type VaultSuggestion struct {
	Kind     VaultKind
	Path     string   // classic vault root
	Root     string   // collection root
	Includes []string // collection include globs (relative to Root)
	Excludes []string
	Links    string
	Reason   string
}

// CodeSuggestion captures coderefs/codeanchor recommendations.
type CodeSuggestion struct {
	EnableCodeRefs bool
	Languages      []string
	Scan           []string
	Ignore         []string
	PythonRoots    []string
	PythonScan     []string
	PythonIgnore   []string
	GoRoots        []string
	GoScan         []string
	GoIgnore       []string
	TSRoots        []string
	TSScan         []string
	TSIgnore       []string
	CSharpRoots    []string
	CSharpScan     []string
	CSharpIgnore   []string
	PHPRoots       []string
	PHPScan        []string
	PHPIgnore      []string
	// FileCount is the number of source files detection saw.
	FileCount int
	// Files lists every file the walk saw, so setup can count notes against
	// the notes include patterns without walking the repository again.
	Files []string
}

type AgentHarnesses struct {
	HasCursor        bool
	HasClaude        bool
	HasCodex         bool
	HasAgentSkills   bool
	HasAgents        bool
	AgentsMdDisabled bool // true when agent files are turned off (--agents none)
}

// DetectedLayout is the aggregate heuristic output.
type DetectedLayout struct {
	WorkingDir  string
	ProjectRoot string
	GitRoot     string
	HasGit      bool

	AgentHarnesses AgentHarnesses

	HasExistingConfig         bool
	ExistingConfigPath        string
	ExistingLocal             repoconfig.LocalConfig
	HasWorkflowTemplateAddons bool
	HasLegacyWorkflowConfig   bool

	VaultCandidates []VaultSuggestion
	SuggestedVault  VaultSuggestion
	Code            CodeSuggestion

	// IgnoredRepoCandidates lists gitignored directories that look like real
	// nested repos or code roots, surfaced for an explicit inclusion decision
	// (SPEC-0064).
	IgnoredRepoCandidates []IgnoredRepoCandidate

	DocPatterns []string
}

// DetectLayout inspects the directory (preferring git root) for vault and code hints.
func DetectLayout(startDir string) (DetectedLayout, error) {
	var layout DetectedLayout

	absStart := filepath.Clean(filepath.FromSlash(paths.ResolveSymlinks(startDir).String()))
	if absStart == "" {
		return layout, errors.New("start directory required")
	}
	layout.WorkingDir = absStart
	layout.ProjectRoot = absStart

	if gitRoot, ok := findGitRoot(absStart); ok {
		layout.GitRoot = gitRoot
		layout.HasGit = true
		if absStart != gitRoot && !hasProjectMarkers(absStart) {
			layout.ProjectRoot = gitRoot
		}
	}

	if err := loadExistingConfig(&layout); err != nil {
		return layout, err
	}

	vaults := detectVaults(layout.ProjectRoot)
	if layout.HasExistingConfig {
		if current, ok := vaultSuggestionFromConfig(layout.ExistingLocal.Notes, layout.ProjectRoot); ok {
			vaults = appendUniqueVault(vaults, current)
		}
	}
	layout.VaultCandidates = vaults
	if len(vaults) > 0 {
		layout.SuggestedVault = vaults[0]
	} else {
		layout.SuggestedVault = defaultVaultSuggestion(layout.ProjectRoot)
	}

	layout.Code = detectCode(layout.ProjectRoot)
	layout.IgnoredRepoCandidates = detectIgnoredNestedRepos(layout.ProjectRoot, initIgnoreMatcher(layout.ProjectRoot))
	layout.DocPatterns = repoconfig.FileContextConfigDefaults.DocPatterns
	layout.AgentHarnesses = detectAgentHarnesses(layout.ProjectRoot)

	return layout, nil
}

func loadExistingConfig(layout *DetectedLayout) error {
	cfg, err := repoconfig.LoadLocalConfig(layout.ProjectRoot)
	if err != nil {
		if errors.Is(err, repoconfig.ErrNoLocalConfig) {
			return nil
		}
		return err
	}
	layout.HasExistingConfig = true
	layout.ExistingConfigPath = filepath.Join(layout.ProjectRoot, ".rhizome", "config.yml")
	layout.HasLegacyWorkflowConfig = configHasTopLevelKey(layout.ExistingConfigPath, "workflowTemplates") ||
		configHasTopLevelKey(layout.ExistingConfigPath, "workflowTemplateAddons") ||
		configHasTopLevelKey(layout.ExistingConfigPath, "workflowTemplateManagement")
	if !layout.HasLegacyWorkflowConfig && isV049Config(cfg.Rhizome.Version) {
		// Keep migrated v0.49 workflow baselines stable on later init runs. The
		// migration deliberately preserves their historical source hashes.
		layout.HasLegacyWorkflowConfig = workflowStateHasTopLevelKey(layout.ProjectRoot, "management")
	}
	if layout.HasLegacyWorkflowConfig {
		legacyCfg, legacyPresent, err := repoconfig.LoadLocalConfigForWorkflowMigration(layout.ProjectRoot)
		if err != nil {
			return err
		}
		if legacyPresent {
			cfg = legacyCfg
		}
	}
	layout.ExistingLocal = *cfg
	layout.HasWorkflowTemplateAddons = configHasTopLevelKey(layout.ExistingConfigPath, sectionWorkflowAddons) ||
		workflowStateHasTopLevelKey(layout.ProjectRoot, "addons")
	return nil
}

func isV049Config(value string) bool {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	return value == "0.49" || strings.HasPrefix(value, "0.49.")
}

func workflowStateHasTopLevelKey(root string, key string) bool {
	path := filepath.Join(root, ".rhizome", "workflows.yml")
	return configHasTopLevelKey(path, key)
}

func configHasTopLevelKey(path string, key string) bool {
	data, err := fileio.ReadFile(path)
	if err != nil {
		return false
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return false
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return false
	}
	mapping := root.Content[0]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return true
		}
	}
	return false
}

// detectVaults proposes vault definitions from common markers.
func detectVaults(root string) []VaultSuggestion {
	var suggestions []VaultSuggestion

	// Prefer an embedded Obsidian vault (root or subdir containing .obsidian)
	if vaultDir := findObsidianVault(root); vaultDir != "" {
		includes := []string{"**/*.md"}
		if rel := relativizeToProject(vaultDir, root); rel != "." {
			includes = []string{filepath.ToSlash(filepath.Join(rel, "**/*.md"))}
		}
		suggestions = append(suggestions, VaultSuggestion{
			Kind:     VaultKindCollection,
			Root:     root,
			Includes: includes,
			Links:    obsidian.LinkTypeBoth,
			Reason:   "Found .obsidian directory",
		})
	}

	// Common docs-like directories plus high-signal markdown directories discovered at the repo root.
	candidates := detectMarkdownDirCandidates(root)
	if len(candidates) > 1 {
		includes := make([]string, 0, len(candidates))
		for _, cand := range candidates {
			includes = append(includes, cand.include)
		}
		suggestions = append(suggestions, VaultSuggestion{
			Kind:     VaultKindCollection,
			Root:     root,
			Includes: includes,
			Links:    obsidian.LinkTypeBoth,
			Reason:   "Detected repo knowledge in " + joinCandidateNames(candidates),
		})
	}
	for _, cand := range candidates {
		suggestions = append(suggestions, VaultSuggestion{
			Kind:     VaultKindCollection,
			Root:     root,
			Includes: []string{cand.include},
			Links:    obsidian.LinkTypeBoth,
			Reason:   cand.reason,
		})
	}

	// If nothing matched but we have markdown at root, fall back to collection of all markdown.
	if len(suggestions) == 0 && hasMarkdown(root) {
		suggestions = append(suggestions, defaultVaultSuggestion(root))
	}

	return suggestions
}

func joinCandidateNames(candidates []markdownDirCandidate) string {
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, candidate.name)
	}
	return strings.Join(names, ", ")
}

func vaultSuggestionFromConfig(cfg repoconfig.LocalVaultConfig, projectRoot string) (VaultSuggestion, bool) {
	if len(cfg.Includes) > 0 || len(cfg.Excludes) > 0 || strings.TrimSpace(cfg.Links) != "" {
		includes := cfg.Includes
		if len(includes) == 0 {
			includes = []string{"**/*.md"}
		}
		return VaultSuggestion{
			Kind:     VaultKindCollection,
			Root:     projectRoot,
			Includes: includes,
			Excludes: cfg.Excludes,
			Links:    cfg.Links,
			Reason:   "Current config",
		}, true
	}

	return VaultSuggestion{}, false
}

func appendUniqueVault(list []VaultSuggestion, candidate VaultSuggestion) []VaultSuggestion {
	key := vaultSuggestionKey(candidate)
	for _, v := range list {
		if vaultSuggestionKey(v) == key {
			return list
		}
	}
	return append(list, candidate)
}

func vaultSuggestionKey(v VaultSuggestion) string {
	return string(v.Kind) + "|" + v.Path + "|" + v.Root + "|" +
		strings.Join(v.Includes, ",") + "|" + strings.Join(v.Excludes, ",") + "|" + v.Links
}

func findObsidianVault(root string) string {
	if hasDir(filepath.Join(root, ".obsidian")) {
		return root
	}
	found := ""
	matcher := initIgnoreMatcher(root)
	rootPaths, _ := paths.NewVaultPaths(root)
	walkRoot := root
	if rootPaths.Root() != "" {
		walkRoot = rootPaths.Root()
	}
	_ = filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".obsidian" {
			found = filepath.Dir(path)
			return fs.SkipAll
		}
		if d.IsDir() {
			if rel, relErr := rootPaths.RelStrict(path); relErr == nil {
				if shouldSkipDir(rel.String(), d.Name(), matcher) {
					return filepath.SkipDir
				}
			}
		}
		return nil
	})
	return found
}

func defaultVaultSuggestion(root string) VaultSuggestion {
	return VaultSuggestion{
		Kind:     VaultKindCollection,
		Root:     root,
		Includes: []string{"**/*.md"},
		Links:    obsidian.LinkTypeBoth,
		Reason:   "Default markdown collection",
	}
}

func findGitRoot(start string) (string, bool) {
	current := filepath.Clean(start)
	for {
		if current == "" || current == string(filepath.Separator) {
			return "", false
		}
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, true
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

func hasDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func detectAgentHarnesses(root string) AgentHarnesses {
	hasCursor := hasDir(filepath.Join(root, ".cursor"))
	hasClaudeDir := hasDir(filepath.Join(root, ".claude"))
	_, claudeFileErr := os.Stat(filepath.Join(root, "CLAUDE.md"))
	hasClaudeFile := claudeFileErr == nil
	hasCodex := hasDir(filepath.Join(root, ".codex"))
	hasAgentSkills := hasDir(filepath.Join(root, ".agents"))
	_, agentsErr := os.Stat(filepath.Join(root, "AGENTS.md"))

	return AgentHarnesses{
		HasCursor:      hasCursor,
		HasClaude:      hasClaudeDir || hasClaudeFile,
		HasCodex:       hasCodex,
		HasAgentSkills: hasAgentSkills,
		HasAgents:      agentsErr == nil,
	}
}

func hasProjectMarkers(dir string) bool {
	if hasDir(filepath.Join(dir, ".rhizome")) || hasDir(filepath.Join(dir, ".obsidian")) {
		return true
	}
	return hasMarkdown(dir)
}

func hasMarkdown(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}

	matcher := initIgnoreMatcher(path)
	rootPaths, _ := paths.NewVaultPaths(path)
	walkRoot := path
	if rootPaths.Root() != "" {
		walkRoot = rootPaths.Root()
	}
	found := false
	_ = filepath.WalkDir(walkRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if rel, relErr := rootPaths.RelStrict(p); relErr == nil {
				if shouldSkipDir(rel.String(), d.Name(), matcher) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func initIgnoreMatcher(root string) *ignore.Matcher {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	return ignore.LoadUnifiedMatcher(root, nil)
}
