package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

// FrozenScopeDriftData is the payload attached to a frozen_scope_drift issue.
// Surfaces enough context for an agent to either land a Deviation entry on the
// effort, refreeze the scope, or revert the spec edit.
type FrozenScopeDriftData struct {
	EffortPath      string `json:"effortPath"`
	EffortID        string `json:"effortId,omitempty"`
	EffortStatus    string `json:"effortStatus,omitempty"`
	EffortCreatedAt string `json:"effortCreatedAt"`
	SpecPath        string `json:"specPath"`
	SpecID          string `json:"specId,omitempty"`
	SpecLastUpdated string `json:"specLastUpdated"`
}

// frozen-scope-drift only flags efforts that are still in flight: `planned`
// and `active`. Closed or otherwise non-live statuses pass through; the spec
// edit is no longer drifting under them.
var frozenScopeDriftLiveEffortStatuses = map[string]struct{}{
	"planned": {},
	"active":  {},
}

// frozenScopeDriftEffortForm names one typed effort family the check reads,
// the schema relation that holds its frozen specs, and where its
// `## Deviations` section lives.
type frozenScopeDriftEffortForm struct {
	typeName string
	relation string
	// deviationsInWorkLog reads `## Deviations` from the Markdown note named by
	// the `work-log` metadata property instead of the effort body.
	deviationsInWorkLog bool
}

// Markdown efforts freeze specs in their `Spec Set (Frozen)` section
// (`FrozenSpecSetSection.frozenSpecs`). HTML workspaces freeze them in
// `governing-specs` metadata and keep Deviations in their linked work log.
var frozenScopeDriftEffortForms = []frozenScopeDriftEffortForm{
	{typeName: "EffortNote", relation: "frozenSpecs"},
	{typeName: "EffortWorkspace", relation: "governingSpecs", deviationsInWorkLog: true},
}

// frozenScopeDriftAckPattern is the marker substring an effort's `Deviations`
// section uses to acknowledge a drift pair. The check treats a (effort, spec)
// pair as resolved when the effort's Deviations text contains BOTH this marker
// AND the spec's identifier (e.g. `SPEC-0022`). The marker form mirrors what
// agents write today: `(frozen-scope-drift acknowledged via EFF-XXXX)` and
// variants. Matched case-insensitively.
const frozenScopeDriftAckMarker = "frozen-scope-drift acknowledged"

// RunFrozenScopeDrift implements the public entrypoint. Defers to the runtime-
// aware variant when a runtime is available. RunSuiteOnce wires the runtime
// reuse, so this path is only hit from RunCheck or external callers.
func RunFrozenScopeDrift(ctx context.Context, runCtx RunContext) CheckResult {
	if err := runCtx.NoteMetadata.Validate(); err != nil {
		return CheckResult{Name: CheckFrozenScopeDrift, Error: fmt.Sprintf("note metadata indexer: %v", err)}
	}
	rt, cleanup, err := ontology.EnsureFreshRuntime(ctx, runCtx.NoteMetadata, runCtx.VaultDef, runCtx.NoteReader)
	if err != nil {
		return CheckResult{Name: CheckFrozenScopeDrift, Skipped: true, Summary: "ontology runtime unavailable: " + err.Error()}
	}
	if cleanup != nil {
		defer cleanup()
	}
	return RunFrozenScopeDriftWithRuntime(ctx, runCtx, rt)
}

