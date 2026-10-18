package cmd

import (
	"context"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/validationproduct"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

func (commandOptions *ontologyInvocationOptions) makeOntologyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ontology",
		Short: "Validate and inspect note ontology",
	}
}

func (commandOptions *ontologyInvocationOptions) makeOntologyValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate ontology schema and typed notes",
		RunE: func(cmd *cobra.Command, args []string) error {
			vaultDef, err := commandOptions.vaultDefOrDefaultContext(cmd.Context())
			if err != nil {
				return err
			}
			note := &obsidian.Note{}
			noteMetadata, err := newNoteMetadataIndexer()
			if err != nil {
				return err
			}
			projection, err := validationproduct.RefreshValidationProjection(cmd.Context(), validationproduct.ValidationProjectionRequest{
				VaultPath: vaultDef.BasePath(), VaultDef: vaultDef, NoteMetadata: noteMetadata, NoteReader: note,
				Target: validationproduct.ValidationProjectionLive,
			})
			if err != nil {
				return err
			}
			defer projection.Close()
			runtime := projection.Runtime

			if runtime == nil || runtime.Schema == nil {
				if runtime != nil && len(runtime.Issues) > 0 {
					return printOntologyRuntimeError(cmd, runtime, fmt.Errorf("ontology validation failed with %d issue(s)", len(runtime.Issues)))
				}
				fmt.Fprintln(cmd.OutOrStdout(), "No ontology files found.")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Schema: %s\n", runtime.Schema.Hash)
			files := make([]string, 0, len(runtime.Schema.Files))
			for _, file := range runtime.Schema.Files {
				files = append(files, ontologyPathBase(file))
			}
			sort.Strings(files)
			fmt.Fprintf(cmd.OutOrStdout(), "Files: %s\n", strings.Join(files, ", "))
			fmt.Fprintf(cmd.OutOrStdout(), "Types: %d\n", len(runtime.Schema.Types))
			if err := printOntologyTypeInventory(cmd.Context(), cmd, vaultDef, note, runtime); err != nil {
				return err
			}
			printOntologyIssues(cmd, runtime.Issues)
			if len(runtime.Issues) > 0 {
				return fmt.Errorf("ontology validation failed with %d issue(s)", len(runtime.Issues))
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Ontology valid.")
			return nil
		},
	}
}

func (commandOptions *ontologyInvocationOptions) makeOntologyWalkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "walk <note>",
		Short: "Walk the local ontology neighborhood for a typed note",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vaultDef, err := commandOptions.vaultDefOrDefaultContext(cmd.Context())
			if err != nil {
				return err
			}
			vaultPaths, _ := paths.NewVaultPaths(vaultDef.BasePath())
			rel, _, err := paths.ResolveNotePathInputWithVaultPaths(vaultPaths, args[0])
			if err != nil {
				return err
			}

			note := &obsidian.Note{}
			noteMetadata, err := newNoteMetadataIndexer()
			if err != nil {
				return err
			}
			projection, err := validationproduct.RefreshValidationProjection(cmd.Context(), validationproduct.ValidationProjectionRequest{
				VaultPath: vaultDef.BasePath(), VaultDef: vaultDef, NoteMetadata: noteMetadata, NoteReader: note,
				Target: validationproduct.ValidationProjectionLive,
			})
			if err != nil {
				return err
			}
			defer projection.Close()
			runtime := projection.Runtime
			if runtime == nil || runtime.Store == nil || runtime.Schema == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "No ontology files found.")
				return nil
			}

			scope := noderead.NewService(vaultDef, note, runtime.Store, runtime.Schema).NewScope(noderead.ScopeOptions{})
			result, err := scope.Walk(cmd.Context(), rel.String(), noderead.WalkOptions{
				MaxDepth:       commandOptions.ontologyWalkDepth,
				RelationFilter: commandOptions.ontologyWalkRelation,
				IncludeAmbient: commandOptions.ontologyWalkIncludeAmbient,
			})
			if err != nil {
				return err
			}
			if result == nil || result.SeedType == "" {
				fmt.Fprintf(cmd.OutOrStdout(), "No typed ontology node found for %s\n", rel.String())
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Seed: %s\n", rel.String())
			fmt.Fprintf(cmd.OutOrStdout(), "Type: %s\n", result.SeedType)
			fmt.Fprintln(cmd.OutOrStdout(), "\nNodes:")
			for _, node := range result.Nodes {
				fmt.Fprintf(cmd.OutOrStdout(), "- %s [%s]\n", node.Path, node.TypeName)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "\nEdges:")
			for _, edge := range result.Edges {
				mode := "ambient"
				if edge.Structural {
					mode = "structural"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "- %s --%s [%s/%s]--> %s\n", edge.Source, edge.RelationName, mode, edge.Provenance, edge.Destination)
			}
			return nil
		},
	}
}

