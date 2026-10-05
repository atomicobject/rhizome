package unifiedsearch

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/answer"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
)

type ApplicationOptions struct {
	Runtime      Options
	Profile      Profile
	VisibleLimit int
	// CandidateWindow widens the profile's fixed candidate window so a caller
	// may ask for more visible sources than the profile default; it never
	// narrows the window. Continuation identity includes the effective window.
	CandidateWindow  int
	BudgetChars      int
	MaxPerOwner      int
	Continuation     string
	VaultIdentity    string
	Diagnostics      bool
	NoDefaultTimeout bool
	// MaterializeDisplay retains renderer-only grouping and Intel metadata
	// before Run closes its stores. JSON and answer transports leave it off.
	MaterializeDisplay bool
	runRuntime         func(context.Context, Options) (Result, error)
}

// Execute is the transport-neutral application entry point. It fixes one
// candidate population, assesses sources, and applies presentation pagination
// after canonical ranking.
func Execute(ctx context.Context, opts ApplicationOptions) (ApplicationResult, error) {
	started := time.Now()
	if opts.Runtime.Filters.TestsOnly && opts.Runtime.Filters.ExcludeTests {
		return ApplicationResult{}, fmt.Errorf("tests-only and exclude-tests filters cannot be combined")
	}
	intent := search.Intent(strings.TrimSpace(opts.Runtime.IntentInput))
	queries := opts.Runtime.QueryInputs
	if len(queries) == 0 {
		queries = NormalizeQueryInputs(opts.Runtime.Query, opts.Runtime.Queries, opts.Runtime.IntentInput)
	}
	if intent == "" {
		intent = search.Intent(uniformQueryMode(queries))
	}
	intentSource := "explicit"
	var intentScore float64
	var routingWarnings []search.Warning
	if intent == "" && !everyQueryHasExplicitMode(queries) {
		// One default-intent heuristic for every caller; MCP and CLI must not
		// route the same text differently.
		decision := InferDefaultIntent(JoinQueryInputs(queries), MergeSeedTokens(opts.Runtime.Seeds, opts.Runtime.Files))
		intent = decision.Intent
		intentSource = decision.Source
		intentScore = decision.Score
		routingWarnings = decision.Warnings
	}
	if intent == "" {
		intent = search.IntentSearch
	}
	policy, err := resolveApplicationPolicy(opts, intent)
	if err != nil {
		return ApplicationResult{}, err
	}
	if opts.NoDefaultTimeout {
		policy.Deadline = 0
		policy.DeadlineMillis = 0
	}
	if _, ok := ctx.Deadline(); !ok && policy.Deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, policy.Deadline)
		defer cancel()
	}
	var timings *search.Timings
	if opts.Diagnostics {
		timings = &search.Timings{}
		ctx = search.WithTimings(ctx, timings)
	}
	queries = NormalizeExplicitQueryInputs(queries, string(intent))
	request := EffectiveRequest{Queries: queries, Intent: intent, IntentSource: intentSource, IntentScore: intentScore, Profile: policy.Profile, Seeds: MergeSeedTokens(opts.Runtime.Seeds, opts.Runtime.Files), Filters: opts.Runtime.Filters, Policy: policy}
	vaultIdentity := strings.TrimSpace(opts.VaultIdentity)
	if vaultIdentity == "" {
		vaultIdentity, err = filepath.Abs(opts.Runtime.VaultPath)
		if err != nil {
			return ApplicationResult{}, err
		}
	}
	identity, err := RequestIdentity(request, vaultIdentity)
	if err != nil {
		return ApplicationResult{}, err
	}
	var cursor *ContinuationCursor
	if strings.TrimSpace(opts.Continuation) != "" {
		decoded, decodeErr := DecodeContinuation(opts.Continuation)
		if decodeErr != nil {
			return ApplicationResult{}, decodeErr
		}
		if err := ValidateContinuationRequest(decoded, identity, policy.CandidateWindow); err != nil {
			return ApplicationResult{}, err
		}
		cursor = &decoded
	}
	var queryEmbeddingMemo *semantic.QueryEmbeddingMemo
	seededEmbeddings := []semantic.QueryEmbeddingRecord(nil)
	if cursor != nil {
		seededEmbeddings = cursor.QueryEmbeddings
	}
	ctx, queryEmbeddingMemo = semantic.WithQueryEmbeddingMemo(ctx, seededEmbeddings)
	runtimeOpts := opts.Runtime
	runtimeOpts.QueryInputs = queries
	runtimeOpts.IntentInput = string(intent)
	runtimeOpts.Limit = policy.CandidateWindow
	runtimeOpts.MaxPerOwner = policy.MaxPerOwner
	runtimeOpts.BudgetChars = policy.BudgetChars
	runtimeOpts.DeferPack = runtimeOpts.Pack
	runRuntime := opts.runRuntime
	if runRuntime == nil {
		runRuntime = Run
	}
	result, err := runRuntime(ctx, runtimeOpts)
	if err != nil {
		return ApplicationResult{}, err
	}
	if result.Cleanup != nil {
		defer result.Cleanup()
	}
	if result.Intent != "" {
		// Target repair may downgrade the requested intent; report what ran.
		intent = result.Intent
		request.Intent = intent
	}
	sources := searchapplication.AssessQuery(JoinQueryInputs(queries), intent, searchapplication.CanonicalizeSources(result.Results))
	target := TargetResolution{Status: result.TargetStatus, Confidence: result.TargetConfidence, Candidates: result.TargetCandidates, Selected: result.ResolvedTarget}
	if search.IsPrecisionIntent(intent) {
		target = searchapplication.ResolvePrecisionEvidenceTarget(intent, target, sources)
		sources = searchapplication.DemoteNonTargetDefinitions(intent, target, sources)
	} else {
		target = searchapplication.ResolveNavigationTarget(target, sources)
	}
	digest := OrderedSourceDigest(sources)
	offset := 0
	if cursor != nil {
		if result.IndexGeneration == "" {
			return ApplicationResult{}, fmt.Errorf("%w: committed index generation unavailable", ErrCursorStale)
		}
		if err := ValidateContinuation(*cursor, identity, result.IndexGeneration, digest, policy.CandidateWindow); err != nil {
			return ApplicationResult{}, err
		}
		offset = cursor.Offset
	}
	if offset > len(sources) {
		return ApplicationResult{}, fmt.Errorf("%w: offset exceeds ranked window", ErrCursorStale)
	}
	end := min(offset+policy.VisibleLimit, len(sources))
	page := append([]SourceAssessment(nil), sources[offset:end]...)
	var display []DisplayItem
	if opts.MaterializeDisplay {
		display = materializePageDisplay(ctx, page, result.Display, result.IntelStore)
	}
	packedText := result.PackedText
	if runtimeOpts.Pack && result.DeferredPacker != nil {
		pageResults := make([]search.RankedResult, 0, len(page))
		for _, source := range page {
			pageResults = append(pageResults, source.Result)
		}
		packed, packErr := search.PackRankedResults(ctx, result.DeferredPacker, result.PackSpec, pageResults)
		if packErr != nil {
			return ApplicationResult{}, packErr
		}
		packedText = packed.Text
	}
	var continuation string
	if end < len(sources) && result.IndexGeneration != "" {
		continuation, err = EncodeContinuation(ContinuationCursor{RequestIdentity: identity, IndexGeneration: result.IndexGeneration, WindowDigest: digest, CandidateWindow: policy.CandidateWindow, Offset: end, QueryEmbeddings: queryEmbeddingMemo.Snapshot()})
		if err != nil {
			return ApplicationResult{}, err
		}
	}
	availability := searchapplication.AvailabilityFromLanes(result.Lanes)
	answerPacket := BuildAssessedAnswerForFacets(intent, JoinQueryInputs(queries), queries, target, result.Warnings, availability, page)
	answerPacket = ApplyAnswerEvidenceLimits(intent, answerPacket, remainingExhaustiveProofs(intent, queries, sources[end:]))
	applicationResult := ApplicationResult{
		RequestIdentity: identity,
		Request:         request,
		Sources:         page,
		Display:         display,
		Target:          target,
		Answer:          answerPacket,
		Warnings:        append(append([]search.Warning(nil), routingWarnings...), result.Warnings...),
		Lanes:           result.Lanes,
		Availability:    availability,
		Counts:          CountSummary{RetrievedCandidates: len(result.Results), GroupedSources: len(sources), ReturnedSources: len(page), RemainingWindow: len(sources) - end},
		Continuation:    continuation,
		IndexGeneration: result.IndexGeneration,
		PackedText:      packedText,
		Duration:        time.Since(started),
	}
	if timings != nil {
		applicationResult.Timings = timings.Snapshot()
	}
	return applicationResult, nil
}

