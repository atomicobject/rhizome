package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	appviews "github.com/atomicobject/rhizome/pkg/app/views"
	"github.com/atomicobject/rhizome/pkg/app/web"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

type viewOptions struct {
	path        string
	id          string
	variant     string
	search      string
	filtersJSON string
	sortJSON    string
	group       string
	offset      int
	first       int
	inputs      []string
	inputsJSON  string
	inputsFile  string
	jsonOutput  bool
	// omitCapabilities drops the field capability catalog from run output.
	omitCapabilities bool
}

type viewValidateResponse struct {
	OK         bool               `json:"ok"`
	IssueCount int                `json:"issueCount"`
	Catalog    appviews.Catalog   `json:"catalog"`
	Issues     []viewconfig.Issue `json:"issues,omitempty"`
}

func newViewCmd(agent bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view",
		Short: "List, validate, show, run, and eject configured views",
	}
	cmd.AddCommand(newViewListCmd(agent, &viewOptions{}))
	cmd.AddCommand(newViewShowCmd(agent, &viewOptions{}))
	cmd.AddCommand(newViewValidateCmd(agent, &viewOptions{}))
	cmd.AddCommand(newViewRunCmd(agent, &viewOptions{}))
	cmd.AddCommand(newViewEjectCmd(agent, &viewOptions{}))
	return cmd
}

func newViewListCmd(agent bool, opts *viewOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured views",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, cleanup, err := buildViewServiceForCommand(cmd, opts)
			if cleanup != nil {
				defer cleanup()
			}
			if err != nil {
				return writeViewCommandError(agent, err)
			}
			catalog, err := service.Catalog(cmd.Context())
			if err != nil {
				return writeViewCommandError(agent, err)
			}
			if useViewJSON(agent, opts.jsonOutput) {
				return writeJSON(cmd.OutOrStdout(), catalog)
			}
			return renderViewListHuman(cmd.OutOrStdout(), catalog)
		},
	}
	addViewPathFlag(cmd, opts)
	addViewJSONFlag(cmd, agent, opts)
	return cmd
}

func newViewShowCmd(agent bool, opts *viewOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show a configured view",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, cleanup, err := buildViewServiceForCommand(cmd, opts)
			if cleanup != nil {
				defer cleanup()
			}
			if err != nil {
				return writeViewCommandError(agent, err)
			}
			entry, err := service.View(cmd.Context(), args[0])
			if err != nil {
				return writeViewCommandError(agent, err)
			}
			if useViewJSON(agent, opts.jsonOutput) {
				return writeJSON(cmd.OutOrStdout(), entry)
			}
			renderViewShowHuman(cmd.OutOrStdout(), entry)
			return nil
		},
	}
	addViewPathFlag(cmd, opts)
	addViewJSONFlag(cmd, agent, opts)
	return cmd
}

func newViewValidateCmd(agent bool, opts *viewOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate configured views",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, cleanup, err := buildViewServiceForCommand(cmd, opts)
			if cleanup != nil {
				defer cleanup()
			}
			if err != nil {
				return writeViewCommandError(agent, err)
			}
			catalog, err := service.Catalog(cmd.Context())
			if err != nil {
				return writeViewCommandError(agent, err)
			}
			resp := viewValidateResponse{OK: len(catalog.Issues) == 0, IssueCount: len(catalog.Issues), Catalog: catalog, Issues: catalog.Issues}
			if useViewJSON(agent, opts.jsonOutput) {
				if err := writeJSON(cmd.OutOrStdout(), resp); err != nil {
					return err
				}
			} else if err := renderViewValidateHuman(cmd.OutOrStdout(), catalog); err != nil {
				return err
			}
			if len(catalog.Issues) > 0 {
				return silentExitError{code: 1}
			}
			return nil
		},
	}
	addViewPathFlag(cmd, opts)
	addViewJSONFlag(cmd, agent, opts)
	return cmd
}

