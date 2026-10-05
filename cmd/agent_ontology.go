package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/guide"
	"github.com/atomicobject/rhizome/pkg/ontology/idalloc"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

func newAgentOntologyQuerySchemaCmd() *cobra.Command {
	var typeName string
	cmd := &cobra.Command{
		Use:   "ontology-query-schema",
		Short: "Print ontology query schema as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := assertRuntimeFreeAgentOperation(cmd); err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			vaultDef, err := vaultDefOrDefaultContext(cmd.Context())
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			schema, execSchema, err := loadOntologyExecutableSchema(vaultDef.BasePath())
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			payload, err := ontologyquery.DiscoverSchema(schema, execSchema, typeName)
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			if err := writeJSON(cmd.OutOrStdout(), payload); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&typeName, "type", "", "return a selected executable type fragment and its root arguments")
	return cmd
}

func newAgentOntologyQueryCmd() *cobra.Command {
	var queryText, queryFile, jsonPayload string
	cmd := &cobra.Command{
		Use:   "ontology-query",
		Short: "Execute a read-only ontology GraphQL query as JSON",
		Long:  "Execute a read-only ontology GraphQL query as JSON. Typed-root coverage is returned in extensions.typedRoots by response alias; continue with offset when nextOffset is present.",
		RunE: func(cmd *cobra.Command, args []string) error {
			rawQuery, variables, err := readAgentOntologyQueryInput(queryText, queryFile, jsonPayload)
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			rt, prepared, cleanup, err := prepareOntologyQuery(cmd.Context(), rawQuery, variables)
			if cleanup != nil {
				defer cleanup()
			}
			if err != nil {
				writeAgentQueryError(err)
				return silentExitError{code: 1}
			}
			result := ontologyquery.ExecutePrepared(cmd.Context(), rt.deps(), rt.schema, rt.execSchema, prepared)
			if len(result.Errors) > 0 {
				writeAgentQueryResult(result)
				return silentExitError{code: 1}
			}
			if err := writeJSON(cmd.OutOrStdout(), result); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&queryText, "query", "", "inline GraphQL query text")
	cmd.Flags().StringVar(&queryFile, "file", "", "path to a GraphQL query file")
	cmd.Flags().StringVar(&jsonPayload, "json", "", "JSON payload with query and variables")
	return cmd
}

type agentOntologyQueryPayload struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

func readAgentOntologyQueryInput(queryText, queryFile, jsonPayload string) (string, map[string]any, error) {
	hasPayload := strings.TrimSpace(jsonPayload) != ""
	if !hasPayload {
		rawQuery, err := readOntologyQueryInput(queryText, queryFile)
		return rawQuery, nil, err
	}
	if strings.TrimSpace(queryText) != "" || strings.TrimSpace(queryFile) != "" {
		return "", nil, fmt.Errorf("--json cannot be combined with --query or --file")
	}
	var payload agentOntologyQueryPayload
	decoder := json.NewDecoder(strings.NewReader(jsonPayload))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(payload.Query) == "" {
		return "", nil, fmt.Errorf("--json payload requires query")
	}
	return payload.Query, payload.Variables, nil
}

func newAgentOntologyReferenceCmd() *cobra.Command {
	var compact bool
	var typeName string
	cmd := &cobra.Command{
		Use:   "ontology-reference",
		Short: "Render ontology reference as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := assertRuntimeFreeAgentOperation(cmd); err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			vaultDef, err := vaultDefOrDefaultContext(cmd.Context())
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			schema, _, err := loadOntologyExecutableSchema(vaultDef.BasePath())
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			typeName, err := singleOntologyTypeFilter(parseOntologyTypes(typeName))
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			if compact {
				payload, err := reference.Compact(schema, typeName)
				if err != nil {
					writeAgentError(err)
					return silentExitError{code: 1}
				}
				return writeJSON(cmd.OutOrStdout(), payload)
			}
			payload, err := reference.RenderJSON(schema, typeName)
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			_, err = cmd.OutOrStdout().Write(append(payload, '\n'))
			return err
		},
	}
	cmd.Flags().StringVar(&typeName, "type", "", "restrict reference output to a single ontology type")
	cmd.Flags().BoolVar(&compact, "compact", false, "return the compact authored contract; requires --type")
	return cmd
}

