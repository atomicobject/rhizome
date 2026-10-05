package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/answer"
	answeradapt "github.com/atomicobject/rhizome/pkg/app/answer/adapt"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	searchplanner "github.com/atomicobject/rhizome/pkg/search/planner"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// SemanticQueryOptions is the typed request shared by public adapters.
type SemanticQueryOptions struct {
	Profile            searchapplication.Profile
	Query              string               // single query text; ignored when Queries is set
	Queries            []SemanticQueryInput // optional multi-facet inputs with optional per-query Mode
	SeedTokens         []string
	Types              []string
	Mode               string
	Limit              int
	BudgetChars        int
	Explain            bool
	Timings            bool
	Scope              string
	PathPrefix         string
	NoteType           string
	ContinuationToken  string
	RequireExactSymbol bool
	IncludeTests       bool
	ExcludeTests       bool
	Compact            bool
}

// SemanticQueryUnifiedWithOptions is the single public entry point for the
// shared search path. Continuation requests must resend the same queries,
// seeds, mode, and controls; the application layer rejects mismatches.
func SemanticQueryUnifiedWithOptions(ctx context.Context, cfg Config, options SemanticQueryOptions) (SemanticQueryResponse, error) {
	inputs := options.Queries
	if len(inputs) == 0 {
		inputs = []SemanticQueryInput{{Text: options.Query}}
	}
	queries := make([]unifiedsearch.QueryInput, 0, len(inputs))
	for _, input := range inputs {
		queries = append(queries, unifiedsearch.QueryInput{Text: input.Text, Mode: input.Mode})
	}
	return runSemanticQuery(ctx, cfg, nil, "", semanticQueryRequest{
		profile:            options.Profile,
		queries:            queries,
		seeds:              append([]string(nil), options.SeedTokens...),
		types:              options.Types,
		mode:               options.Mode,
		limit:              options.Limit,
		budgetChars:        options.BudgetChars,
		explain:            options.Explain,
		timings:            options.Timings,
		scope:              options.Scope,
		pathPrefix:         options.PathPrefix,
		noteType:           options.NoteType,
		continuation:       options.ContinuationToken,
		requireExactSymbol: options.RequireExactSymbol,
		excludeTests:       options.ExcludeTests,
		compact:            options.Compact,
	})
}

// semanticQueryRequest is the normalized adapter request shared by the MCP tool
// handler and the typed public entry point.
type semanticQueryRequest struct {
	profile            searchapplication.Profile
	queries            []unifiedsearch.QueryInput
	seeds              []string
	types              []string
	mode               string
	limit              int
	budgetChars        int
	explain            bool
	timings            bool
	scope              string
	pathPrefix         string
	noteType           string
	continuation       string
	requireExactSymbol bool
	excludeTests       bool
	compact            bool
}

// normalizeSemanticQueryInputs trims inputs and splits blank-line separated
// text into independent facets while retaining each facet's mode.
func normalizeSemanticQueryInputs(inputs []unifiedsearch.QueryInput) []unifiedsearch.QueryInput {
	out := make([]unifiedsearch.QueryInput, 0, len(inputs))
	for _, input := range inputs {
		mode := strings.TrimSpace(input.Mode)
		for _, text := range normalizeQueryStrings([]string{input.Text}) {
			out = append(out, unifiedsearch.QueryInput{Text: text, Mode: mode})
		}
	}
	return out
}

func semanticQueryTexts(inputs []unifiedsearch.QueryInput) []string {
	out := make([]string, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, input.Text)
	}
	return out
}

func normalizeSemanticScope(scope string) string {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "all" {
		return ""
	}
	if scope == "notes" {
		return "docs"
	}
	return scope
}

func normalizeSemanticPathPrefix(prefix string) string {
	prefix = strings.TrimSpace(filepath.ToSlash(prefix))
	prefix = filepath.ToSlash(filepath.Clean(prefix))
	prefix = strings.TrimSuffix(prefix, "/")
	prefix = strings.TrimPrefix(prefix, "./")
	if prefix == "." || prefix == "/" {
		return ""
	}
	return prefix
}

func validateSemanticPathPrefix(prefix string) (string, error) {
	raw := strings.TrimSpace(filepath.ToSlash(prefix))
	if raw == "" {
		return "", nil
	}
	windowsVolume := len(raw) >= 2 && raw[1] == ':' && ((raw[0] >= 'a' && raw[0] <= 'z') || (raw[0] >= 'A' && raw[0] <= 'Z'))
	if strings.HasPrefix(raw, "/") || filepath.VolumeName(prefix) != "" || windowsVolume {
		return "", fmt.Errorf("pathPrefix must be vault-relative")
	}
	normalized := normalizeSemanticPathPrefix(raw)
	if normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", fmt.Errorf("pathPrefix must stay within the vault")
	}
	return normalized, nil
}