func newViewRunCmd(agent bool, opts *viewOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute a configured view",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, cleanup, err := buildViewServiceForCommand(cmd, opts)
			if cleanup != nil {
				defer cleanup()
			}
			if err != nil {
				return writeViewCommandError(agent, err)
			}
			req, err := buildViewExecuteRequest(cmd, opts)
			if err != nil {
				return writeViewCommandError(agent, err)
			}
			resp, err := service.Execute(cmd.Context(), opts.id, req)
			if err != nil {
				return writeViewCommandError(agent, err)
			}
			if opts.omitCapabilities {
				resp.Capabilities = nil
			}
			return writeJSON(cmd.OutOrStdout(), resp)
		},
	}
	addViewPathFlag(cmd, opts)
	cmd.Flags().StringVar(&opts.id, "id", "", "view id to execute")
	cmd.Flags().StringVar(&opts.variant, "variant", "", "variant to execute (table, card, or kanban)")
	cmd.Flags().StringVar(&opts.search, "search", "", "case-insensitive search")
	cmd.Flags().StringVar(&opts.filtersJSON, "filters-json", "", "JSON array of structured filters")
	cmd.Flags().StringVar(&opts.sortJSON, "sort-json", "", "JSON array of sort specs")
	cmd.Flags().StringVar(&opts.group, "group", "", "field to group by")
	cmd.Flags().IntVar(&opts.offset, "offset", 0, "pagination offset")
	cmd.Flags().IntVar(&opts.first, "first", 0, "page size")
	cmd.Flags().StringArrayVar(&opts.inputs, "input", nil, "named source input as key=value (repeatable)")
	cmd.Flags().StringVar(&opts.inputsJSON, "inputs-json", "", "JSON object of named source inputs")
	cmd.Flags().StringVar(&opts.inputsFile, "inputs-file", "", "path to JSON object of named source inputs")
	cmd.Flags().BoolVar(&opts.omitCapabilities, "omit-capabilities", false, "leave the field capability catalog out of the response")
	return cmd
}

func addViewPathFlag(cmd *cobra.Command, opts *viewOptions) {
	cmd.Flags().StringVar(&opts.path, "path", "", "view YAML file or directory (defaults to .rhizome/views)")
}

func addViewJSONFlag(cmd *cobra.Command, agent bool, opts *viewOptions) {
	if agent {
		return
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "emit JSON instead of the human-friendly rendering")
}

func useViewJSON(agent, jsonFlag bool) bool {
	return agent || jsonFlag
}

func buildViewServiceForCommand(cmd *cobra.Command, opts *viewOptions) (*appviews.Service, func(), error) {
	return buildViewService(cmd.Context(), opts.path)
}

// buildViewService is shared by Cobra and the persistent code-mode adapter.
// It resolves the current vault and projection for every request so a
// long-lived process does not retain stale configuration or note state.
func buildViewService(ctx context.Context, path string) (*appviews.Service, func(), error) {
	var defs []viewconfig.ViewDefinition
	var loadIssues []viewconfig.Issue
	if strings.TrimSpace(path) != "" {
		defs, loadIssues = viewconfig.LoadPath(path)
		if defs == nil {
			defs = []viewconfig.ViewDefinition{}
		}
	}

	vaultDef, err := vaultDefOrDefaultContext(ctx)
	if err != nil && strings.TrimSpace(path) == "" {
		return nil, nil, err
	}
	vaultPath := vaultDef.BasePath()
	if vaultPath == "" {
		vaultDef = obsidian.VaultDefinition{}
	}

	schema, execSchema := loadViewSchemas(vaultPath)
	plan := oneshotruntime.ViewPlan()
	store, cleanup, err := openOptionalIntelStoreForPlan(vaultPath, plan)
	if err != nil {
		return nil, cleanup, err
	}
	formats, err := newNoteFormatRuntime()
	if err != nil {
		return nil, cleanup, err
	}
	return newViewService(vaultDef, store, schema, execSchema, formats, defs, loadIssues), cleanup, nil
}

// newViewService assembles a view service over an already-open store. The
// vault runtime reuses it with its own store and cached schema.
func newViewService(vaultDef obsidian.VaultDefinition, store *semdb.Store, schema *ontology.Schema, execSchema *ontologyquery.ExecutableSchema, formats noteformat.Runtime, defs []viewconfig.ViewDefinition, loadIssues []viewconfig.Issue) *appviews.Service {
	vaultPath := vaultDef.BasePath()
	var viewStore noderead.Store
	var queryStore ontologyquery.Store
	if store != nil {
		viewStore = store
		queryStore = store
	}
	noteReader := &obsidian.Note{}
	return appviews.New(appviews.ServiceOptions{
		VaultPath:  vaultPath,
		VaultDef:   vaultDef,
		NoteReader: noteReader,
		Store:      viewStore,
		Schema:     schema,
		ExecSchema: execSchema,
		QueryDeps: ontologyquery.Deps{
			NoteFormats: formats,
			VaultDef:    vaultDef,
			NoteReader:  noteReader,
			Store:       queryStore,
			Service:     ontology.NewService(vaultDef, noteReader, store, schema),
		},
		Views:      defs,
		LoadIssues: loadIssues,
		Bundled:    web.BundledViews(web.Assets()),
	})
}

