package init

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/codepatterns"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
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
	ExistingLocal             obsidian.LocalConfig
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
	layout.DocPatterns = obsidian.FileContextConfigDefaults.DocPatterns
	layout.AgentHarnesses = detectAgentHarnesses(layout.ProjectRoot)

	return layout, nil
}

func loadExistingConfig(layout *DetectedLayout) error {
	cfg, err := obsidian.LoadLocalConfig(layout.ProjectRoot)
	if err != nil {
		if errors.Is(err, obsidian.ErrNoLocalConfig) {
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
		legacyCfg, legacyPresent, err := obsidian.LoadLocalConfigForWorkflowMigration(layout.ProjectRoot)
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

func vaultSuggestionFromConfig(cfg obsidian.LocalVaultConfig, projectRoot string) (VaultSuggestion, bool) {
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

// detectCode inspects the tree for language signals and module roots.
func detectCode(root string) CodeSuggestion {
	langSet := map[string]struct{}{}
	pythonRoots := map[string]struct{}{}
	goMarkerRoots := map[string]struct{}{}
	goFallbackRoots := map[string]struct{}{}
	tsMarkerRoots := map[string]struct{}{}
	tsFallbackRoots := map[string]struct{}{}
	csMarkerRoots := map[string]struct{}{}
	csFallbackRoots := map[string]struct{}{}
	phpMarkerRoots := map[string]struct{}{}
	phpFallbackRoots := map[string]struct{}{}
	rootPaths, _ := paths.NewVaultPaths(root)
	matcher := initIgnoreMatcher(root)
	fileCount := 0
	var files []string

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, relErr := rootPaths.RelStrict(path)
		if relErr != nil {
			return relErr
		}
		rel := relPath.String()

		if d.IsDir() {
			if shouldSkipDir(rel, d.Name(), matcher) {
				return filepath.SkipDir
			}
			return nil
		}

		switch strings.ToLower(filepath.Base(path)) {
		case "go.mod":
			langSet["go"] = struct{}{}
			if rel != "" {
				// Root is the directory containing go.mod (module root).
				moduleRoot := filepath.ToSlash(filepath.Dir(rel))
				if moduleRoot == "." {
					moduleRoot = "."
				}
				goMarkerRoots[moduleRoot] = struct{}{}
			}
		case "package.json", "tsconfig.json":
			langSet["typescript"] = struct{}{}
			langSet["javascript"] = struct{}{}
			if rel != "" {
				// Root is the directory containing package.json/tsconfig.json.
				moduleRoot := filepath.ToSlash(filepath.Dir(rel))
				if moduleRoot == "." {
					moduleRoot = "."
				}
				tsMarkerRoots[moduleRoot] = struct{}{}
			}
		case "pyproject.toml", "requirements.txt", "setup.py":
			langSet["python"] = struct{}{}
		case "cargo.toml":
			langSet["rust"] = struct{}{}
		case "pom.xml", "build.gradle", "build.gradle.kts":
			langSet["java"] = struct{}{}
		case "global.json", "Directory.Build.props", "Directory.Build.targets":
			langSet["csharp"] = struct{}{}
		case "composer.json":
			langSet["php"] = struct{}{}
			if rel != "" {
				moduleRoot := filepath.ToSlash(filepath.Dir(rel))
				if moduleRoot == "." {
					moduleRoot = "."
				}
				phpMarkerRoots[moduleRoot] = struct{}{}
			}
		}

		if strings.HasSuffix(strings.ToLower(path), ".csproj") || strings.HasSuffix(strings.ToLower(path), ".sln") {
			langSet["csharp"] = struct{}{}
			if rel != "" {
				rootDir := filepath.ToSlash(filepath.Dir(rel))
				if rootDir == "." {
					rootDir = "."
				}
				csMarkerRoots[rootDir] = struct{}{}
			}
		}

		ext := strings.ToLower(filepath.Ext(path))
		files = append(files, rel)
		if sourceFileExtensions[ext] {
			fileCount++
		}
		if recordTypeScriptJavaScriptFile(ext, rel, langSet, tsFallbackRoots) {
			return nil
		}
		switch ext {
		case ".go":
			langSet["go"] = struct{}{}
			if rel != "" {
				parts := strings.Split(rel, "/")
				if len(parts) > 1 {
					goFallbackRoots[parts[0]] = struct{}{}
				} else {
					goFallbackRoots["."] = struct{}{}
				}
			}
		case ".py":
			langSet["python"] = struct{}{}
			root := pythonRootFromRel(rel)
			if root != "" {
				pythonRoots[root] = struct{}{}
			}
		case ".java":
			langSet["java"] = struct{}{}
		case ".cs":
			langSet["csharp"] = struct{}{}
			if rel != "" {
				parts := strings.Split(rel, "/")
				if len(parts) > 1 {
					csFallbackRoots[parts[0]] = struct{}{}
				} else {
					csFallbackRoots["."] = struct{}{}
				}
			}
		case ".php":
			langSet["php"] = struct{}{}
			if rel != "" {
				parts := strings.Split(rel, "/")
				if len(parts) > 1 {
					phpFallbackRoots[parts[0]] = struct{}{}
				} else {
					phpFallbackRoots["."] = struct{}{}
				}
			}
		case ".c", ".cpp", ".h", ".hpp":
			langSet["cpp"] = struct{}{}
		case ".rs":
			langSet["rust"] = struct{}{}
		case ".rb":
			langSet["ruby"] = struct{}{}
		case ".sh":
			langSet["shell"] = struct{}{}
		}

		return nil
	})

	if walkErr != nil {
		return CodeSuggestion{}
	}

	langs := make([]string, 0, len(langSet))
	for lang := range langSet {
		langs = append(langs, lang)
	}
	sort.Strings(langs)

	scan := includesForLanguages(langs)
	if len(scan) == 0 && len(langs) > 0 {
		scan = append(scan, coderefs.DefaultIncludes...)
	}

	var pyRoots []string
	for r := range pythonRoots {
		pyRoots = append(pyRoots, r)
	}
	pyRoots = rollupRoots(pyRoots)

	goRoots := goMarkerRoots
	if len(goRoots) == 0 {
		goRoots = goFallbackRoots
	}
	var gRoots []string
	for r := range goRoots {
		gRoots = append(gRoots, r)
	}
	gRoots = rollupRoots(gRoots)

	tsRoots := tsMarkerRoots
	if len(tsRoots) == 0 {
		tsRoots = tsFallbackRoots
	}
	var tRoots []string
	for r := range tsRoots {
		tRoots = append(tRoots, r)
	}
	tRoots = rollupRoots(tRoots)

	csRoots := csMarkerRoots
	if len(csRoots) == 0 {
		csRoots = csFallbackRoots
	}
	var cRoots []string
	for r := range csRoots {
		cRoots = append(cRoots, r)
	}
	cRoots = rollupRoots(cRoots)

	phpRoots := phpMarkerRoots
	if len(phpRoots) == 0 {
		phpRoots = phpFallbackRoots
	}
	var pRoots []string
	for r := range phpRoots {
		pRoots = append(pRoots, r)
	}
	pRoots = rollupRoots(pRoots)

	return CodeSuggestion{
		EnableCodeRefs: len(langs) > 0,
		Languages:      langs,
		Scan:           scan,
		Ignore:         append([]string{}, obsidian.DefaultCodeConfig.Ignore...),
		PythonRoots:    pyRoots,
		PythonScan:     codepatterns.DefaultPythonGlobs(),
		PythonIgnore:   []string{},
		GoRoots:        gRoots,
		GoScan:         codepatterns.DefaultGoGlobs(),
		GoIgnore:       []string{},
		TSRoots:        tRoots,
		TSScan:         codepatterns.DefaultTypeScriptGlobs(),
		TSIgnore:       []string{},
		CSharpRoots:    cRoots,
		CSharpScan:     codepatterns.DefaultCSharpGlobs(),
		CSharpIgnore:   []string{},
		PHPRoots:       pRoots,
		PHPScan:        codepatterns.DefaultPHPGlobs(),
		PHPIgnore:      []string{},
		FileCount:      fileCount,
		Files:          files,
	}
}

var sourceFileExtensions = map[string]bool{
	".go": true, ".py": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
	".cs": true, ".php": true, ".java": true, ".rs": true, ".rb": true, ".c": true, ".cpp": true, ".h": true, ".hpp": true,
}

func recordTypeScriptJavaScriptFile(ext, rel string, langSet, fallbackRoots map[string]struct{}) bool {
	switch {
	case codefile.IsTypeScriptExtension(ext):
		langSet["typescript"] = struct{}{}
		if ext == ".tsx" {
			langSet["javascript"] = struct{}{}
		}
	case codefile.IsJavaScriptExtension(ext):
		langSet["javascript"] = struct{}{}
	default:
		return false
	}
	if rel == "" {
		return true
	}
	parts := strings.Split(rel, "/")
	if len(parts) > 1 {
		fallbackRoots[parts[0]] = struct{}{}
	} else {
		fallbackRoots["."] = struct{}{}
	}
	return true
}

func includesForLanguages(langs []string) []string {
	patterns := map[string][]string{
		"go":         codepatterns.DefaultGoGlobs(),
		"typescript": codepatterns.DefaultTypeScriptOnlyGlobs(),
		"javascript": codepatterns.DefaultJavaScriptOnlyGlobs(),
		"python":     codepatterns.DefaultPythonGlobs(),
		"java":       {"**/*.java"},
		"csharp":     codepatterns.DefaultCSharpGlobs(),
		"php":        codepatterns.DefaultPHPGlobs(),
		"cpp":        {"**/*.c", "**/*.cpp", "**/*.h", "**/*.hpp"},
		"rust":       {"**/*.rs"},
		"ruby":       {"**/*.rb"},
		"shell":      {"**/*.sh"},
	}

	var result []string
	seen := map[string]struct{}{}
	for _, lang := range langs {
		for _, p := range patterns[lang] {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			result = append(result, p)
		}
	}
	return result
}

func rollupRoots(roots []string) []string {
	seen := map[string]struct{}{}
	var cleaned []string
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		r = filepath.ToSlash(filepath.Clean(r))
		if r == "" || r == "/" {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		cleaned = append(cleaned, r)
	}

	if _, ok := seen["."]; ok {
		return []string{"."}
	}

	sort.Slice(cleaned, func(i, j int) bool {
		if len(cleaned[i]) == len(cleaned[j]) {
			return cleaned[i] < cleaned[j]
		}
		return len(cleaned[i]) < len(cleaned[j])
	})

	var kept []string
	for _, r := range cleaned {
		skip := false
		for _, k := range kept {
			if isSubpath(r, k) {
				skip = true
				break
			}
		}
		if !skip {
			kept = append(kept, r)
		}
	}
	return kept
}

func isSubpath(child, parent string) bool {
	if parent == "" || child == "" {
		return false
	}
	if child == parent {
		return true
	}
	return strings.HasPrefix(child, parent+"/")
}

func pythonRootFromRel(rel string) string {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		switch part {
		case "src", "packages", "scripts", "app", "apps", "services", "pkg":
			return strings.Join(parts[:i+1], "/")
		}
	}
	if len(parts) > 1 {
		return parts[0]
	}
	return "."
}

func shouldSkipDir(rel, name string, matcher *ignore.Matcher) bool {
	if strings.HasPrefix(name, ".") && name != "." {
		return true
	}
	if matcher == nil {
		return false
	}
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return false
	}
	return matcher.IsIgnoredShallow(rel, true)
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
