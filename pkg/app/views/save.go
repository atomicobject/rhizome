package views

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// ErrSaveConflict means the view changed since the client loaded it.
var ErrSaveConflict = errors.New("view changed since it was loaded")

// SaveRequest carries the view state Save view writes (SPEC-0112).
type SaveRequest struct {
	// DefinitionFingerprint is the definition the client loaded; when set, a
	// different current definition refuses the save.
	DefinitionFingerprint string    `json:"definitionFingerprint,omitempty"`
	State                 SaveState `json:"state"`
}

// SaveState is the view state to save. Variant, search, filters, and sort
// are written as given, empty ones removing their keys. Group, columns,
// density, columnField, and laneField leave the view's setting unchanged when
// absent. A group with an empty field or "none" saves an explicit "no
// grouping" (`group: {field: none}`) that no default grouping overrides; an
// empty density or laneField removes the key (the default applies), and
// laneField "none" turns lanes off.
type SaveState struct {
	Variant     string                  `json:"variant,omitempty"`
	Search      string                  `json:"search,omitempty"`
	Filters     []viewconfig.FilterSpec `json:"filters,omitempty"`
	Sort        []viewconfig.SortSpec   `json:"sort,omitempty"`
	Group       *viewconfig.GroupSpec   `json:"group,omitempty"`
	Columns     []viewconfig.ViewColumn `json:"columns,omitempty"`
	Density     *string                 `json:"density,omitempty"`
	ColumnField *string                 `json:"columnField,omitempty"`
	LaneField   *string                 `json:"laneField,omitempty"`
}

type SaveResponse struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Created bool   `json:"created"`
}

// Save writes view state to YAML under .rhizome/views. A generated view
// becomes a new native view that replaces the generated layouts of its type
// or interface; an authored view's file gets its defaults and variants
// rewritten in place. The result must pass the checks `rzm validate views`
// runs, and the write is atomic and confined to the views folder. The next
// catalog read serves it, since the catalog loads definitions per request.
func (s *Service) Save(ctx context.Context, id string, req SaveRequest) (SaveResponse, error) {
	// ponytail: one process-wide lock serializes the whole load-check-write so
	// two saves cannot both pass the replacing-view check; per-folder locks if
	// saves ever contend.
	saveMu.Lock()
	defer saveMu.Unlock()
	if s.opts.VaultPath == "" {
		return SaveResponse{}, fmt.Errorf("%w: saving views needs a vault", ErrInvalidRequest)
	}
	entry, err := s.entry(ctx, id)
	if err != nil {
		return SaveResponse{}, err
	}
	def := entry.Definition
	if def.SourceSpec.Kind == viewconfig.SourceKindCustom {
		return SaveResponse{}, fmt.Errorf("%w: custom view %s has no native layout to save", ErrInvalidRequest, id)
	}
	if fp := strings.TrimSpace(req.DefinitionFingerprint); fp != "" && fp != fingerprintDefinition(def) {
		return SaveResponse{}, fmt.Errorf("%w: %s", ErrSaveConflict, id)
	}
	layout := savedLayout(req.State)
	switch layout.Variant {
	case "", "table", "card", "kanban":
	default:
		return SaveResponse{}, fmt.Errorf("%w: unsupported variant %q", ErrInvalidRequest, layout.Variant)
	}
	root := viewconfig.DefaultSourceRoot(s.opts.VaultPath)
	if entry.Generated {
		return s.saveGenerated(ctx, entry, layout, root)
	}
	return s.saveAuthored(ctx, def, layout, root)
}