// runSemanticQuery is the one adapter core: it validates inputs, calls the
// shared application entry point once, and renders the returned page.
func runSemanticQuery(ctx context.Context, cfg Config, tracker *sessionTracker, sessionID string, req semanticQueryRequest) (semanticQueryResponse, error) {
	if _, ok := ctx.Deadline(); !ok {
		// MCP clients often omit deadlines. Put a server-side ceiling around the
		// whole unified-search path so a slow provider or SQLite read cannot pin a
		// tool call indefinitely.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, search.DefaultQueryTimeout)
		defer cancel()
	}
	if deadline, ok := ctx.Deadline(); ok {
		observeSemanticQueryDeadline(ctx, deadline)
	}
	ctx = withSemanticQueryReadCache(ctx)

	pathPrefix, err := validateSemanticPathPrefix(req.pathPrefix)
	if err != nil {
		return semanticQueryResponse{}, err
	}
	scope := normalizeSemanticScope(req.scope)
	noteType := strings.TrimSpace(req.noteType)
	types := normalizeSemanticTypes(req.types)
	switch scope {
	case "code", "tests":
		if noteType != "" {
			return semanticQueryResponse{}, fmt.Errorf("noteType cannot be combined with code scope")
		}
		types = []string{"code"}
	case "docs":
		types = []string{"note"}
	default:
		if noteType != "" {
			types = []string{"note"}
		}
	}

	queries := normalizeSemanticQueryInputs(req.queries)
	routeQuery := unifiedsearch.JoinQueryInputs(queries)
	seedTokens := normalizeSeedTokens(req.seeds)
	explicitMode := strings.TrimSpace(req.mode)
	perQueryModes := false
	for _, query := range queries {
		if query.Mode != "" {
			perQueryModes = true
			break
		}
	}
	if perQueryModes {
		if explicitMode != "" {
			return semanticQueryResponse{}, fmt.Errorf("use mode on queries or the mode argument, not both")
		}
		if err := requirePerQueryMode(queries); err != nil {
			return semanticQueryResponse{}, err
		}
	}
	if routeQuery == "" && len(seedTokens) == 0 {
		return semanticQueryResponse{}, fmt.Errorf("provide queries or seed paths")
	}
	if explicitMode != "" {
		if err := searchplanner.ValidateIntent(explicitMode); err != nil {
			return semanticQueryResponse{}, fmt.Errorf("unknown mode %q (allowed: %s)", explicitMode, strings.Join(semanticQueryModes(), ", "))
		}
	}

	budgetChars := req.budgetChars
	if budgetChars <= 0 {
		budgetChars = DefaultBudgetChars()
	}
	profile := req.profile
	if profile == "" {
		profile = searchapplication.ProfileAgent
	}

	// Prefer the long-lived, caller-managed indexed reader over opening a new
	// store. Session persistence is a separate capability.
	intelStore := cfg.GetIntelStore()
	if intelStore == nil {
		if cfg.IndexedReadOnlySemanticQuery {
			return unavailableIndexedSemanticQueryResponse(cfg, sessionID, routeQuery), nil
		}
		indexingperf.AddCount(ctx, indexingperf.SemanticQueryOpFallbackStoreOpens, 1)
		store, closeIntel, openErr := obsidian.OpenIntelStoreBestEffort(cfg.VaultPath, true)
		if openErr != nil {
			return semanticQueryResponse{}, openErr
		}
		intelStore = store
		defer closeIntel()
	}

	intent := search.Intent(explicitMode)
	modeDetected := ""
	modeScore := 0.0
	// Docs: [[Search - Seed expansion strategies#^search-seed-mcp-mode-contract]]
	// IMPORTANT: explicit mode wins. When callers omit it, unifiedsearch.Execute
	// applies the shared default-intent heuristic and reports what it chose.
	intentInput := ""
	if !perQueryModes && intent != "" {
		intentInput = string(intent)
	}

	var exactSymbols []string
	if req.requireExactSymbol {
		exactSymbols = normalizeQueryStrings(strings.Fields(routeQuery))
	}

	_, _, noteProvider, _ := cfg.NoteEmbeddings()
	_, _, codeProvider := cfg.CodeEmbeddingsState()
	warnCodeVectorOff := codeProvider == nil && semanticTypesWantCode(types)

	finishSearch := indexingperf.StartSpan(ctx, indexingperf.SemanticQueryPhaseSearch)
	result, err := unifiedsearch.Execute(ctx, unifiedsearch.ApplicationOptions{
		Runtime: unifiedsearch.Options{
			QueryInputs: queries,
			Seeds:       seedTokens,
			IntentInput: intentInput,
			UseVector:   true,
			UseIntel:    true,
			UseGraph:    true,
			UseRefs:     true,
			Filters: search.Filters{
				Types:        types,
				PathPrefixes: nonEmptyStrings(pathPrefix),
				NoteTypes:    nonEmptyStrings(noteType),
				TestsOnly:    scope == "tests",
				ExcludeTests: req.excludeTests && scope != "tests",
				ExactSymbols: exactSymbols,
			},
			VaultPath:              cfg.VaultPath,
			VaultDef:               cfg.VaultDef,
			IntelStore:             intelStore,
			NoteProvider:           noteProvider,
			CodeProvider:           codeProvider,
			DirectorySeedExpansion: semanticQueryDirectorySeedExpansionPolicy(cfg),
			GraphSource:            semanticQueryGraphSourcePolicy(cfg),
		},
		Profile:            profile,
		VisibleLimit:       req.limit,
		CandidateWindow:    req.limit,
		BudgetChars:        budgetChars,
		Continuation:       req.continuation,
		VaultIdentity:      semanticQueryVaultIdentity(cfg.VaultPath),
		Diagnostics:        req.timings,
		MaterializeDisplay: true,
	})
	finishSearch(err)
	if err != nil {
		switch {
		case errors.Is(err, unifiedsearch.ErrCursorStale), errors.Is(err, unifiedsearch.ErrCursorRefreshRequired):
			return semanticQueryResponse{}, fmt.Errorf("continuation_stale: %w", err)
		case errors.Is(err, unifiedsearch.ErrCursorInvalid):
			return semanticQueryResponse{}, fmt.Errorf("invalid continuationToken: %w", err)
		}
		return semanticQueryResponse{}, err
	}

	if result.Request.IntentSource == "inferred" {
		modeDetected = string(result.Request.Intent)
		modeScore = result.Request.IntentScore
	}
	policy := result.Request.Policy
	modeApplied := ""
	if !perQueryModes {
		modeApplied = string(result.Request.Intent)
	}
	query := unifiedsearch.JoinQueryInputs(result.Request.Queries)
	dedupeHits := func() int {
		if tracker == nil {
			return 0
		}
		return tracker.DedupeHits()
	}

	// Warning order: routing suggestions first, then the adapter's
	// retrieval-configuration note, then runtime repair and control notes.
	warnings := make([]search.Warning, 0, len(result.Warnings)+1)
	var runtimeWarnings []search.Warning
	for _, warning := range result.Warnings {
		if warning.Kind == "tool_routing" {
			warnings = append(warnings, warning)
		} else {
			runtimeWarnings = append(runtimeWarnings, warning)
		}
	}
	if warnCodeVectorOff {
		warnings = append(warnings, search.Warning{
			Code:    "code_vector_off",
			Kind:    "retrieval_config",
			Source:  "semantic_query",
			Message: "Code embeddings are unavailable; code recall is limited to lexical, symbol, refs, and graph evidence. Run `rzm index` after configuring code embeddings if vector code recall is expected.",
		})
	}
	warnings = append(warnings, runtimeWarnings...)

	if search.IsPrecisionIntent(result.Request.Intent) && len(result.Sources) == 0 && semanticTargetUnestablished(result.Target.Status) {
		// Precision modes fail closed. Returning a broad semantic pack for an
		// unresolved target makes agents think they have definition/usages
		// evidence when they only have topical matches.
		return semanticQueryResponse{
			Profile:              policy.Profile,
			Policy:               &policy,
			SessionID:            sessionID,
			ModeApplied:          modeApplied,
			ModeDetected:         modeDetected,
			ModeScore:            modeScore,
			TargetStatus:         result.Target.Status,
			ResolutionConfidence: result.Target.Confidence,
			TargetCandidates:     result.Target.Candidates,
			Warnings:             dedupeSemanticWarnings(warnings),
			DedupeHits:           dedupeHits(),
			Query:                query,
			TypeCounts:           map[string]int{},
			Text:                 "Target resolution failed for this precision mode. Pass --path or choose one of the suggested candidates.",
		}, nil
	}

	finishShaping := indexingperf.StartSpan(ctx, indexingperf.SemanticQueryPhaseShaping)
	page := semanticGroupsFromPage(result, req.explain)
	if err := hydrateSemanticGroupNoteTypes(ctx, intelStore, page); err != nil {
		warnings = append(warnings, search.Warning{
			Code:    "note_type_metadata_unavailable",
			Kind:    "result_metadata",
			Source:  "semantic_query",
			Message: "Resolved note types could not be added to search results.",
		})
	}
	if req.requireExactSymbol && len(result.Sources) == 0 {
		warnings = append(warnings, search.Warning{
			Code:    "exact_symbol_not_found",
			Kind:    "semantic_query",
			Message: "requireExactSymbol removed broad semantic matches; use code_symbol for candidate disambiguation.",
		})
	}
	if scope != "" {
		warnings = append(warnings, search.Warning{
			Code:    "scope_filter_applied",
			Kind:    "semantic_query",
			Source:  scope,
			Message: "semantic_query scope filtered all returned evidence representations.",
		})
	}
	warnings = dedupeSemanticWarnings(warnings)

	total := result.Counts.GroupedSources
	remaining := result.Counts.RemainingWindow
	pageOffset := total - len(page) - remaining
	if pageOffset < 0 {
		pageOffset = 0
	}
	queryIntent := buildSemanticQueryIntent(result.Request.Intent)
	docMatcher := newDocMatcher(effectiveVaultRoot(cfg))

	typeCounts := make(map[string]int)
	for _, g := range page {
		typeCounts[g.match.Type]++
	}

	textBudget := budgetChars - 2400
	if textBudget < 1200 {
		textBudget = budgetChars
	}

	grouped := packGroups(page, queryIntent, docMatcher)
	assignSemanticRoles(page, grouped, queryIntent, docMatcher)

	qLabel := query
	if strings.TrimSpace(qLabel) == "" {
		qLabel = "(seed-only)"
	}
	header := contextpack.Piece{
		Key:      "header",
		Priority: 1000,
		Score:    1,
		Text:     fmt.Sprintf("Query: %s\nResults: %d\nOffset: %d", qLabel, total, pageOffset),
	}

	anchorMeta := semanticPageAnchors(ctx, intelStore, result.Display, page)
	attachSymbolHits(page, anchorMeta)
	attachModuleOutlines(ctx, intelStore, page, anchorMeta)

	optionsByIndex := make(map[int][]semanticContentOption, len(page))
	type groupRank struct {
		idx   int
		ratio float64
		score float64
		path  string
	}
	groupRanks := make([]groupRank, 0, len(page))
	for i, g := range page {
		opts := buildSemanticOptions(g, anchorMeta, cfg.VaultPath, pageOffset+i, total, textBudget, queryIntent, docMatcher)
		if len(opts) == 0 {
			opts = []semanticContentOption{{plan: planStub, cost: 80, utility: g.score * 0.2, ratio: g.score * 0.2 / 80}}
		}
		optionsByIndex[i] = opts
		groupRanks = append(groupRanks, groupRank{
			idx:   i,
			ratio: opts[0].ratio,
			score: g.score,
			path:  g.match.Path,
		})
	}
	sort.SliceStable(groupRanks, func(i, j int) bool {
		if groupRanks[i].ratio != groupRanks[j].ratio {
			return groupRanks[i].ratio > groupRanks[j].ratio
		}
		if groupRanks[i].score != groupRanks[j].score {
			return groupRanks[i].score > groupRanks[j].score
		}
		return groupRanks[i].path < groupRanks[j].path
	})

	enrichSemanticGroupLinkTargets(ctx, cfg, intelStore, page)

	remainingBudget := textBudget - len(header.Text)
	if remainingBudget < 0 {
		remainingBudget = 0
	}
	selected := make(map[int]semanticSelection, len(page))
	selectionContext := semanticSelectionContext{
		selectedGroups: map[string]int{},
	}
	for _, gr := range groupRanks {
		if _, ok := selected[gr.idx]; ok {
			continue
		}
		opts := optionsByIndex[gr.idx]
		group := page[gr.idx]
		groupKey, _ := groupKeyAndLabel(group.match)
		summaryKey := candidateSummaryKey(group, anchorMeta)
		bestIdx := -1
		bestRatio := 0.0
		for i, opt := range opts {
			if opt.cost > remainingBudget {
				continue
			}
			if queryIntent.Fetch && opt.plan == planFull {
				bestIdx = i
				break
			}
			ratio := adjustOptionRatio(opt, group, summaryKey, groupKey, selectionContext, queryIntent, docMatcher)
			if ratio > bestRatio {
				bestRatio = ratio
				bestIdx = i
			}
		}
		if bestIdx >= 0 {
			opt := opts[bestIdx]
			selected[gr.idx] = semanticSelection{plan: opt.plan, cost: opt.cost}
			remainingBudget -= opt.cost
			recordSemanticSelection(&selectionContext, group, groupKey, summaryKey, opt.plan, queryIntent, docMatcher)
		}
	}

	materialized := materializeSemanticBodies(ctx, cfg, intelStore, page, selected, optionsByIndex, anchorMeta, pageOffset)
	// Selected distinct bodies for one source share the final group decision.
	// Filtering individual members here would lose that represented identity.
	groupDedupe := make(map[string]bool)
	firstFingerprint := make(map[string]string)
	for idx, body := range materialized {
		if _, ok := selected[idx]; !ok || body.plan == planStub || body.fingerprint == "" {
			continue
		}
		key := semanticMatchKey(page[idx].match)
		if first, ok := firstFingerprint[key]; ok && first != body.fingerprint {
			groupDedupe[key] = true
		} else {
			firstFingerprint[key] = body.fingerprint
		}
	}

	var b strings.Builder
	b.WriteString(contextpack.TrimToBudget(header.Text, textBudget))
	used := b.Len()

	matches := make([]SemanticMatchPayload, len(page))
	var textBodies []semanticTextBody
	included := make(map[int]bool, len(page))
	displayRanks := make(map[int]int, len(page))
	displayRank := pageOffset
	groupHeaderWritten := map[string]bool{}
	notePreviewCache := map[string]string{}
	codePreviewCache := map[string]string{}
	codeAnchorCache := map[string][]codeanchor.IntelAnchor{}
	clusterSeen := map[string]map[string]string{}

	maybeWriteGroupHeader := func(group semanticGroup) {
		if group.label == "" || groupHeaderWritten[group.key] {
			return
		}
		headerText := renderGroupHeader(group.label)
		if headerText == "" {
			return
		}
		sep := "\n\n"
		addLen := len(sep) + len(headerText)
		if used+addLen <= textBudget {
			b.WriteString(sep)
			b.WriteString(headerText)
			used += addLen
			groupHeaderWritten[group.key] = true
		}
	}

	for _, group := range grouped {
		for _, idx := range group.indices {
			displayRanks[idx] = displayRank
			g := page[idx]
			match := g.match
			match.Included = false

			body := ""
			fingerprint := ""
			plan := planStub
			if _, ok := selected[idx]; ok {
				body = materialized[idx].body
				fingerprint = materialized[idx].fingerprint
				plan = materialized[idx].plan

				if key := clusterKeyFromBody(body, match); key != "" {
					if clusterSeen[group.key] == nil {
						clusterSeen[group.key] = map[string]string{}
					}
					if primary, ok := clusterSeen[group.key][key]; ok && primary != match.Path {
						body = fmt.Sprintf("variant of %s; read if needed", primary)
						fingerprint = ""
						plan = planStub
					} else {
						clusterSeen[group.key][key] = match.Path
					}
				}

				key := semanticMatchKey(match)
				dup := false
				if tracker != nil && fingerprint != "" && !(tracker.deferMarks && groupDedupe[key]) {
					if !tracker.Allow(key, fingerprint) {
						dup = true
					}
				}
				if dup {
					match.ContentKind = string(plan)
					match.ContentTruncated = materialized[idx].truncated
					match.ContentDeduped = true
				}

				if !dup {
					pieceText := renderSemanticPiece(displayRank, match, g.score, body)
					sep := "\n\n"
					addLen := len(sep) + len(pieceText)
					if used+addLen <= textBudget {
						maybeWriteGroupHeader(group)
						b.WriteString(sep)
						start := b.Len()
						b.WriteString(pieceText)
						if fingerprint != "" && plan != planStub {
							textBodies = append(textBodies, semanticTextBody{key: key, fingerprint: fingerprint, start: start, end: b.Len()})
						}
						used += addLen
						match.Included = true
						included[idx] = true
						assignContentField(&match, plan, body, materialized[idx].truncated)
					}
				}
			}

			match.Preview = previewFromBody(body, match, cfg, notePreviewCache, codePreviewCache, codeAnchorCache, intelStore, g, anchorMeta)
			matches[idx] = match
			displayRank++
		}
	}

	// Use any remaining budget for cheap stubs in rank order.
	if used+100 < textBudget {
		for _, group := range grouped {
			for _, idx := range group.indices {
				g := page[idx]
				if included[idx] {
					continue
				}
				match := matches[idx]
				if match.Included || (match.ContentDeduped && tracker != nil && tracker.deferMarks) {
					continue
				}
				stubBody := "matches; read if needed"
				pieceText := renderSemanticPiece(displayRanks[idx], match, g.score, stubBody)
				sep := "\n\n"
				addLen := len(sep) + len(pieceText)
				if used+addLen > textBudget {
					break
				}
				maybeWriteGroupHeader(group)
				b.WriteString(sep)
				b.WriteString(pieceText)
				used += addLen
				match.Included = true
				assignContentField(&match, planStub, stubBody, false)
				match.Preview = previewFromBody("", match, cfg, notePreviewCache, codePreviewCache, codeAnchorCache, intelStore, g, anchorMeta)
				matches[idx] = match
				included[idx] = true
			}
		}
	}

	packed := b.String()
	// Docs: [[search-quality-evaluation-corpus#^spec-0041-us2-ac2]] and
	// [[search-diagnostics-explain-architecture#^spec-0043-us4-ac1]].
	// semantic-query returns answer roles plus raw matches so quality checks can
	// separate retrieval/ranking failures from answer-packet shaping failures.
	packet := applySemanticAnswerPresentation(result.Answer, matches)
	packet.NextQueries = addExactCodeNextQuery(packet.NextQueries, routeQuery)
	if packetText := answer.RenderText(packet); strings.TrimSpace(packetText) != "" {
		if strings.TrimSpace(packed) != "" {
			packed = packetText + "\n\n" + packed
			for i := range textBodies {
				textBodies[i].start += len(packetText) + 2
				textBodies[i].end += len(packetText) + 2
			}
		} else {
			packed = packetText
		}
	}

	// Retrieve pack metadata from intel store if available.
	var pm *codeanchor.PackMetadata
	if store := cfg.GetIntelStore(); store != nil {
		if meta, err := store.GetPackMetadata(ctx); err == nil {
			pm = &meta
		}
	}

	finishShaping(nil)
	out := semanticQueryResponse{
		Profile:              policy.Profile,
		Policy:               &policy,
		SessionID:            sessionID,
		DedupeHits:           dedupeHits(),
		Query:                query,
		ModeApplied:          modeApplied,
		ModeDetected:         modeDetected,
		ModeScore:            modeScore,
		TargetStatus:         result.Target.Status,
		ResolutionConfidence: result.Target.Confidence,
		TargetCandidates:     result.Target.Candidates,
		Warnings:             warnings,
		Lanes:                result.Lanes,
		Returned:             result.Counts.ReturnedSources,
		Total:                total,
		Count:                len(matches),
		Remaining:            remaining,
		TypeCounts:           typeCounts,
		ContinuationToken:    result.Continuation,
		Summary:              packet.Summary,
		MustRead:             packet.MustRead,
		Supporting:           packet.Supporting,
		Coverage:             packet.Coverage,
		Confidence:           packet.Confidence,
		NextQueries:          packet.NextQueries,
		Text:                 packed,
		Matches:              matches,
		AssessedSources:      result.Sources,
		PackMeta:             pm,
		searchTimings:        result.Timings,
		textBodies:           textBodies,
	}
	if len(result.Request.Queries) > 1 {
		out.Queries = semanticQueryTexts(result.Request.Queries)
		out.QueryInputs = make([]SemanticQueryInput, 0, len(result.Request.Queries))
		for _, input := range result.Request.Queries {
			out.QueryInputs = append(out.QueryInputs, SemanticQueryInput{Text: input.Text, Mode: input.Mode})
		}
	}
	if req.compact {
		out = compactSemanticQueryResponse(out)
	}
	return out, nil
}

