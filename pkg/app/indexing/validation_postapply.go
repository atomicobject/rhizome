package indexing

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// ValidationProjectionPostApplyRefresher adapts validation's borrow-only
// repair lease to the validation projection's lock-already-held core.
// It never acquires or releases the index lock.
type ValidationProjectionPostApplyRefresher struct {
	VaultPath    string
	VaultDef     obsidian.VaultDefinition
	NoteMetadata notemeta.Indexer
	NoteReader   obsidian.NoteReader
}

// Refresh converges the exact committed repair delta and transfers the
// projection runtime's cleanup ownership to the repair engine.
func (r ValidationProjectionPostApplyRefresher) Refresh(
	ctx context.Context,
	lease *validate.IndexLockLease,
	changed []string,
	renamed []validate.PathRename,
	deleted []string,
) (validate.PostApplyRefreshResult, error) {
	return r.refresh(ctx, lease, changed, renamed, deleted, postApplyAnchorChangedPaths(changed, renamed))
}

// RefreshScoped refreshes code-anchor declarations only when the committed
// repair owns that check or changes note-path ownership.
func (r ValidationProjectionPostApplyRefresher) RefreshScoped(
	ctx context.Context,
	lease *validate.IndexLockLease,
	scope validate.PostApplyRefreshScope,
) (validate.PostApplyRefreshResult, error) {
	anchorChanged := postApplyAnchorChangedPaths(scope.ChangedForCheck(validate.CheckCodeAnchors), scope.Renamed)
	return r.refresh(ctx, lease, scope.Changed, scope.Renamed, scope.Deleted, anchorChanged)
}

func (r ValidationProjectionPostApplyRefresher) refresh(
	ctx context.Context,
	lease *validate.IndexLockLease,
	changed []string,
	renamed []validate.PathRename,
	deleted []string,
	anchorChanged []string,
) (validate.PostApplyRefreshResult, error) {
	if err := r.NoteMetadata.Validate(); err != nil {
		return validate.PostApplyRefreshResult{}, fmt.Errorf("note metadata indexer: %w", err)
	}
	refreshNoteAnchors := len(anchorChanged) > 0 || len(deleted) > 0
	request := normalizeValidationProjectionRequest(ValidationProjectionRequest{
		VaultPath:          r.VaultPath,
		VaultDef:           r.VaultDef,
		NoteMetadata:       r.NoteMetadata,
		NoteReader:         r.NoteReader,
		Target:             ValidationProjectionLive,
		refreshNoteAnchors: refreshNoteAnchors,
	})
	if err := lease.RequireHeldForVault(request.VaultPath); err != nil {
		return validate.PostApplyRefreshResult{}, err
	}

	vaultPaths, err := paths.NewVaultPaths(request.VaultPath)
	if err != nil {
		return validate.PostApplyRefreshResult{}, fmt.Errorf("resolve post-apply vault paths: %w", err)
	}
	exactPaths, err := postApplyProjectionExactPaths(vaultPaths, changed, renamed, deleted)
	if err != nil {
		return validate.PostApplyRefreshResult{}, err
	}
	request.ExactPaths = &exactPaths
	request.noteAnchorChangedPaths, err = postApplyNotePaths(vaultPaths, anchorChanged)
	if err != nil {
		return validate.PostApplyRefreshResult{}, fmt.Errorf("normalize changed code-anchor paths: %w", err)
	}

	projection, err := refreshValidationProjectionWithHeldIndexLock(ctx, request)
	if err != nil {
		return validate.PostApplyRefreshResult{}, err
	}
	return newPostApplyProjectionResult(projection)
}

func postApplyAnchorChangedPaths(changed []string, renamed []validate.PathRename) []string {
	result := append([]string(nil), changed...)
	for _, rename := range renamed {
		result = append(result, rename.To)
	}
	return dedupeSortedStrings(result)
}

func postApplyProjectionExactPaths(
	vaultPaths paths.VaultPaths,
	changed []string,
	renamed []validate.PathRename,
	deleted []string,
) (ValidationProjectionPaths, error) {
	changed = append([]string(nil), changed...)
	deleted = append([]string(nil), deleted...)
	for _, rename := range renamed {
		changed = append(changed, rename.To)
		deleted = append(deleted, rename.From)
	}
	exactChanged, err := postApplyNotePaths(vaultPaths, changed)
	if err != nil {
		return ValidationProjectionPaths{}, fmt.Errorf("normalize changed post-apply paths: %w", err)
	}
	exactDeleted, err := postApplyNotePaths(vaultPaths, deleted)
	if err != nil {
		return ValidationProjectionPaths{}, fmt.Errorf("normalize deleted post-apply paths: %w", err)
	}
	return ValidationProjectionPaths{Changed: exactChanged, Deleted: exactDeleted}, nil
}