// checkSaveState runs the state through execution so Save refuses what the
// runtime would: board column and lane fields, filter operators such as
// contains on a link, group buckets, and unresolvable link filter values.
func (s *Service) checkSaveState(ctx context.Context, id string, layout viewconfig.SavedLayout) error {
	_, err := s.Execute(ctx, id, ExecuteRequest{
		Variant:     layout.Variant,
		Search:      layout.Search,
		Filters:     append([]viewconfig.FilterSpec{}, layout.Filters...),
		Sort:        append([]viewconfig.SortSpec{}, layout.Sort...),
		Group:       layout.Group,
		ColumnField: valueOrEmpty(layout.ColumnField),
		LaneField:   valueOrEmpty(layout.LaneField),
		Page:        PageRequest{First: 1},
		PageSet:     true,
	})
	if err != nil {
		return err
	}
	// A board column or lane field saved from another variant must still work
	// when the reader switches to Board.
	if layout.Variant != "kanban" && (valueOrEmpty(layout.ColumnField) != "" || valueOrEmpty(layout.LaneField) != "") {
		_, err = s.Execute(ctx, id, ExecuteRequest{
			Variant:     "kanban",
			Filters:     append([]viewconfig.FilterSpec{}, layout.Filters...),
			ColumnField: valueOrEmpty(layout.ColumnField),
			LaneField:   valueOrEmpty(layout.LaneField),
			Page:        PageRequest{First: 1},
			PageSet:     true,
		})
	}
	return err
}

func savedLayout(state SaveState) viewconfig.SavedLayout {
	layout := viewconfig.SavedLayout{
		Variant:     strings.TrimSpace(state.Variant),
		Search:      strings.TrimSpace(state.Search),
		Filters:     state.Filters,
		Sort:        state.Sort,
		Group:       state.Group,
		Density:     trimmed(state.Density),
		ColumnField: trimmed(state.ColumnField),
		LaneField:   trimmed(state.LaneField),
	}
	if group := state.Group; group != nil && len(normalizedGroupFields(group)) == 0 {
		layout.Group = &viewconfig.GroupSpec{Field: viewconfig.GroupNone}
	}
	for _, column := range state.Columns {
		if field := strings.TrimSpace(column.Field); field != "" {
			layout.Columns = append(layout.Columns, viewconfig.ViewColumn{Field: field, Label: strings.TrimSpace(column.Label)})
		}
	}
	return layout
}