func nonEmptyStrings(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{value}
}

func semanticTargetUnestablished(status search.TargetStatus) bool {
	switch status {
	case search.TargetStatusAmbiguous, search.TargetStatusUnresolved, search.TargetStatusNone:
		return true
	default:
		return false
	}
}

// semanticGroupsFromPage renders the application page as presentation groups.
// Display carries renderer-only enrichment such as doc-section chunk indexes.
func semanticGroupsFromPage(result unifiedsearch.ApplicationResult, explain bool) []semanticQueryGroup {
	page := make([]semanticQueryGroup, 0, len(result.Sources))
	for i, source := range result.Sources {
		ranked := source.Result
		if i < len(result.Display) {
			ranked = result.Display[i].Primary
		}
		match := matchFromRanked(ranked)
		evidence, diversity := evidenceSummary(ranked.Evidence)
		if explain && len(evidence) > 0 {
			match.Evidence = evidence
		}
		page = append(page, semanticQueryGroup{
			key:       searchapplication.CanonicalSourceIdentity(ranked),
			match:     match,
			score:     ranked.FinalScore,
			hits:      []search.RankedResult{ranked},
			spread:    1,
			diversity: diversity,
		})
	}
	return page
}

// semanticPageAnchors reuses anchors already materialized for the page and
// fetches only what the display layer did not supply.
func semanticPageAnchors(ctx context.Context, store *semdb.Store, display []unifiedsearch.DisplayItem, page []semanticQueryGroup) map[string]codeanchor.IntelAnchor {
	anchors := make(map[string]codeanchor.IntelAnchor, len(display))
	for _, item := range display {
		if item.Anchor != nil && strings.TrimSpace(item.Anchor.AnchorID) != "" {
			anchors[item.Anchor.AnchorID] = *item.Anchor
		}
	}
	if store == nil {
		return anchors
	}
	missing := make([]string, 0)
	for _, id := range collectAnchorIDs(page) {
		if _, ok := anchors[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return anchors
	}
	fetched, err := store.IntelAnchorsByIDs(ctx, missing)
	if err != nil {
		return anchors
	}
	for id, anchor := range fetched {
		anchors[id] = anchor
	}
	return anchors
}

func requirePerQueryMode(inputs []unifiedsearch.QueryInput) error {
	for i, input := range inputs {
		if strings.TrimSpace(input.Mode) == "" {
			return fmt.Errorf("query %d is missing mode", i+1)
		}
	}
	return nil
}

func semanticQueryVaultIdentity(vaultPath string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(filepath.Clean(vaultPath))))
}

