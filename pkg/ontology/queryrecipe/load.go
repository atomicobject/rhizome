package queryrecipe

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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
	var recipes []Recipe
	var issues []Issue
	seen := map[string]struct{}{}
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				_, entryErr := os.Lstat(root)
				if errors.Is(entryErr, os.ErrNotExist) {
					continue
				}
				if entryErr != nil {
					err = entryErr
				}
			}
			issues = append(issues, Issue{Code: "recipe_path_error", Path: root, Message: err.Error()})
			continue
		}
		if !info.IsDir() {
			issues = append(issues, Issue{Code: "recipe_path_error", Path: root, Message: "recipe root is not a directory"})
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				issues = append(issues, Issue{Code: "recipe_path_error", Path: path, Message: walkErr.Error()})
				return nil
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && path != root {
					return filepath.SkipDir
				}
				if isAgentMetadataDir(path) {
					return filepath.SkipDir
				}
				return nil
			}
			if !isRecipeFileForScan(root, path) {
				return nil
			}
			if _, ok := seen[path]; ok {
				return nil
			}
			seen[path] = struct{}{}
			fileRecipes, fileIssues := LoadPath(path)
			recipes = append(recipes, fileRecipes...)
			issues = append(issues, fileIssues...)
			return nil
		})
	}
	return recipes, issues
}

func LoadPath(path string) ([]Recipe, []Issue) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, []Issue{{Code: "recipe_path_error", Path: path, Message: err.Error()}}
	}
	if !info.IsDir() {
		return loadFile(path)
	}
	var recipes []Recipe
	var issues []Issue
	err = filepath.WalkDir(path, func(current string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			issues = append(issues, Issue{Code: "recipe_path_error", Path: current, Message: walkErr.Error()})
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && current != path {
				return filepath.SkipDir
			}
			if isAgentMetadataDir(current) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isRecipeFileForScan(path, current) {
			return nil
		}
		fileRecipes, fileIssues := loadFile(current)
		recipes = append(recipes, fileRecipes...)
		issues = append(issues, fileIssues...)
		return nil
	})
	if err != nil {
		issues = append(issues, Issue{Code: "recipe_path_error", Path: path, Message: err.Error()})
	}
	return recipes, issues
}

func loadFile(path string) ([]Recipe, []Issue) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, []Issue{{Code: "recipe_read_error", Path: path, Message: err.Error()}}
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return decodeRecipeYAMLDocuments(data, Source{Path: path, Line: 1})
	case ".md", ".markdown":
		return parseMarkdownRecipes(path, string(data))
	default:
		return nil, []Issue{{Code: "unsupported_recipe_file", Path: path, Message: "expected .md, .yaml, or .yml recipe file"}}
	}
}

func isRecipeFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

func isRecipeFileForScan(root, path string) bool {
	if !isRecipeFile(path) {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".md" || ext == ".markdown" {
		return true
	}
	if rootUsesExplicitRecipeFiles(root) {
		return true
	}
	if isNamedQueryRecipeFile(path) {
		return true
	}
	return yamlFileLooksLikeRecipe(path)
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

func yamlFileLooksLikeRecipe(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	return bytes.Contains(data, []byte("apiVersion: "+APIVersion)) || bytes.Contains(data, []byte("apiVersion: \""+APIVersion+"\""))
}

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
		return Recipe{}, &Issue{Code: "recipe_parse_error", Path: source.Path, Line: source.Line, Message: err.Error()}
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
			return nil, []Issue{{Code: "recipe_parse_error", Path: source.Path, Line: source.Line, Message: err.Error()}}
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