func trimmed(value *string) *string {
	if value == nil {
		return nil
	}
	out := strings.TrimSpace(*value)
	return &out
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// saveGenerated creates a native view mounted on the generated view's type or
// interface with replaceGenerated, carrying its card spec. When a view already
// replaces the generated one, the client loaded a stale catalog: it conflicts
// rather than adding a second replacement that validation would warn about.
func (s *Service) saveGenerated(ctx context.Context, entry CatalogEntry, layout viewconfig.SavedLayout, root string) (SaveResponse, error) {
	generated := entry.Definition
	if replacing, err := s.replacingView(ctx, generated); err != nil {
		return SaveResponse{}, err
	} else if replacing != nil {
		return SaveResponse{}, fmt.Errorf("%w: %s already replaces the generated %s view; reload and save that view instead", ErrSaveConflict, s.vaultRelative(replacing.Definition.Source.Path), sourceSubject(generated))
	}
	def := viewconfig.ViewDefinition{
		APIVersion: viewconfig.APIVersion,
		Name:       entry.Name,
		SourceSpec: generated.SourceSpec,
		Mount:      viewconfig.MountSpec{Kind: generated.Mount.Kind, Type: generated.Mount.Type, Interface: generated.Mount.Interface, ReplaceGenerated: true},
		Defaults: viewconfig.DefaultsSpec{
			Variant: firstNonEmpty(layout.Variant, generated.Defaults.Variant),
			First:   generated.Defaults.First,
			Search:  layout.Search,
			Filters: layout.Filters,
			Sort:    layout.Sort,
			Group:   layout.Group,
		},
		Variants: viewconfig.VariantSet{
			Table: &viewconfig.TableVariant{Columns: layout.Columns, Density: valueOrEmpty(layout.Density)},
			Card:  generated.Variants.Card,
		},
	}
	// Settings the state leaves unset keep the generated view's.
	if def.Defaults.Group == nil {
		def.Defaults.Group = generated.Defaults.Group
	}
	if len(def.Variants.Table.Columns) == 0 && generated.Variants.Table != nil {
		def.Variants.Table.Columns = generated.Variants.Table.Columns
	}
	if kanban := generated.Variants.Kanban; kanban != nil {
		def.Variants.Kanban = &viewconfig.KanbanVariant{ColumnField: firstNonEmpty(valueOrEmpty(layout.ColumnField), kanban.ColumnField), LaneField: valueOrEmpty(layout.LaneField)}
	}
	existing, _ := s.loadDefinitions()
	def.ID, def.Source.Path = s.freeViewSlot(root, sourceSubject(generated), existing)
	data, err := viewconfig.Marshal(def)
	if err != nil {
		return SaveResponse{}, err
	}
	if err := s.validateViewFile(existing, def.Source.Path, data); err != nil {
		return SaveResponse{}, err
	}
	if err := s.checkSaveState(ctx, entry.ID, layout); err != nil {
		return SaveResponse{}, err
	}
	if err := ensureViewsFolder(s.opts.VaultPath, root); err != nil {
		return SaveResponse{}, err
	}
	if err := s.writeViewFile(def.Source.Path, data, nil); err != nil {
		return SaveResponse{}, err
	}
	return SaveResponse{ID: def.ID, Path: s.vaultRelative(def.Source.Path), Created: true}, nil
}

// saveAuthored rewrites the defaults and variants of the view's own file.
func (s *Service) saveAuthored(ctx context.Context, def viewconfig.ViewDefinition, layout viewconfig.SavedLayout, root string) (SaveResponse, error) {
	path := def.Source.Path
	if err := confineViewFile(s.opts.VaultPath, root, path, false); err != nil {
		return SaveResponse{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return SaveResponse{}, err
	}
	edited, err := viewconfig.ApplySavedLayout(data, layout)
	if err != nil {
		return SaveResponse{}, fmt.Errorf("%w: %v", ErrInvalidView, err)
	}
	existing, _ := s.loadDefinitions()
	if err := s.validateViewFile(existing, path, edited); err != nil {
		return SaveResponse{}, err
	}
	if err := s.checkSaveState(ctx, def.ID, layout); err != nil {
		return SaveResponse{}, err
	}
	if err := s.writeViewFile(path, edited, data); err != nil {
		return SaveResponse{}, err
	}
	return SaveResponse{ID: def.ID, Path: s.vaultRelative(path), Created: false}, nil
}

// replacingView returns the view that takes the generated view's slot, as
// target resolution picks it: visible, valid, first by order, then ID.
func (s *Service) replacingView(ctx context.Context, generated viewconfig.ViewDefinition) (*CatalogEntry, error) {
	catalog, err := s.catalog(ctx, true)
	if err != nil {
		return nil, err
	}
	var found *CatalogEntry
	for i, entry := range catalog.Views {
		if entry.Generated || entry.Mount.Hidden || hasBlockingIssues(entry.Issues) || !viewconfig.ReplacesGenerated(entry.Definition) ||
			!viewconfig.MatchesMount(entry.Mount, generated.Mount.Kind, sourceSubject(generated)) {
			continue
		}
		if found == nil || entry.Mount.Order < found.Mount.Order || (entry.Mount.Order == found.Mount.Order && entry.ID < found.ID) {
			found = &catalog.Views[i]
		}
	}
	return found, nil
}

// validateViewFile validates data as the view file at path among the other
// loaded views, refusing any issue `rzm validate views` would report on that
// file, warnings included.
func (s *Service) validateViewFile(existing []viewconfig.ViewDefinition, path string, data []byte) error {
	decoded, issues := viewconfig.Decode(data, path)
	if len(issues) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidView, issues[0].Message)
	}
	defs := make([]viewconfig.ViewDefinition, 0, len(existing)+1)
	for _, def := range existing {
		if def.Source.Path != path {
			defs = append(defs, def)
		}
	}
	defs = append(defs, decoded...)
	for _, issue := range viewconfig.Validate(defs, s.validateOptions()).Issues {
		if issue.Path == path {
			return fmt.Errorf("%w: %s: %s", ErrInvalidView, issue.Field, issue.Message)
		}
	}
	return nil
}

