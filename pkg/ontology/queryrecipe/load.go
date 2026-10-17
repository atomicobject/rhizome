package queryrecipe

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultSourceRoots returns active project locations where Rhizome should
// discover saved query recipes for agent-facing surfaces.
func DefaultSourceRoots(vaultPath string) []string {
	return []string{
		filepath.Join(vaultPath, ".rhizome", "query-recipes"),
		filepath.Join(vaultPath, "docs", "query-recipes"),
		filepath.Join(vaultPath, "docs", "rhizome-md-templates"),
		filepath.Join(vaultPath, ".agents", "skills"),
	}
}

// LoadDefaultSources scans the standard recipe registry locations under a vault.
func LoadDefaultSources(vaultPath string) ([]Recipe, []Issue) {
	return LoadRoots(DefaultSourceRoots(vaultPath))
}

// HasDefaultSources includes load diagnostics so malformed sources remain applicable.
func HasDefaultSources(vaultPath string) bool {
	recipes, issues := LoadDefaultSources(vaultPath)
	return len(recipes) > 0 || len(issues) > 0
}

// LoadRoots scans existing recipe roots and deduplicates files across roots.
func LoadRoots(roots []string) ([]Recipe, []Issue) {
	files, issues := scanSources(roots, true)
	return loadSources(files, issues)
}

func LoadPath(path string) ([]Recipe, []Issue) {
	files, issues := scanSources([]string{path}, false)
	return loadSources(files, issues)
}

type sourceFile struct {
	path string
	data []byte
}

func scanSources(roots []string, optional bool) ([]sourceFile, []Issue) {
	var files []sourceFile
	var issues []Issue
	seen := map[string]bool{}
	readCache := map[string][]byte{}
	for _, root := range roots {
		root = filepath.Clean(root)
		info, err := os.Stat(root)
		if err != nil {
			if optional && errors.Is(err, os.ErrNotExist) {
				if _, entryErr := os.Lstat(root); errors.Is(entryErr, os.ErrNotExist) {
					continue
				}
			}
			issues = append(issues, Issue{Code: "recipe_path_error", Path: root, Message: err.Error()})
			continue
		}
		if optional && !info.IsDir() {
			issues = append(issues, Issue{Code: "recipe_path_error", Path: root, Message: "recipe root is not a directory"})
			continue
		}
		read := func(path string, explicit bool) {
			if seen[path] {
				return
			}
			if !explicit && !isRecipeFile(path) {
				return
			}
			data, cached := readCache[path]
			var err error
			if !cached {
				data, err = os.ReadFile(path)
				readCache[path] = data
			}
			if err != nil {
				seen[path] = true
				issues = append(issues, Issue{Code: "recipe_read_error", Path: path, Message: err.Error()})
				return
			}
			ext := strings.ToLower(filepath.Ext(path))
			if !explicit && (ext == ".yaml" || ext == ".yml") && !rootUsesExplicitRecipeFiles(root) && !isNamedQueryRecipeFile(path) && !yamlLooksLikeRecipe(data) {
				return
			}
			seen[path] = true
			files = append(files, sourceFile{path: path, data: data})
		}
		if !info.IsDir() {
			read(root, true)
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				issues = append(issues, Issue{Code: "recipe_path_error", Path: path, Message: walkErr.Error()})
				return nil
			}
			if d.IsDir() {
				if (strings.HasPrefix(d.Name(), ".") && path != root) || isAgentMetadataDir(path) {
					return filepath.SkipDir
				}
				return nil
			}
			read(path, false)
			return nil
		})
	}
	return files, issues
}

func loadSources(files []sourceFile, issues []Issue) ([]Recipe, []Issue) {
	var recipes []Recipe
	for _, file := range files {
		var loaded []Recipe
		var diagnostics []Issue
		switch strings.ToLower(filepath.Ext(file.path)) {
		case ".yaml", ".yml":
			loaded, diagnostics = decodeRecipeYAMLDocuments(file.data, Source{Path: file.path, Line: 1})
		case ".md", ".markdown":
			loaded, diagnostics = parseMarkdownRecipes(file.path, string(file.data))
		default:
			diagnostics = []Issue{{Code: "unsupported_recipe_file", Path: file.path, Message: "expected .md, .yaml, or .yml recipe file"}}
		}
		recipes = append(recipes, loaded...)
		issues = append(issues, diagnostics...)
	}
	return recipes, issues
}

func isRecipeFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

func rootUsesExplicitRecipeFiles(root string) bool {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(root)), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == ".rhizome" && parts[i+1] == "query-recipes" {
			return true
		}
		if parts[i] == "docs" && parts[i+1] == "query-recipes" {
			return true
		}
	}
	return false
}

func isNamedQueryRecipeFile(path string) bool {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(path)), strings.ToLower(filepath.Ext(path)))
	if base == "query-recipes" || base == "query-recipe" {
		return true
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	for _, part := range parts {
		if part == "query-recipes" || part == "query-recipe" {
			return true
		}
	}
	return false
}

var recipeVersionLine = regexp.MustCompile(`(?m)^\s*apiVersion\s*:\s*['"]?rhizome\.(?:ontology-)?query-recipe\.`)

func yamlLooksLikeRecipe(data []byte) bool { return recipeVersionLine.Match(data) }

func isAgentMetadataDir(path string) bool {
	if filepath.Base(path) != "agents" {
		return false
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == ".agents" && parts[i+1] == "skills" {
			return true
		}
	}
	return false
}

func parseMarkdownRecipes(path, content string) ([]Recipe, []Issue) {
	lines := strings.Split(content, "\n")
	var recipes []Recipe
	var issues []Issue
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "```") || !isRecipeFence(strings.TrimSpace(strings.TrimPrefix(line, "```"))) {
			continue
		}
		startLine := i + 1
		var body strings.Builder
		i++
		for ; i < len(lines); i++ {
			if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				break
			}
			body.WriteString(lines[i])
			body.WriteByte('\n')
		}
		if i >= len(lines) {
			issues = append(issues, Issue{Code: "unterminated_recipe_fence", Path: path, Line: startLine, Message: "query recipe fence is missing closing fence"})
			break
		}
		recipe, issue := decodeRecipeYAML([]byte(body.String()), Source{Path: path, Line: startLine + 1, Block: fmt.Sprintf("line-%d", startLine)})
		if issue != nil {
			issues = append(issues, *issue)
			continue
		}
		recipes = append(recipes, recipe)
	}
	return recipes, issues
}

func isRecipeFence(info string) bool {
	fields := strings.Fields(info)
	if len(fields) == 0 {
		return false
	}
	if fields[0] == "query-recipe" {
		return true
	}
	for _, field := range fields {
		if field == "query-recipe" {
			return true
		}
	}
	return false
}

func decodeRecipeYAML(data []byte, source Source) (Recipe, *Issue) {
	var recipe Recipe
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&recipe); err != nil {
		return Recipe{}, &Issue{Code: "recipe_parse_error", Recipe: recipe.ID, Path: source.Path, Line: source.Line, Message: err.Error()}
	}
	recipe.Source = source
	return recipe, nil
}

func decodeRecipeYAMLDocuments(data []byte, source Source) ([]Recipe, []Issue) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var recipes []Recipe
	doc := 0
	for {
		var recipe Recipe
		if err := dec.Decode(&recipe); err != nil {
			if err == io.EOF {
				break
			}
			return recipes, []Issue{{Code: "recipe_parse_error", Recipe: recipe.ID, Path: source.Path, Line: source.Line, Message: err.Error()}}
		}
		doc++
		if strings.TrimSpace(recipe.APIVersion) == "" && strings.TrimSpace(recipe.ID) == "" && strings.TrimSpace(recipe.Name) == "" {
			continue
		}
		recipe.Source = source
		if doc > 1 {
			recipe.Source.Block = fmt.Sprintf("document-%d", doc)
		}
		recipes = append(recipes, recipe)
	}
	return recipes, nil
}