// RunFrozenScopeDriftWithRuntime emits issues for every (effort, spec) pair
// where the spec's `last-updated` is after the effort's `created-at` AND the
// effort is still live. Closed efforts are skipped. Off-pattern timestamps
// land in result.Notes rather than as issues.
func RunFrozenScopeDriftWithRuntime(ctx context.Context, runCtx RunContext, runtime *ontology.Runtime) CheckResult {
	result := CheckResult{Name: CheckFrozenScopeDrift, OK: true}
	if runtime == nil || runtime.Schema == nil {
		result.Skipped = true
		result.Summary = "no ontology schema"
		return result
	}
	if runtime.Store == nil {
		result.Skipped = true
		result.Summary = "no intel store"
		return result
	}
	formByEffort := make(map[string]frozenScopeDriftEffortForm)
	var knownTypes []string
	for _, form := range frozenScopeDriftEffortForms {
		if _, ok := runtime.Schema.Types[form.typeName]; !ok {
			continue
		}
		knownTypes = append(knownTypes, form.typeName)
		paths, err := runtime.Store.OntologyPathsByType(ctx, form.typeName, 0)
		if err != nil {
			result.OK = false
			result.Error = err.Error()
			return result
		}
		for _, path := range paths {
			formByEffort[path] = form
		}
	}
	if len(knownTypes) == 0 {
		result.Skipped = true
		result.Summary = "no EffortNote or EffortWorkspace type in ontology"
		return result
	}
	effortPaths := make([]string, 0, len(formByEffort))
	for path := range formByEffort {
		effortPaths = append(effortPaths, path)
	}
	if len(effortPaths) == 0 {
		result.Summary = "no efforts to check"
		return result
	}
	sort.Strings(effortPaths)

	// Read effort metadata for status, created-at, and the Deviations source in
	// one round trip. HTML workspace metadata is indexed as frontmatter.
	effortRows, err := runtime.Store.CurrentNotePropertyValues(ctx, effortPaths, []string{"status", "created-at", "work-log"}, semdb.NotePropertySourceFrontmatter)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	statusByEffort := make(map[string]string, len(effortPaths))
	createdAtByEffort := make(map[string]time.Time, len(effortPaths))
	createdAtRawByEffort := make(map[string]string, len(effortPaths))
	workLogByEffort := make(map[string]string, len(effortPaths))
	for _, row := range effortRows {
		switch row.PropertyName {
		case "work-log":
			workLogByEffort[row.NotePath] = strings.TrimSpace(row.ValueText)
		case "status":
			statusByEffort[row.NotePath] = strings.TrimSpace(row.ValueText)
		case "created-at":
			raw := strings.TrimSpace(row.ValueText)
			createdAtRawByEffort[row.NotePath] = raw
			if t, ok := parseLooseTime(raw); ok {
				createdAtByEffort[row.NotePath] = t
			}
		}
	}

	// Narrow to live efforts. Skip closed ones (complete/archived) per SPEC-0051.
	liveEfforts := make([]string, 0, len(effortPaths))
	for _, path := range effortPaths {
		status := strings.ToLower(statusByEffort[path])
		if _, ok := frozenScopeDriftLiveEffortStatuses[status]; !ok {
			continue
		}
		if _, hasCreatedAt := createdAtByEffort[path]; !hasCreatedAt {
			result.Notes = append(result.Notes, fmt.Sprintf("%s: cannot parse created-at %q; skipping", path, createdAtRawByEffort[path]))
			continue
		}
		liveEfforts = append(liveEfforts, path)
	}

	if len(liveEfforts) == 0 {
		result.Summary = "no live efforts to check"
		return result
	}

	// Pull frozen-spec edges for the live efforts of each form, one query per form.
	liveByRelation := make(map[string][]string)
	for _, path := range liveEfforts {
		relation := formByEffort[path].relation
		liveByRelation[relation] = append(liveByRelation[relation], path)
	}
	var edges []semdb.OntologyEdgeRow
	for _, form := range frozenScopeDriftEffortForms {
		paths := liveByRelation[form.relation]
		if len(paths) == 0 {
			continue
		}
		formEdges, err := runtime.Store.OntologyEdgesForPaths(ctx, paths, false, form.relation, 0)
		if err != nil {
			result.OK = false
			result.Error = err.Error()
			return result
		}
		edges = append(edges, formEdges...)
	}
	if len(edges) == 0 {
		result.Summary = "no frozen spec links found"
		return result
	}

	// Read each linked spec's last-updated in one query.
	specPathSet := make(map[string]struct{})
	for _, edge := range edges {
		if strings.TrimSpace(edge.DstPath) == "" {
			continue
		}
		specPathSet[edge.DstPath] = struct{}{}
	}
	specPaths := make([]string, 0, len(specPathSet))
	for p := range specPathSet {
		specPaths = append(specPaths, p)
	}
	sort.Strings(specPaths)

	specRows, err := runtime.Store.CurrentNotePropertyValues(ctx, specPaths, []string{"last-updated", "id"}, semdb.NotePropertySourceFrontmatter)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	lastUpdatedBySpec := make(map[string]time.Time, len(specPaths))
	lastUpdatedRawBySpec := make(map[string]string, len(specPaths))
	idBySpec := make(map[string]string, len(specPaths))
	for _, row := range specRows {
		switch row.PropertyName {
		case "last-updated":
			raw := strings.TrimSpace(row.ValueText)
			lastUpdatedRawBySpec[row.NotePath] = raw
			if t, ok := parseLooseTime(raw); ok {
				lastUpdatedBySpec[row.NotePath] = t
			}
		case "id":
			idBySpec[row.NotePath] = strings.TrimSpace(row.ValueText)
		}
	}

	idByEffort := make(map[string]string, len(liveEfforts))
	for _, row := range effortRows {
		// Re-scan for `id` separately so we don't dirty the status/created-at loop above.
		if row.PropertyName == "id" {
			idByEffort[row.NotePath] = strings.TrimSpace(row.ValueText)
		}
	}
	if len(idByEffort) == 0 {
		idRows, err := runtime.Store.CurrentNotePropertyValues(ctx, liveEfforts, []string{"id"}, semdb.NotePropertySourceFrontmatter)
		if err == nil {
			for _, row := range idRows {
				if row.PropertyName == "id" {
					idByEffort[row.NotePath] = strings.TrimSpace(row.ValueText)
				}
			}
		}
	}

	// Stable iteration: edges keyed by (effort, spec) to dedupe; sort by effort
	// then spec for deterministic reporting.
	type pair struct{ effort, spec string }
	seen := make(map[pair]struct{}, len(edges))
	pairs := make([]pair, 0, len(edges))
	for _, edge := range edges {
		p := pair{effort: edge.SrcPath, spec: edge.DstPath}
		if p.effort == "" || p.spec == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].effort != pairs[j].effort {
			return pairs[i].effort < pairs[j].effort
		}
		return pairs[i].spec < pairs[j].spec
	})

	// Cache effort body Deviations sections so we read each note at most once
	// even when an effort has multiple drift pairs. Lazy-fill to avoid IO when
	// the check has nothing to skip.
	deviationsByEffort := make(map[string]string, len(liveEfforts))

	issues := make([]Issue, 0)
	for _, p := range pairs {
		createdAt, ok := createdAtByEffort[p.effort]
		if !ok {
			continue
		}
		lastUpdated, ok := lastUpdatedBySpec[p.spec]
		if !ok {
			if raw := strings.TrimSpace(lastUpdatedRawBySpec[p.spec]); raw != "" {
				result.Notes = append(result.Notes, fmt.Sprintf("%s: cannot parse last-updated %q on %s; skipping", p.effort, raw, p.spec))
			}
			continue
		}
		if sameDayDateOnly(lastUpdated, lastUpdatedRawBySpec[p.spec], createdAt) {
			// A spec and its effort are often written the same day, so a
			// date-only last-updated on the creation day is usually not drift.
			// Report it without failing so a same-day edit stays visible.
			result.Notes = append(result.Notes, fmt.Sprintf("%s: %s last-updated %s is the effort's creation day; the date cannot show whether the spec changed after the freeze", p.effort, specLabel(idBySpec[p.spec], p.spec), lastUpdatedRawBySpec[p.spec]))
			continue
		}
		if !lastUpdated.After(createdAt) {
			continue
		}
		// Acknowledgement gate: the agent has explicitly marked this drift
		// pair as resolved in the effort's Deviations section. Skip silently.
		if specID := idBySpec[p.spec]; specID != "" {
			body, ok := deviationsByEffort[p.effort]
			if !ok && runCtx.NoteReader != nil {
				source := p.effort
				if formByEffort[p.effort].deviationsInWorkLog {
					source = workLogByEffort[p.effort]
				}
				if source != "" {
					if raw, err := runCtx.NoteReader.GetContents(runCtx.VaultDef, source); err == nil {
						body = extractDeviationsSection(raw)
					}
				}
				deviationsByEffort[p.effort] = body
			}
			if body != "" && deviationsAcknowledgeDrift(body, specID) {
				continue
			}
		}
		data := FrozenScopeDriftData{
			EffortPath:      p.effort,
			EffortID:        idByEffort[p.effort],
			EffortStatus:    statusByEffort[p.effort],
			EffortCreatedAt: createdAtRawByEffort[p.effort],
			SpecPath:        p.spec,
			SpecID:          idBySpec[p.spec],
			SpecLastUpdated: lastUpdatedRawBySpec[p.spec],
		}
		payload, _ := json.Marshal(data)
		issues = append(issues, Issue{
			Code:    "frozen_scope_drift",
			Path:    p.effort,
			Target:  p.spec,
			Source:  data.SpecID,
			Message: fmt.Sprintf("effort %q (%s, status=%s, created %s) has frozen %s last-updated %s; either land a Deviation entry on the effort or refreeze the scope", p.effort, idByEffort[p.effort], statusByEffort[p.effort], createdAtRawByEffort[p.effort], specLabel(idBySpec[p.spec], p.spec), lastUpdatedRawBySpec[p.spec]),
			Data:    payload,
		})
	}

	totalIssues := len(issues)
	result.Issues = issues
	result.IssueCount = totalIssues
	if totalIssues == 0 {
		result.Summary = "no frozen scope drift detected"
	} else {
		result.Summary = fmt.Sprintf("%d frozen scope drift issue(s) across %d live effort(s)", totalIssues, len(liveEfforts))
	}
	return result
}