func materializePageDisplay(ctx context.Context, page []SourceAssessment, available []DisplayItem, intelStore *semdb.Store) []DisplayItem {
	byIdentity := make(map[string]DisplayItem, len(available))
	for _, item := range available {
		byIdentity[searchapplication.CanonicalSourceIdentity(item.Primary)] = item
	}
	out := make([]DisplayItem, 0, len(page))
	for _, source := range page {
		item := byIdentity[searchapplication.CanonicalSourceIdentity(source.Result)]
		item.Primary = source.Result
		if intelStore != nil && item.Primary.Type == "doc_section" {
			materializeDocSection(ctx, intelStore, &item.Primary)
		}
		if intelStore != nil && strings.TrimSpace(item.Primary.AnchorID) != "" {
			anchor, ok, err := intelStore.IntelAnchorByID(ctx, item.Primary.AnchorID)
			if err == nil && ok {
				item.Anchor = &anchor
				if strings.EqualFold(strings.TrimSpace(anchor.Kind), "module") {
					anchors, queryErr := intelStore.IntelAnchorsByPath(ctx, anchor.Path)
					if queryErr == nil {
						seen := map[string]struct{}{}
						for _, candidate := range anchors {
							symbol := strings.TrimSpace(candidate.Symbol)
							if symbol == "" || strings.EqualFold(strings.TrimSpace(candidate.Kind), "module") {
								continue
							}
							if _, exists := seen[symbol]; exists {
								continue
							}
							seen[symbol] = struct{}{}
							item.ModuleExports = append(item.ModuleExports, symbol)
						}
						sort.Strings(item.ModuleExports)
					}
				}
			}
		}
		out = append(out, item)
	}
	return out
}