func postApplyNotePaths(vaultPaths paths.VaultPaths, values []string) ([]paths.NotePath, error) {
	raw := make([]paths.NotePath, 0, len(values))
	for _, value := range values {
		raw = append(raw, paths.NotePath(value))
	}
	normalized, err := normalizedNotePathStrings(vaultPaths, raw)
	if err != nil {
		return nil, err
	}
	return stringPathsToNotePaths(normalized), nil
}

// validationProjectionSourcePaths separates exact affected-path evidence from
// source intake. A present path that no longer belongs to notes must retire its
// old projection, while still participating in ontology dependency refresh.
func validationProjectionSourcePaths(request ValidationProjectionRequest, changed, deleted []string) ([]string, []string, []string, error) {
	anchorChanged := changed
	if request.noteAnchorChangedPaths != nil {
		anchorChanged = notePathsToStrings(request.noteAnchorChangedPaths)
	}
	if request.ExactPaths == nil {
		return changed, deleted, anchorChanged, nil
	}
	formats, err := request.NoteMetadata.FormatRuntime()
	if err != nil {
		return nil, nil, nil, err
	}
	selector, err := noteownership.CompileSelector(noteownership.SelectorInput{
		VaultDefinition: request.VaultDef, Registry: formats.Registry(),
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("compile exact note-source selection: %w", err)
	}
	selected := make([]string, 0, len(changed))
	retired := append([]string(nil), deleted...)
	anchorSources := make(map[string]struct{}, len(changed))
	var systemSelector *noteownership.Selector
	for _, path := range changed {
		pathSelector := selector
		if ignore.IsSystemContextPath(path) {
			if systemSelector == nil {
				// Match note discovery's system-context exemption without
				// relaxing hidden, infrastructure, or unified hard ignores.
				definition := request.VaultDef
				definition.Includes = append(append([]string(nil), definition.Includes...), ignore.SystemContextFilename, "**/"+ignore.SystemContextFilename)
				definition.Excludes = nil
				compiled, err := noteownership.CompileSelector(noteownership.SelectorInput{VaultDefinition: definition, Registry: formats.Registry()})
				if err != nil {
					return nil, nil, nil, fmt.Errorf("compile system-context source selection: %w", err)
				}
				systemSelector = &compiled
			}
			pathSelector = *systemSelector
		}
		selection, err := pathSelector.Select(paths.RelPath(path))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("select exact note source %s: %w", path, err)
		}
		if selection.Owner != notediscovery.Note {
			retired = append(retired, path)
			continue
		}
		selected = append(selected, path)
		// The anchor declaration adapter owns Markdown syntax only. Other
		// selected providers still publish their normal metadata and targets.
		if selection.Provider == noteownership.MarkdownFormatID {
			anchorSources[path] = struct{}{}
		}
	}
	exactAnchors := make([]string, 0, len(anchorChanged))
	for _, path := range anchorChanged {
		if _, selected := anchorSources[path]; selected {
			exactAnchors = append(exactAnchors, path)
		}
	}
	return selected, dedupeSortedStrings(retired), exactAnchors, nil
}

func newPostApplyProjectionResult(projection *ValidationProjectionResult) (validate.PostApplyRefreshResult, error) {
	if projection == nil {
		return validate.PostApplyRefreshResult{}, fmt.Errorf("validation projection result is absent")
	}
	if projection.Runtime == nil {
		return validate.PostApplyRefreshResult{}, errors.Join(
			fmt.Errorf("prepared validation runtime is absent"),
			projection.Close(),
		)
	}

	result := validate.NewPostApplyRefreshResult(projection.Runtime, projection)
	result.Domains = freshPostApplyProjectionDomains(projection.Freshness)
	result.Paths = postApplyProjectionPaths(projection.ChangedPaths, projection.DeletedPaths)
	result.Timings = postApplyProjectionTimings(projection.Timings)
	return result, nil
}

func freshPostApplyProjectionDomains(freshness map[ProjectionDomain]ProjectionFreshness) []string {
	domains := make([]string, 0, len(freshness))
	for domain, evidence := range freshness {
		if evidence.State == ProjectionFresh {
			domains = append(domains, string(domain))
		}
	}
	sort.Strings(domains)
	return domains
}

func postApplyProjectionPaths(changed, deleted []paths.NotePath) []string {
	values := make([]string, 0, len(changed)+len(deleted))
	for _, path := range changed {
		values = append(values, path.String())
	}
	for _, path := range deleted {
		values = append(values, path.String())
	}
	return dedupeSortedStrings(values)
}

func postApplyProjectionTimings(timings map[string]time.Duration) []validate.PostApplyTiming {
	phases := make([]string, 0, len(timings))
	for phase := range timings {
		phases = append(phases, phase)
	}
	sort.Strings(phases)
	result := make([]validate.PostApplyTiming, 0, len(phases))
	for _, phase := range phases {
		result = append(result, validate.PostApplyTiming{
			Phase:      phase,
			DurationMs: timings[phase].Milliseconds(),
		})
	}
	return result
}

var _ validate.PostApplyRefresher = ValidationProjectionPostApplyRefresher{}
