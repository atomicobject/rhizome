// Command contract exposes pure canonical parsers to release snapshot selection.
// Input content comes from the caller's Git snapshot, never from a live note.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/html"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type request struct {
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

type snapshotSchemaError struct {
	error
}

func evaluate(input request) (any, error) {
	switch input.Kind {
	case "note-discovery":
		return discoverSnapshotNote(input.Content)
	case "workspace-id":
		format := ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategyDateTime, Prefix: "EFF", Separator: "-"}
		contract, err := format.StrategyContract()
		if err != nil {
			return nil, err
		}
		_, valid := contract.Parse(input.Content)
		if !valid {
			return nil, fmt.Errorf("workspace id must follow the EFF DATETIME identifier contract")
		}
		return true, nil
	case "effort-sections":
		sections := ontology.ParseSections("snapshot.md", input.Content)
		return map[string]string{
			"actual_delivered": effortSectionBody(sections, "Actual Delivered"),
			"deviations":       effortSectionBody(sections, "Deviations"),
		}, nil
	case "spec-metadata", "target-type", "spec-like":
		path, content := "snapshot.md", input.Content
		var schemaFiles map[string]string
		if input.Kind != "spec-metadata" {
			var target struct {
				Path        string            `json:"path"`
				Content     string            `json:"content"`
				SchemaFiles map[string]string `json:"schema_files"`
			}
			if err := json.Unmarshal([]byte(content), &target); err != nil {
				return nil, err
			}
			path, content = target.Path, target.Content
			schemaFiles = target.SchemaFiles
		}
		var schema *ontology.Schema
		if schemaFiles != nil {
			var err error
			schema, err = loadTargetSchema(schemaFiles)
			if err != nil {
				return nil, &snapshotSchemaError{fmt.Errorf("invalid snapshot ontology: %w", err)}
			}
		}
		var provider noteformat.Projector = markdown.New()
		if filepath.Ext(path) == ".html" {
			provider = html.New()
		}
		source, err := noteformat.NewAuthoredSource(paths.NotePath(path), provider.Descriptor(), []byte(content), 0)
		if err != nil {
			return nil, err
		}
		projection, err := provider.Project(source)
		if err != nil {
			return nil, err
		}
		for _, diagnostic := range projection.Diagnostics {
			if diagnostic.Code == "markdown_frontmatter_invalid" || diagnostic.Category == noteformat.DiagnosticCategoryMetadata || diagnostic.Blocking {
				if input.Kind != "target-type" {
					return nil, fmt.Errorf("invalid governing spec frontmatter: %s", diagnostic.Message)
				}
				return nil, fmt.Errorf("invalid component metadata: %s", diagnostic.Message)
			}
		}
		metadata := map[string]any{}
		for _, fact := range projection.Facts.RootMetadata {
			metadata[fact.Key] = fact.Value.Export()
		}
		if input.Kind == "spec-like" {
			if value, present := metadata["type"]; present {
				if _, valid := value.(string); !valid {
					return nil, fmt.Errorf("invalid governing spec frontmatter: type must be a string")
				}
			}
		}
		if input.Kind != "spec-metadata" {
			if schemaFiles == nil {
				schema, err = loadTargetSchema(nil)
				if err != nil {
					return nil, err
				}
			}
			root := &ontology.RootDocumentSnapshot{NotePath: paths.NotePath(path), Metadata: metadata, RawSource: []byte(content), Projection: projection}
			resolved, err := ontology.ProjectRootDocumentSnapshot(root, schema)
			if err != nil {
				return nil, err
			}
			if input.Kind == "spec-like" {
				return ontology.TypeMatchesOrImplements(schema, resolved.ResolvedType, "SpecLike"), nil
			}
			return resolved.ResolvedType, nil
		}
		return metadata, nil
	default:
		return nil, fmt.Errorf("unknown release contract: %q", input.Kind)
	}
}

func effortSectionBody(nodes []*ontology.SectionNode, heading string) string {
	matches := ontology.FindMatchingSections(nodes, ontology.SectionLevelH2, heading)
	if len(matches) == 0 {
		return ""
	}
	_, body, _ := strings.Cut(matches[0].Content, "\n")
	return strings.TrimSpace(body)
}

func main() {
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for {
		var input request
		if err := decoder.Decode(&input); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		result, err := evaluate(input)
		response := map[string]any{"result": result}
		if err != nil {
			response = map[string]any{"error": err.Error()}
			var schemaError *snapshotSchemaError
			if errors.As(err, &schemaError) {
				response["error_kind"] = "snapshot_schema"
			}
		}
		if err := encoder.Encode(response); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

// Materialize only the candidate and policy files from its snapshot, then use
// the same discovery path as vault indexing (including ancestor pruning).
func discoverSnapshotNote(content string) (bool, error) {
	var snapshot struct {
		Path  string            `json:"path"`
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal([]byte(content), &snapshot); err != nil {
		return false, err
	}
	root, err := os.MkdirTemp("", "rhizome-release-discovery-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(root)
	if snapshot.Files == nil {
		snapshot.Files = make(map[string]string)
	}
	snapshot.Files[snapshot.Path] = ""
	for name, data := range snapshot.Files {
		if !filepath.IsLocal(name) {
			return false, fmt.Errorf("invalid snapshot path: %q", name)
		}
		target := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(target, []byte(data), 0o600); err != nil {
			return false, err
		}
	}
	definition, err := obsidian.LoadDefinitionFromPath(root)
	if errors.Is(err, obsidian.ErrNoLocalConfig) {
		// Historical repositories without local config retain Markdown discovery.
		definition = obsidian.VaultDefinition{Path: root}
	} else if err != nil {
		return false, err
	}
	notes, err := obsidian.DiscoverFiles(definition)
	if err != nil {
		return false, err
	}
	return slices.Contains(notes, snapshot.Path), nil
}

// Omitted files retain the standalone parser API; an explicit snapshot never
// falls back to the working tree, including when its ontology is empty.
func loadTargetSchema(files map[string]string) (*ontology.Schema, error) {
	if files == nil {
		return ontology.LoadSchema(".")
	}
	root, err := os.MkdirTemp("", "rhizome-release-schema-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	dir := ontology.OntologyDir(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	for name, content := range files {
		if filepath.Base(name) != name || filepath.Ext(name) != ".graphql" {
			return nil, fmt.Errorf("invalid snapshot schema filename: %q", name)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			return nil, err
		}
	}
	return ontology.LoadSchema(root)
}
