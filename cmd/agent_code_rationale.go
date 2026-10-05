package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/spf13/cobra"
)

type agentCodeRationaleEntry struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	SymbolFQN   string `json:"symbolFqn,omitempty"`
	Kind        string `json:"kind"`
	Content     string `json:"content"`
	StartLine   int64  `json:"startLine"`
	EndLine     int64  `json:"endLine"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type agentCodeRationaleResponse struct {
	Count     int                       `json:"count"`
	Rationale []agentCodeRationaleEntry `json:"rationale"`
}

func newAgentCodeRationaleCmd() *cobra.Command {
	var pathInput string
	var kinds []string
	cmd := &cobra.Command{
		Use:   "code-rationale",
		Short: "List extracted rationale comments from indexed code as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			operationID, err := agentOperationIDFromCommand(cmd)
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			cfg, rt, err := buildAgentConfigForOperation(cmd.Context(), 0, "code_rationale", operationID)
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			defer rt.Close()
			if cfg.IndexedContextUnavailable != nil {
				encoded, marshalErr := json.Marshal(map[string]string{
					"code":        cfg.IndexedContextUnavailable.WarningCode,
					"message":     "code_rationale requires the managed indexed read model",
					"remediation": cfg.IndexedContextUnavailable.Remediation,
				})
				if marshalErr != nil {
					writeAgentError(fmt.Errorf("managed indexed read model is unavailable"))
				} else {
					writeAgentToolError(fmt.Errorf("%s", encoded))
				}
				return silentExitError{code: 1}
			}

			resp, err := queryAgentCodeRationale(cmd.Context(), cfg.GetIntelStore(), cfg.VaultPath, pathInput, kinds)
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			return writeAgentPayload(cmd, resp)
		},
	}
	cmd.Flags().StringVar(&pathInput, "path", "", "file or directory path; omit for all indexed rationale")
	cmd.Flags().StringArrayVar(&kinds, "kind", nil, "filter by kind (repeatable or comma-separated)")
	return cmd
}

func queryAgentCodeRationale(ctx context.Context, store *semdb.Store, vaultPath, pathInput string, kinds []string) (agentCodeRationaleResponse, error) {
	if store == nil {
		return agentCodeRationaleResponse{}, fmt.Errorf("code index not available")
	}

	kinds = normalizeRationaleKinds(kinds)

	var (
		rows []agentCodeRationaleEntry
		err  error
	)
	if strings.TrimSpace(pathInput) == "" {
		rows, err = loadAgentCodeRationaleAll(ctx, store, kinds)
	} else {
		rows, err = loadAgentCodeRationaleByPath(ctx, store, vaultPath, pathInput, kinds)
	}
	if err != nil {
		return agentCodeRationaleResponse{}, err
	}
	return agentCodeRationaleResponse{Count: len(rows), Rationale: rows}, nil
}

func normalizeRationaleKinds(values []string) []string {
	out := make([]string, 0, len(values))
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			part = strings.ToLower(strings.TrimSpace(part))
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func loadAgentCodeRationaleAll(ctx context.Context, store *semdb.Store, kinds []string) ([]agentCodeRationaleEntry, error) {
	rationale, err := store.RationaleByKind(ctx, kinds)
	if err != nil {
		return nil, fmt.Errorf("rationale query: %w", err)
	}
	return toAgentCodeRationaleEntries(rationale), nil
}

func loadAgentCodeRationaleByPath(ctx context.Context, store *semdb.Store, vaultPath, pathInput string, kinds []string) ([]agentCodeRationaleEntry, error) {
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return nil, fmt.Errorf("invalid vault path %q", vaultPath)
	}

	_, abs, err := paths.ResolveCodeInputWithVaultPaths(vaultPaths, pathInput)
	if err != nil || abs == "" {
		return nil, fmt.Errorf("invalid path %q", pathInput)
	}
	absStr := abs.String()

	relPath := ""
	if rel, relErr := vaultPaths.RelCodeStrict(absStr); relErr == nil {
		relPath = string(paths.NormalizeCode(rel.String()))
	}
	if relPath == "" {
		relPath = absStr
	}

	info, statErr := os.Stat(absStr)
	isDir := statErr == nil && info.IsDir()
	if isDir {
		prefix := relPath
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		rationale, err := store.RationaleForPathPrefix(ctx, prefix, kinds)
		if err != nil {
			return nil, fmt.Errorf("rationale query: %w", err)
		}
		return toAgentCodeRationaleEntries(rationale), nil
	}

	rationale, err := store.RationaleForPath(ctx, relPath)
	if err != nil {
		return nil, fmt.Errorf("rationale query: %w", err)
	}
	if len(kinds) > 0 {
		kindSet := make(map[string]struct{}, len(kinds))
		for _, kind := range kinds {
			kindSet[kind] = struct{}{}
		}
		filtered := rationale[:0]
		for _, row := range rationale {
			if _, ok := kindSet[string(row.Kind)]; ok {
				filtered = append(filtered, row)
			}
		}
		rationale = filtered
	}
	return toAgentCodeRationaleEntries(rationale), nil
}

func toAgentCodeRationaleEntries(rows []codeanchor.Rationale) []agentCodeRationaleEntry {
	out := make([]agentCodeRationaleEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, agentCodeRationaleEntry{
			ID:          row.ID,
			Path:        row.Path,
			SymbolFQN:   row.SymbolFQN,
			Kind:        string(row.Kind),
			Content:     row.Content,
			StartLine:   row.StartLine,
			EndLine:     row.EndLine,
			Fingerprint: row.Fingerprint,
		})
	}
	return out
}