func printOntologyIssues(cmd *cobra.Command, issues []ontology.ValidationIssue) {
	if len(issues) == 0 {
		return
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Issues:")
	for _, issue := range issues {
		location := issue.NotePath
		if location == "" {
			location = issue.TypeName
		}
		if location == "" {
			location = "<schema>"
		}
		field := ""
		if strings.TrimSpace(issue.FieldName) != "" {
			field = " field=" + issue.FieldName
		}
		line := ""
		if issue.Line > 0 {
			line = fmt.Sprintf(" line=%d", issue.Line)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "- [%s] %s%s%s: %s\n", issue.Code, location, field, line, issue.Message)
	}
}

func printOntologyRuntimeError(cmd *cobra.Command, runtime *ontology.Runtime, err error) error {
	if runtime != nil {
		printOntologyIssues(cmd, runtime.Issues)
	}
	return err
}

func ontologyPathBase(path string) string {
	parts := strings.Split(strings.ReplaceAll(path, "\\", "/"), "/")
	return parts[len(parts)-1]
}

func printOntologyTypeInventory(ctx context.Context, cmd *cobra.Command, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, runtime *ontology.Runtime) error {
	if runtime == nil || runtime.Schema == nil || runtime.Store == nil {
		return nil
	}
	scope := noderead.NewService(vaultDef, noteMgr, runtime.Store, runtime.Schema).NewScope(noderead.ScopeOptions{})
	typeNames := make([]string, 0, len(runtime.Schema.Types))
	for typeName, typeDef := range runtime.Schema.Types {
		if typeDef == nil {
			continue
		}
		if typeDef.Role != ontoschema.TypeRoleNote && typeDef.Role != ontoschema.TypeRoleEmbeddedNode {
			continue
		}
		typeNames = append(typeNames, typeName)
	}
	sort.Strings(typeNames)

	fmt.Fprintln(cmd.OutOrStdout(), "Resolved notes by type:")
	for _, typeName := range typeNames {
		result, err := scope.TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: typeName})
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "- %s (%d)\n", typeName, result.Count)
		if len(result.Items) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "  (none)")
			continue
		}
		for _, item := range result.Items {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", item.Ref.String())
		}
	}
	return nil
}

func (commandOptions *ontologyInvocationOptions) registerOntologyCommands(ontologyWalkCmd, ontologyCmd, ontologyValidateCmd, rootCmd *cobra.Command) {
	ontologyWalkCmd.Flags().IntVar(&commandOptions.ontologyWalkDepth, "depth", 2, "max ontology walk depth")
	ontologyWalkCmd.Flags().StringVar(&commandOptions.ontologyWalkRelation, "relation", "", "filter to a specific relation name")
	ontologyWalkCmd.Flags().BoolVar(&commandOptions.ontologyWalkIncludeAmbient, "include-ambient", true, "include ambient body-link/backlink edges")

	ontologyCmd.AddCommand(ontologyValidateCmd)
	ontologyCmd.AddCommand(ontologyWalkCmd)
	ontologyCmd.PersistentFlags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name (uses default if unset)")
	rootCmd.AddCommand(ontologyCmd)
}