func materializeDocSection(ctx context.Context, intelStore *semdb.Store, result *search.RankedResult) {
	sectionID := ""
	if result.Handle.Kind == knowledge.KindFile && len(result.Handle.Fragments) == 2 && result.Handle.Fragments[0] == "doc" {
		sectionID = result.Handle.Fragments[1]
	}
	if sectionID == "" || strings.TrimSpace(result.Path) == "" {
		return
	}
	sections, err := intelStore.IntelDocSectionsByPath(ctx, result.Path)
	if err != nil {
		return
	}
	for index, section := range sections {
		if section.SectionID != sectionID {
			continue
		}
		result.ChunkIndex = index
		if strings.TrimSpace(result.Heading) == "" {
			result.Heading = strings.TrimSpace(result.Title)
		}
		if strings.TrimSpace(result.Breadcrumb) == "" {
			result.Breadcrumb = result.Heading
		}
		return
	}
}

// ApplyAnswerEvidenceLimits applies page/window evidence discovered outside
// pure answer selection and always refreshes the displayed summary.
func ApplyAnswerEvidenceLimits(intent search.Intent, packet answer.Response, missingSignals []string) answer.Response {
	for _, missing := range missingSignals {
		packet.Coverage.Missing = appendUniqueText(packet.Coverage.Missing, missing)
		packet.Confidence.MissingSignals = appendUniqueText(packet.Confidence.MissingSignals, missing)
		if packet.Confidence.Level == "high" {
			packet.Confidence.Level = "medium"
		}
		packet.Confidence.Reason = "additional relationship results remain outside this page"
	}
	return answer.RefreshSummary(intent, packet)
}