func semanticQueryDirectorySeedExpansionPolicy(cfg Config) searchplanner.DirectorySeedExpansionPolicy {
	if cfg.IndexedReadOnlySemanticQuery {
		return searchplanner.DirectorySeedExpansionIndexedOnly
	}
	return searchplanner.DirectorySeedExpansionAllowFilesystemFallback
}

func semanticQueryGraphSourcePolicy(cfg Config) retrieval.GraphSourcePolicy {
	if cfg.IndexedReadOnlySemanticQuery {
		return retrieval.GraphSourceIndexedOnly
	}
	return retrieval.GraphSourceAllowFilesystemFallback
}

func unavailableIndexedSemanticQueryResponse(cfg Config, sessionID, query string) semanticQueryResponse {
	state := cfg.IndexedContextUnavailable
	warning := search.Warning{
		Code:    "indexed-context-missing",
		Kind:    "retrieval_config",
		Source:  "semantic_query",
		Message: "The current indexed read model is unavailable. Run `rzm index` and retry.",
	}
	if state != nil {
		warning.Code = state.WarningCode
		warning.Message = fmt.Sprintf("The current indexed read model is %s. Run `%s` and retry.", state.State, state.Remediation)
	}
	return semanticQueryResponse{
		SessionID:  sessionID,
		Query:      query,
		Warnings:   []search.Warning{warning},
		TypeCounts: map[string]int{},
		Text:       warning.Message,
	}
}