func loadViewSchemas(vaultPath string) (*ontology.Schema, *ontologyquery.ExecutableSchema) {
	if strings.TrimSpace(vaultPath) == "" {
		return nil, nil
	}
	schema, execSchema, err := loadOntologyExecutableSchema(vaultPath)
	if err != nil {
		return nil, nil
	}
	return schema, execSchema
}

func buildViewExecuteRequest(cmd *cobra.Command, opts *viewOptions) (appviews.ExecuteRequest, error) {
	inputs, err := parseRecipeInputs(opts.inputs, opts.inputsJSON, opts.inputsFile)
	if err != nil {
		return appviews.ExecuteRequest{}, err
	}
	req := appviews.ExecuteRequest{
		Variant: opts.variant,
		Search:  opts.search,
		Page:    appviews.PageRequest{Offset: opts.offset, First: opts.first},
		Inputs:  inputs,
	}
	if cmd != nil {
		req.PageSet = cmd.Flags().Changed("offset") || cmd.Flags().Changed("first")
	}
	if strings.TrimSpace(opts.filtersJSON) != "" {
		if err := decodeViewJSON(opts.filtersJSON, &req.Filters); err != nil {
			return appviews.ExecuteRequest{}, fmt.Errorf("--filters-json: %w", err)
		}
	}
	if strings.TrimSpace(opts.sortJSON) != "" {
		if err := decodeViewJSON(opts.sortJSON, &req.Sort); err != nil {
			return appviews.ExecuteRequest{}, fmt.Errorf("--sort-json: %w", err)
		}
	}
	if strings.TrimSpace(opts.group) != "" {
		req.Group = &viewconfig.GroupSpec{Field: strings.TrimSpace(opts.group)}
	}
	return req, nil
}

func decodeViewJSON(raw string, target any) error {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(target)
}

func renderViewListHuman(w io.Writer, catalog appviews.Catalog) error {
	if len(catalog.Views) == 0 {
		_, err := fmt.Fprintln(w, "No configured views found. Add YAML files under .rhizome/views/.")
		return err
	}
	_, _ = fmt.Fprintln(w, "ID\tNAME\tMOUNT\tSOURCE")
	views := append([]appviews.CatalogEntry(nil), catalog.Views...)
	sort.SliceStable(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	for _, view := range views {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", view.ID, view.Name, view.Mount.Kind, view.Source.Kind)
	}
	_, err := fmt.Fprintf(w, "%d view(s)\n", len(views))
	return err
}

func renderViewShowHuman(w io.Writer, entry appviews.CatalogEntry) {
	_, _ = fmt.Fprintf(w, "# %s (%s)\n\n", entry.Name, entry.ID)
	_, _ = fmt.Fprintf(w, "Source: %s\n", entry.Source.Kind)
	_, _ = fmt.Fprintf(w, "Mount: %s\n", entry.Mount.Kind)
	_, _ = fmt.Fprintf(w, "Default variant: %s\n", entry.Defaults.Variant)
	_, _ = fmt.Fprintf(w, "Available variants: %s\n", strings.Join(entry.AvailableVariants, ", "))
	if entry.Generated {
		_, _ = fmt.Fprintln(w, "Generated: true")
	}
}

func renderViewValidateHuman(w io.Writer, catalog appviews.Catalog) error {
	if len(catalog.Issues) == 0 {
		_, err := fmt.Fprintf(w, "OK - %d view(s) validated\n", len(catalog.Views))
		return err
	}
	for _, issue := range catalog.Issues {
		_, _ = fmt.Fprintf(w, "%s", issue.Code)
		if issue.View != "" {
			_, _ = fmt.Fprintf(w, " [%s]", issue.View)
		}
		if issue.Path != "" {
			_, _ = fmt.Fprintf(w, " %s", issue.Path)
		}
		if issue.Line > 0 {
			_, _ = fmt.Fprintf(w, ":%d", issue.Line)
		}
		if issue.Field != "" {
			_, _ = fmt.Fprintf(w, " field %s", issue.Field)
		}
		_, _ = fmt.Fprintf(w, " - %s\n", issue.Message)
	}
	_, err := fmt.Fprintf(w, "FAIL - %d issue(s)\n", len(catalog.Issues))
	return err
}

func writeViewCommandError(agent bool, err error) error {
	if agent {
		writeAgentError(err)
		return silentExitError{code: 1}
	}
	return err
}

func init() {
	cmd := newViewCmd(false)
	cmd.PersistentFlags().StringVarP(&vaultName, "vault", "v", "", "vault name (uses default if unset)")
	rootCmd.AddCommand(cmd)
}
