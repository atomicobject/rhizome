package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

func newOntologyInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <path-or-finder...>",
		Short: "Inspect ontology assessment, docs, and relations for matching notes",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := buildOntologyInspectPayload(cmd.Context(), args)
			if err != nil {
				return err
			}
			return writePrettyJSON(cmd.OutOrStdout(), payload)
		},
	}
}

func newAgentOntologyInspectCmd() *cobra.Command {
	var inputs []string
	cmd := &cobra.Command{
		Use:   "ontology-inspect",
		Short: "Inspect ontology assessment for matching notes as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(inputs) == 0 {
				writeAgentError(fmt.Errorf("at least one --input is required"))
				return silentExitError{code: 1}
			}
			payload, err := buildOntologyInspectPayload(cmd.Context(), inputs)
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			return writeJSON(cmd.OutOrStdout(), payload)
		},
	}
	cmd.Flags().StringArrayVar(&inputs, "input", nil, "path or list-style finder (repeatable)")
	return cmd
}

func buildOntologyInspectPayload(ctx context.Context, rawInputs []string) (*ontology.InspectResult, error) {
	vaultDef, err := vaultDefOrDefaultContext(ctx)
	if err != nil {
		return nil, err
	}
	noteReader := &obsidian.Note{}
	noteMetadata, err := newNoteMetadataIndexer()
	if err != nil {
		return nil, err
	}
	runtime, cleanup, ensureErr := ontology.EnsureFreshRuntime(ctx, noteMetadata, vaultDef, noteReader)
	if cleanup != nil {
		defer cleanup()
	}
	if ensureErr != nil {
		return nil, ensureErr
	}
	if runtime == nil || runtime.Schema == nil || runtime.Store == nil {
		return &ontology.InspectResult{OntologyAvailable: false, Notes: []ontology.InspectNote{}}, nil
	}

	targets, err := resolveOntologyInspectTargets(vaultDef, noteReader, runtime.Store, rawInputs)
	if err != nil {
		return nil, err
	}
	service := ontology.NewService(vaultDef, noteReader, runtime.Store, runtime.Schema)
	return service.InspectPaths(ctx, targets)
}

func resolveOntologyInspectTargets(vaultDef obsidian.VaultDefinition, noteReader obsidian.NoteReader, store *semdb.Store, rawInputs []string) ([]string, error) {
	if len(rawInputs) == 0 {
		return nil, fmt.Errorf("at least one path or finder is required")
	}
	vaultPaths, _ := paths.NewVaultPaths(vaultDef.BasePath())
	normalizedInputs := normalizeOntologyInspectInputs(vaultPaths, rawInputs)
	inputs, expr, err := actions.ParseInputsWithExpression(normalizedInputs)
	if err != nil {
		return nil, err
	}
	return actions.ListFiles(&fixedVaultDefinition{def: vaultDef}, noteReader, actions.ListParams{
		Inputs:       inputs,
		Expression:   expr,
		SessionStore: store,
	})
}

type fixedVaultDefinition struct {
	def obsidian.VaultDefinition
}

func (v *fixedVaultDefinition) DefaultName() (string, error) {
	return v.def.Name, nil
}

func (v *fixedVaultDefinition) SetDefaultName(name string) error {
	v.def.Name = name
	return nil
}

func (v *fixedVaultDefinition) Path() (string, error) {
	return v.def.BasePath(), nil
}

func (v *fixedVaultDefinition) Definition() (obsidian.VaultDefinition, error) {
	return v.def, nil
}

func normalizeOntologyInspectInputs(vaultPaths paths.VaultPaths, rawInputs []string) []string {
	normalized := make([]string, 0, len(rawInputs))
	for _, raw := range rawInputs {
		candidate := strings.TrimSpace(raw)
		if !looksLikeOntologyInspectPath(candidate) {
			normalized = append(normalized, raw)
			continue
		}
		rel, abs, err := paths.ResolveNotePathInputWithVaultPaths(vaultPaths, candidate)
		if err != nil || rel == "" || abs == "" {
			normalized = append(normalized, raw)
			continue
		}
		if info, statErr := os.Stat(abs.String()); statErr == nil && !info.IsDir() {
			normalized = append(normalized, rel.String())
			continue
		}
		normalized = append(normalized, raw)
	}
	return normalized
}

func looksLikeOntologyInspectPath(input string) bool {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "", "and", "or", "not", "&&", "||", "!", "(", ")":
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(input))
	if strings.HasPrefix(lower, "find:") || strings.HasPrefix(lower, "tag:") {
		return false
	}
	if filepath.IsAbs(input) || looksLikeWindowsAbsPath(input) {
		return true
	}
	return !strings.Contains(input, ":")
}

func looksLikeWindowsAbsPath(input string) bool {
	input = strings.TrimSpace(input)
	return len(input) >= 3 &&
		((input[0] >= 'A' && input[0] <= 'Z') || (input[0] >= 'a' && input[0] <= 'z')) &&
		input[1] == ':' &&
		(input[2] == '\\' || input[2] == '/')
}

func writePrettyJSON(w interface{ Write([]byte) (int, error) }, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}