func hydrateSemanticGroupNoteTypes(ctx context.Context, store *semdb.Store, groups []semanticQueryGroup) error {
	if store == nil || len(groups) == 0 {
		return nil
	}
	paths := make([]string, 0, len(groups))
	for _, group := range groups {
		if group.match.Type == "note" && strings.TrimSpace(group.match.Path) != "" {
			paths = append(paths, group.match.Path)
		}
	}
	rows, err := store.OntologyTypesByPaths(ctx, paths)
	if err != nil {
		return err
	}
	for i := range groups {
		if row, ok := rows[groups[i].match.Path]; ok {
			groups[i].match.NoteType = row.TypeName
		}
	}
	return nil
}

func applySemanticAnswerPresentation(packet answer.Response, matches []SemanticMatchPayload) answer.Response {
	byKey := make(map[string]SemanticMatchPayload, len(matches))
	for _, match := range matches {
		byKey[semanticAnswerMatchKey(match)] = match
	}
	apply := func(items []answer.Item) {
		for i := range items {
			match, ok := byKey[semanticAnswerItemKey(items[i])]
			if !ok {
				continue
			}
			items[i].Line = match.StartLine
			items[i].Preview = contextpack.TrimToBudget(match.Preview, 220)
			items[i].NodeRef = answeradapt.NodeRef(match.NodeRef)
			items[i].LinkTarget = answeradapt.LinkTarget(match.LinkTarget)
		}
	}
	apply(packet.MustRead)
	apply(packet.Supporting)
	return packet
}