func newAgentOntologyAuthoringGuideCmd() *cobra.Command {
	var typeNames string
	cmd := &cobra.Command{
		Use:   "ontology-authoring-guide",
		Short: "Render ontology note-authoring guidance as markdown",
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := renderAgentOntologyAuthoringGuide(cmd.Context(), parseOntologyTypes(typeNames))
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			_, err = cmd.OutOrStdout().Write([]byte(text + "\n"))
			return err
		},
	}
	cmd.Flags().StringVar(&typeNames, "type", "", "restrict authoring guidance to specific ontology types (comma-separated)")
	return cmd
}

func renderAgentOntologyAuthoringGuide(ctx context.Context, requested []string) (string, error) {
	vaultDef, err := vaultDefOrDefaultContext(ctx)
	if err != nil {
		return "", err
	}
	schema, _, err := loadOntologyExecutableSchema(vaultDef.BasePath())
	if err != nil {
		return "", err
	}
	schema, lookup, cleanup := authoringGuideIDLookup(ctx, schema, requested)
	defer cleanup()
	return guide.RenderMarkdownWithLookups(schema, requested, ontologyCompanionResolver(nil), lookup)
}

// authoringGuideIDLookup pairs rendering and optional ID suggestions with one
// refreshed schema. Without eligible types or an available runtime, it retains
// the original schema and omits suggestions.
func authoringGuideIDLookup(ctx context.Context, schema *ontology.Schema, requested []string) (*ontology.Schema, guide.IDLookup, func()) {
	if !anyRequestedTypeUsesIDFormat(schema, requested) {
		return schema, nil, func() {}
	}
	live, err := buildOntologyQueryLiveRuntime(ctx, oneshotruntime.OntologyQueryPlan(false, false))
	if err != nil {
		return schema, nil, func() {}
	}
	cleanup := func() { _ = live.Close() }
	noteReader := &obsidian.Note{}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, live.NoteMetadataIndexer(), live.VaultDef, noteReader, live.IntelStore())
	if err != nil || runtime == nil || runtime.Schema == nil || runtime.Store == nil {
		cleanup()
		return schema, nil, func() {}
	}
	lookup := guide.IDLookup(func(typeName string) (string, bool, error) {
		res, err := idalloc.Allocate(ctx, runtime.Schema, runtime.Store, typeName)
		if err != nil {
			// Allocate's typed errors (`unsupported_for_type`,
			// `no_preferred_identifier`, `type_not_found`) just mean the
			// guide can't suggest an id for this type; skip silently.
			return "", false, nil
		}
		return res.Next, true, nil
	})
	return runtime.Schema, lookup, cleanup
}

func anyRequestedTypeUsesIDFormat(schema *ontology.Schema, requested []string) bool {
	if schema == nil {
		return false
	}
	check := func(nt *ontology.NoteType) bool {
		if nt == nil {
			return false
		}
		for _, f := range nt.Fields {
			if f != nil && f.IsPreferredIdentifier && f.IdentifierFormat != nil {
				return true
			}
		}
		return false
	}
	if len(requested) == 0 {
		for _, nt := range schema.Types {
			if check(nt) {
				return true
			}
		}
		return false
	}
	for _, name := range requested {
		if check(schema.Types[name]) {
			return true
		}
	}
	return false
}

func writeAgentQueryError(err error) {
	var payload map[string]any
	if json.Unmarshal([]byte(err.Error()), &payload) == nil {
		encoded, marshalErr := json.Marshal(payload)
		if marshalErr == nil {
			_, _ = os.Stderr.Write(append(encoded, '\n'))
			return
		}
	}
	writeAgentError(err)
}

func writeAgentQueryResult(result ontologyquery.Result) {
	encoded, err := json.Marshal(result)
	if err != nil {
		writeAgentError(err)
		return
	}
	_, _ = os.Stderr.Write(append(encoded, '\n'))
}