// writeViewFile writes data to a view file confined to the views folder.
// With expected nil it creates the file, failing when another save created
// that name first; otherwise it replaces the file only while it still holds
// expected, checked under a per-file lock immediately before the rename.
func (s *Service) writeViewFile(path string, data, expected []byte) error {
	create := expected == nil
	if err := confineViewFile(s.opts.VaultPath, viewconfig.DefaultSourceRoot(s.opts.VaultPath), path, create); err != nil {
		return err
	}
	unlock := lockViewFile(path)
	defer unlock()
	if create {
		return s.createViewFile(path, data)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, expected) {
		return fmt.Errorf("%w: %s changed while saving", ErrSaveConflict, s.vaultRelative(path))
	}
	return obsidian.WriteFileAtomicPreservingMode(path, data, 0o644)
}

// viewFileLocks serializes the check-and-rename of in-place saves per file.
// ponytail: process-local; an editor outside rzm can still write between the
// check and the rename, which only a filesystem lock would close.
var (
	viewFileLocks sync.Map
	saveMu        sync.Mutex
)

func lockViewFile(path string) func() {
	value, _ := viewFileLocks.LoadOrStore(path, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// createViewFile writes a complete temporary file and hard-links it into
// place, so readers never see a partial file and a name another save took
// first is never replaced.
func (s *Service) createViewFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".save-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(0o644)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Link(tmp.Name(), path); errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: %s already exists", ErrSaveConflict, s.vaultRelative(path))
	} else if err != nil {
		return err
	}
	return nil
}

// freeViewSlot picks an id and file name from the subject's name that no
// loaded view or existing file uses: task, task-2, and so on.
func (s *Service) freeViewSlot(root, subject string, existing []viewconfig.ViewDefinition) (string, string) {
	base := strings.ReplaceAll(strings.ToLower(ontology.HumanizeFieldName(subject)), " ", "-")
	base = strings.Trim(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return -1
	}, base), "-")
	if base == "" {
		base = "view"
	}
	ids := map[string]bool{}
	for _, def := range existing {
		ids[def.ID] = true
	}
	for n := 1; ; n++ {
		slug := base
		if n > 1 {
			slug = fmt.Sprintf("%s-%d", base, n)
		}
		path := filepath.Join(root, slug+".yaml")
		if _, err := os.Lstat(path); err == nil || ids[slug] {
			continue
		}
		if _, err := os.Lstat(filepath.Join(root, slug+".yml")); err == nil {
			continue
		}
		return slug, path
	}
}

func (s *Service) vaultRelative(path string) string {
	rel, err := filepath.Rel(s.opts.VaultPath, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// ensureViewsFolder creates .rhizome/views when missing. It refuses a
// symlinked or non-folder component before creating anything, so a folder
// that resolves outside the vault is never created there.
func ensureViewsFolder(vaultPath, root string) error {
	rel, err := filepath.Rel(vaultPath, root)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return fmt.Errorf("%w: views folder is outside the vault", ErrInvalidRequest)
	}
	dir := vaultPath
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		dir = filepath.Join(dir, part)
		info, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%w: %s is a symlink or not a folder", ErrInvalidRequest, dir)
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	return confineViewFile(vaultPath, root, filepath.Join(root, "probe.yaml"), true)
}

// confineViewFile refuses a view file outside the vault's .rhizome/views
// folder, reached through a symlink, or not a regular file. When create is
// set the file may be absent.
func confineViewFile(vaultPath, root, path string, create bool) error {
	realVault, err := filepath.EvalSymlinks(vaultPath)
	if err != nil {
		return err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	if realRoot != filepath.Join(realVault, ".rhizome", "views") {
		return fmt.Errorf("%w: views folder resolves outside the vault", ErrInvalidRequest)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return fmt.Errorf("%w: %s is outside .rhizome/views", ErrInvalidRequest, path)
	}
	realDir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil || realDir != filepath.Join(realRoot, filepath.Dir(rel)) {
		return fmt.Errorf("%w: %s is reached through a symlink", ErrInvalidRequest, path)
	}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist) && create:
		return nil
	case err != nil:
		return err
	case !info.Mode().IsRegular():
		return fmt.Errorf("%w: %s is not a regular file", ErrInvalidRequest, path)
	}
	return nil
}