// sameDayDateOnly reports whether a date-only last-updated falls on the
// effort's UTC creation day, where the date cannot be ordered against the
// creation time.
func sameDayDateOnly(lastUpdated time.Time, lastUpdatedRaw string, createdAt time.Time) bool {
	if _, err := time.Parse("2006-01-02", strings.TrimSpace(lastUpdatedRaw)); err != nil {
		return false
	}
	return lastUpdated.Equal(createdAt.UTC().Truncate(24 * time.Hour))
}

// parseLooseTime accepts both `Date` (`2026-04-30`) and `DateTime`
// (`2026-04-30T14:32:00Z` and offset variants) frontmatter values, normalizing
// `Date` to the start of the day in UTC. Anything else returns ok=false.
func parseLooseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func specLabel(id, path string) string {
	if id != "" {
		return fmt.Sprintf("%s (%s)", id, path)
	}
	return path
}

// extractDeviationsSection returns the body text under the `## Deviations` H2
// heading in a markdown note, stopping at the next H2 (`## ...`). Returns an
// empty string when the section is absent or the input is not a string body.
// The lookup is intentionally heading-name-based rather than ontology-aware so
// the check works for partially-typed effort notes.
func extractDeviationsSection(body string) string {
	const heading = "## Deviations"
	idx := strings.Index(body, heading)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(heading):]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		rest = rest[:next]
	}
	return rest
}

// deviationsAcknowledgeDrift reports whether the given Deviations section
// text contains both the acknowledgement marker and the spec's identifier.
// Matching is case-insensitive on the marker (the spec id stays uppercase).
func deviationsAcknowledgeDrift(deviationsBody, specID string) bool {
	if specID == "" {
		return false
	}
	if !strings.Contains(deviationsBody, specID) {
		return false
	}
	return strings.Contains(strings.ToLower(deviationsBody), frozenScopeDriftAckMarker)
}