func remainingExhaustiveProofs(intent search.Intent, queries []QueryInput, remaining []SourceAssessment) []string {
	var missing []string
	if relationship := exhaustiveRelationship(intent); relationship != "" {
		for _, source := range remaining {
			if source.Relevance == RelevanceStrong && source.Eligibility == EvidencePrimary && source.Relationship == relationship {
				missing = appendUniqueText(missing, "relationship:"+relationship+":more_results")
				break
			}
		}
	}
	for _, query := range queries {
		mode := search.Intent(strings.TrimSpace(query.Mode))
		if mode == "" {
			mode = intent
		}
		if exhaustiveRelationship(mode) == "" {
			continue
		}
		key := strings.TrimSpace(query.Text) + "\x00" + string(mode)
		for _, source := range remaining {
			if source.Relevance != RelevanceStrong {
				continue
			}
			for _, facet := range source.FacetSupport {
				facetKey := strings.TrimSpace(facet.Text) + "\x00" + string(facet.Mode)
				if facetKey == key && facet.Relevance == RelevanceStrong && facet.TargetEstablished && facet.Eligibility == EvidencePrimary && facet.Relationship == exhaustiveRelationship(mode) {
					missing = appendUniqueText(missing, "facet:"+strings.TrimSpace(query.Text)+":more_results")
					break
				}
			}
		}
	}
	return missing
}

func exhaustiveRelationship(intent search.Intent) string {
	switch intent {
	case search.IntentFindUsages:
		return "usage"
	case search.IntentCallers:
		return "caller"
	case search.IntentCallees:
		return "callee"
	case search.IntentTestsForCode:
		return "tests"
	case search.IntentImplementers:
		return "implementation"
	case search.IntentOverrides:
		return "override"
	case search.IntentImports:
		return "import"
	default:
		return ""
	}
}

func appendUniqueText(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func BuildAssessedAnswerForFacets(intent search.Intent, query string, queries []QueryInput, target TargetResolution, warnings []search.Warning, availability Availability, sources []SourceAssessment) answer.Response {
	facets := make([]searchapplication.RequiredFacet, 0, len(queries))
	for _, input := range queries {
		facets = append(facets, searchapplication.RequiredFacet{Text: input.Text, Mode: search.Intent(input.Mode)})
	}
	return searchapplication.BuildAssessedAnswerForFacets(intent, query, facets, target, warnings, availability, sources)
}

func everyQueryHasExplicitMode(queries []QueryInput) bool {
	if len(queries) == 0 {
		return false
	}
	for _, query := range queries {
		if strings.TrimSpace(query.Mode) == "" {
			return false
		}
	}
	return true
}

// uniformQueryMode returns the one mode shared by every query input, or "" when
// inputs disagree or carry no mode.
func uniformQueryMode(queries []QueryInput) string {
	mode := ""
	for _, query := range queries {
		current := strings.TrimSpace(query.Mode)
		if current == "" || (mode != "" && current != mode) {
			return ""
		}
		mode = current
	}
	return mode
}

func resolveApplicationPolicy(opts ApplicationOptions, intent search.Intent) (EffectivePolicy, error) {
	if opts.CandidateWindow <= 0 || opts.CandidateWindow <= opts.VisibleLimit && opts.VisibleLimit <= 0 {
		return ResolveEffectivePolicy(opts.Profile, intent, opts.VisibleLimit, opts.BudgetChars, opts.MaxPerOwner)
	}
	policy, err := ResolveEffectivePolicy(opts.Profile, intent, 0, opts.BudgetChars, opts.MaxPerOwner)
	if err != nil {
		return EffectivePolicy{}, err
	}
	policy.CandidateWindow = max(policy.CandidateWindow, opts.CandidateWindow)
	if opts.VisibleLimit > 0 {
		policy.VisibleLimit = opts.VisibleLimit
	}
	if policy.VisibleLimit > policy.CandidateWindow {
		return EffectivePolicy{}, errors.New("visible limit exceeds fixed candidate window")
	}
	return policy, nil
}