func semanticAnswerMatchKey(match SemanticMatchPayload) string {
	return semanticMatchKey(match)
}

func semanticAnswerItemKey(item answer.Item) string {
	if item.NodeRef != nil && item.NodeRef.Kind != "" && item.NodeRef.Kind != "NOTE" {
		return strings.Join([]string{"node", item.NodeRef.NotePath, item.NodeRef.NodeID, item.NodeRef.Fragment, item.NodeRef.StructuralFingerprint}, "\x00")
	}
	return semanticMatchKey(SemanticMatchPayload{Type: item.Type, Path: item.Path})
}

func semanticTypesWantCode(types []string) bool {
	if len(types) == 0 {
		return true
	}
	for _, typ := range types {
		switch strings.ToLower(strings.TrimSpace(typ)) {
		case "code", "anchor":
			return true
		}
	}
	return false
}

func attachSymbolHits(page []semanticQueryGroup, anchors map[string]codeanchor.IntelAnchor) {
	if len(page) == 0 || len(anchors) == 0 {
		return
	}
	for i := range page {
		group := &page[i]
		if group.match.Type != "code" {
			continue
		}
		seen := map[string]struct{}{}
		for _, existing := range group.match.Symbols {
			key := firstNonEmpty(existing.FQN, existing.Symbol)
			if key != "" {
				seen[key] = struct{}{}
			}
		}
		for _, hit := range group.hits {
			anchorID := strings.TrimSpace(hit.AnchorID)
			if anchorID == "" {
				continue
			}
			anchor, ok := anchors[anchorID]
			if !ok {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(anchor.Kind), "module") {
				continue
			}
			keys := []string{anchor.AnchorID, anchor.FQN, anchor.Symbol}
			key := firstNonEmpty(keys...)
			if key == "" {
				continue
			}
			duplicate := false
			for _, candidateKey := range keys {
				candidateKey = strings.TrimSpace(candidateKey)
				if candidateKey == "" {
					continue
				}
				if _, ok := seen[candidateKey]; ok {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			for _, candidateKey := range keys {
				candidateKey = strings.TrimSpace(candidateKey)
				if candidateKey != "" {
					seen[candidateKey] = struct{}{}
				}
			}
			group.match.Symbols = append(group.match.Symbols, SymbolHit{
				Symbol:    anchor.Symbol,
				FQN:       anchor.FQN,
				Kind:      anchor.Kind,
				StartLine: int(anchor.StartLine),
				EndLine:   int(anchor.EndLine),
				Score:     hit.FinalScore,
				Signature: strings.TrimSpace(anchor.Signature),
			})
			if len(group.match.Symbols) >= 5 {
				break
			}
		}
		if len(group.match.Symbols) == 0 {
			continue
		}
		best := group.match.Symbols[0]
		group.match.StartLine = best.StartLine
		group.match.EndLine = best.EndLine
		if best.Symbol != "" && (strings.TrimSpace(group.match.Symbol) == "" || group.match.Symbol == filepath.Base(group.match.Path)) {
			group.match.Symbol = best.Symbol
		}
		if best.FQN != "" && strings.TrimSpace(group.match.FQN) == "" {
			group.match.FQN = best.FQN
		}
		if best.Symbol != "" && (strings.TrimSpace(group.match.Title) == "" || group.match.Title == filepath.Base(group.match.Path)) {
			group.match.Title = best.Symbol
		}
	}
}

func attachModuleOutlines(ctx context.Context, store *semdb.Store, page []semanticQueryGroup, anchors map[string]codeanchor.IntelAnchor) {
	if store == nil || len(page) == 0 || len(anchors) == 0 {
		return
	}
	for i := range page {
		group := &page[i]
		if group.match.Type != "code" || len(group.match.Symbols) > 0 {
			continue
		}
		modulePath := ""
		for _, hit := range group.hits {
			anchorID := strings.TrimSpace(hit.AnchorID)
			if anchorID == "" {
				continue
			}
			anchor, ok := anchors[anchorID]
			if !ok || !strings.EqualFold(strings.TrimSpace(anchor.Kind), "module") {
				continue
			}
			modulePath = strings.TrimSpace(anchor.Path)
			break
		}
		if modulePath == "" {
			continue
		}
		outline, err := store.IntelAnchorsByPath(ctx, modulePath)
		if err != nil {
			continue
		}
		seen := map[string]struct{}{}
		for _, anchor := range outline {
			if strings.EqualFold(strings.TrimSpace(anchor.Kind), "module") || strings.TrimSpace(anchor.Symbol) == "" {
				continue
			}
			key := firstNonEmpty(anchor.AnchorID, anchor.FQN, anchor.Symbol)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			group.match.Symbols = append(group.match.Symbols, SymbolHit{
				Symbol:    anchor.Symbol,
				FQN:       anchor.FQN,
				Kind:      anchor.Kind,
				StartLine: int(anchor.StartLine),
				EndLine:   int(anchor.EndLine),
				Signature: strings.TrimSpace(anchor.Signature),
			})
			if len(group.match.Symbols) >= 5 {
				break
			}
		}
		if len(group.match.Symbols) == 0 {
			continue
		}
		best := group.match.Symbols[0]
		group.match.StartLine = best.StartLine
		group.match.EndLine = best.EndLine
		if strings.TrimSpace(group.match.Title) == "" || group.match.Title == filepath.Base(group.match.Path) {
			group.match.Title = filepath.Base(group.match.Path) + " exports"
		}
	}
}

func enrichSemanticLinkTargets(ctx context.Context, cfg Config, store noderead.Store, matches []SemanticMatchPayload) {
	if len(matches) == 0 || strings.TrimSpace(cfg.VaultPath) == "" {
		return
	}
	schema, err := ontology.LoadSchema(cfg.VaultPath)
	if err != nil || schema == nil {
		return
	}
	refs := make([]ontology.NodeRef, 0)
	byKey := map[string][]int{}
	for i := range matches {
		ref := ontologyRef(matches[i].NodeRef)
		if ref.IsZero() {
			ref = ontologyRefFromJSON(matches[i].NodeRefJSON)
		}
		if ref.IsZero() {
			continue
		}
		key := ref.String()
		refs = append(refs, ref)
		byKey[key] = append(byKey[key], i)
	}
	if len(refs) == 0 {
		return
	}
	scope := noderead.NewService(cfg.VaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, noderead.ScopeOptions{})
	locators, err := scope.Locators(ctx, refs)
	if err != nil {
		return
	}
	for key, locator := range locators {
		if locator.LinkTarget == nil {
			continue
		}
		for _, idx := range byKey[key] {
			targetCopy := *locator.LinkTarget
			matches[idx].LinkTarget = &targetCopy
			matches[idx].ReferenceHint = referenceHintForLinkTarget(&targetCopy)
		}
	}
}

func enrichSemanticGroupLinkTargets(ctx context.Context, cfg Config, store noderead.Store, groups []semanticQueryGroup) {
	if len(groups) == 0 {
		return
	}
	matches := make([]SemanticMatchPayload, len(groups))
	for i := range groups {
		matches[i] = groups[i].match
	}
	enrichSemanticLinkTargets(ctx, cfg, store, matches)
	for i := range groups {
		groups[i].match = matches[i]
	}
}

func referenceHintForLinkTarget(target *ontology.NodeLinkTarget) string {
	if target == nil {
		return ""
	}
	if strings.TrimSpace(target.Wikilink) != "" {
		return strings.TrimSpace(target.Wikilink)
	}
	return strings.TrimSpace(target.Markdown)
}

func ontologyRef(ref *ontology.NodeRef) ontology.NodeRef {
	if ref == nil {
		return ontology.NodeRef{}
	}
	return *ref
}

func ontologyRefFromJSON(raw string) ontology.NodeRef {
	if strings.TrimSpace(raw) == "" {
		return ontology.NodeRef{}
	}
	var ref ontology.NodeRef
	if err := json.Unmarshal([]byte(raw), &ref); err != nil {
		return ontology.NodeRef{}
	}
	return ref
}

func dedupeSemanticWarnings(in []search.Warning) []search.Warning {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]search.Warning, 0, len(in))
	for _, warning := range in {
		key := strings.Join([]string{warning.Code, warning.Kind, warning.Source, warning.Message}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, warning)
	}
	return out
}

