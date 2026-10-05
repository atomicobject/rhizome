package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/ontology/guide"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

func newOntologyQuerySchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "query-schema",
		Short: "Print the executable ontology query schema as SDL",
		RunE: func(cmd *cobra.Command, args []string) error {
			vaultDef, err := vaultDefOrDefaultContext(cmd.Context())
			if err != nil {
				return err
			}
			_, execSchema, err := loadOntologyExecutableSchema(vaultDef.BasePath())
			if err != nil {
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), execSchema.SDL)
			return err
		},
	}
}

func newOntologyQueryCmd() *cobra.Command {
	var queryText string
	var queryFile string
	var variablesJSON string
	var variablesFile string
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Execute a read-only GraphQL ontology query",
		Long:  "Execute a read-only GraphQL ontology query. Typed-root coverage is returned in extensions.typedRoots by response alias; continue with offset when nextOffset is present.",
		RunE: func(cmd *cobra.Command, args []string) error {
			rawQuery, err := readOntologyQueryInput(queryText, queryFile)
			if err != nil {
				return err
			}
			variables, err := readOntologyVariablesInput(variablesJSON, variablesFile)
			if err != nil {
				return err
			}
			rt, prepared, cleanup, err := prepareOntologyQuery(cmd.Context(), rawQuery, variables)
			if cleanup != nil {
				defer cleanup()
			}
			if err != nil {
				if writeErr := writeOntologyQueryFailure(cmd.OutOrStdout(), err); writeErr != nil {
					return writeErr
				}
				return silentExitError{code: 1}
			}

			result := ontologyquery.ExecutePrepared(cmd.Context(), rt.deps(), rt.schema, rt.execSchema, prepared)
			if err := writeJSON(cmd.OutOrStdout(), result); err != nil {
				return err
			}
			if len(result.Errors) > 0 {
				return silentExitError{code: 1}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&queryText, "query", "", "inline GraphQL query text")
	cmd.Flags().StringVar(&queryFile, "file", "", "path to a GraphQL query file")
	cmd.Flags().StringVar(&variablesJSON, "variables-json", "", "GraphQL variables as a JSON object")
	cmd.Flags().StringVar(&variablesFile, "variables-file", "", "path to a JSON file containing GraphQL variables")
	return cmd
}

func newOntologyReferenceCmd() *cobra.Command {
	var typeName string
	cmd := &cobra.Command{
		Use:   "reference",
		Short: "Render ontology schema reference from the compiled schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			vaultDef, err := vaultDefOrDefaultContext(cmd.Context())
			if err != nil {
				return err
			}
			schema, _, err := loadOntologyExecutableSchema(vaultDef.BasePath())
			if err != nil {
				return err
			}
			typeName, err := singleOntologyTypeFilter(parseOntologyTypes(typeName))
			if err != nil {
				return err
			}
			text, err := reference.RenderMarkdown(schema, typeName)
			if err != nil {
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), text)
			return err
		},
	}
	cmd.Flags().StringVar(&typeName, "type", "", "restrict reference output to a single ontology type")
	return cmd
}

func newOntologyAuthoringGuideCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "authoring-guide [type...]",
		Short: "Render note-authoring guidance from the compiled ontology schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			vaultDef, err := vaultDefOrDefaultContext(cmd.Context())
			if err != nil {
				return err
			}
			schema, _, err := loadOntologyExecutableSchema(vaultDef.BasePath())
			if err != nil {
				return err
			}
			note, err := newProjectedNoteReader(cmd.Context(), vaultDef)
			if err != nil {
				return err
			}
			text, err := guide.RenderMarkdownWithResolver(schema, args, ontologyCompanionResolver(note))
			if err != nil {
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), text)
			return err
		},
	}
}

func ontologyCompanionResolver(note obsidian.NoteReader) guide.CompanionDocResolver {
	if note == nil {
		note = &obsidian.Note{}
	}
	return func(path string) (guide.CompanionDocMeta, bool) {
		meta := guide.CompanionDocMeta{}
		if title, ok := note.Title(path); ok {
			meta.Title = strings.TrimSpace(title)
		}
		fact, ok := actions.NoteFactsFromReader(note).LookupFact(path)
		if ok {
			meta.Summary = firstFrontmatterSummary(fact.Frontmatter)
		}
		return meta, ok
	}
}

func firstFrontmatterSummary(frontmatter map[string]interface{}) string {
	if len(frontmatter) == 0 {
		return ""
	}
	for _, key := range []string{"summary", "synopsis", "about", "description", "desc", "doc"} {
		for current, value := range frontmatter {
			if !strings.EqualFold(current, key) {
				continue
			}
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" {
				return text
			}
		}
	}
	return ""
}

func singleOntologyTypeFilter(types []string) (string, error) {
	cleaned := make([]string, 0, len(types))
	seen := make(map[string]struct{}, len(types))
	for _, typeName := range types {
		typeName = strings.TrimSpace(typeName)
		if typeName == "" {
			continue
		}
		if _, ok := seen[typeName]; ok {
			continue
		}
		seen[typeName] = struct{}{}
		cleaned = append(cleaned, typeName)
	}
	switch len(cleaned) {
	case 0:
		return "", nil
	case 1:
		return cleaned[0], nil
	default:
		return "", fmt.Errorf("--type may be provided at most once")
	}
}

func parseOntologyTypes(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func writeJSON(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s\n", data); err != nil {
		return err
	}
	return nil
}

func writeOntologyQueryFailure(w io.Writer, err error) error {
	var result ontologyquery.Result
	if json.Unmarshal([]byte(err.Error()), &result) == nil && len(result.Errors) > 0 {
		return writeJSON(w, result)
	}
	return writeJSON(w, ontologyquery.Result{
		Errors: []ontologyquery.Error{{Message: err.Error()}},
	})
}

func init() {
	ontologyCmd.AddCommand(newOntologyQuerySchemaCmd())
	ontologyCmd.AddCommand(newOntologyQueryCmd())
	ontologyCmd.AddCommand(newOntologyReferenceCmd())
	ontologyCmd.AddCommand(newOntologyAuthoringGuideCmd())
	ontologyCmd.AddCommand(newOntologyInspectCmd())
}