func semanticQueryModes() []string {
	return []string{
		string(search.IntentSearch),
		string(search.IntentRelatedToSeed),
		string(search.IntentDocsForCode),
		string(search.IntentOverview),
		string(search.IntentSubsystemOverview),
		string(search.IntentCodeForDocs),
		string(search.IntentConsolidation),
		string(search.IntentFindUsages),
		string(search.IntentGoToDef),
		string(search.IntentExplainSymbol),
		string(search.IntentTestsForCode),
		string(search.IntentRefactorImpact),
		string(search.IntentCallers),
		string(search.IntentCallees),
		string(search.IntentImplementers),
		string(search.IntentOverrides),
		string(search.IntentImports),
		string(search.IntentDataFlow),
		string(search.IntentSecurityAudit),
	}
}

// docMatcher helpers

type docMatcher struct {
	patterns []string
}

func newDocMatcher(vaultPath string) docMatcher {
	fileCfg, _ := obsidian.FileContextConfigForVault(vaultPath)
	patterns := fileCfg.DocPatterns
	return docMatcher{patterns: normalizeDocPatterns(patterns)}
}

func effectiveVaultRoot(cfg Config) string {
	if base := strings.TrimSpace(cfg.VaultDef.BasePath()); base != "" {
		return base
	}
	if root := strings.TrimSpace(cfg.VaultPath); root != "" {
		return root
	}
	return ""
}

func normalizeDocPatterns(patterns []string) []string {
	patterns = obsidian.NormalizeDocPatterns(patterns)
	seen := map[string]struct{}{}
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(filepath.ToSlash(trimmed))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func (m docMatcher) IsDoc(path, typ string) bool {
	if typ == "note" {
		return true
	}
	return obsidian.MatchDocPatternNormalized(path, m.patterns)
}

func (m docMatcher) IsPrimaryDoc(path string) bool {
	if len(m.patterns) == 0 {
		return false
	}
	primary := strings.TrimSpace(m.patterns[0])
	if primary == "" {
		return false
	}
	return obsidian.MatchDocPatternNormalized(path, []string{primary})
}

func isFetchIntent(intent search.Intent) bool {
	switch intent {
	case search.IntentGoToDef,
		search.IntentFindUsages,
		search.IntentCallers,
		search.IntentCallees,
		search.IntentTestsForCode,
		search.IntentImplementers,
		search.IntentOverrides,
		search.IntentImports:
		return true
	default:
		return false
	}
}

func safeJoinVaultPath(vaultPath, rel string) (string, bool) {
	vaultPath = strings.TrimSpace(vaultPath)
	if vaultPath == "" {
		return "", false
	}
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", false
	}
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return "", false
	}
	relPath, err := vaultPaths.RelStrict(rel)
	if err != nil || relPath == "" {
		return "", false
	}
	abs, err := vaultPaths.Abs(relPath)
	if err != nil || abs == "" {
		return "", false
	}
	return abs.String(), true
}
