package mcp

import (
	"context"

	"errors"
	"fmt"

	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/answer"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/notemeta"

	"github.com/atomicobject/rhizome/pkg/search"
	searchplanner "github.com/atomicobject/rhizome/pkg/search/planner"
	// SemanticQueryOptions is the typed request shared by public adapters.
)

// runResolvedSemanticQuery validates inputs against one captured config, calls the
// shared application entry point once, and renders the returned page.
func runResolvedSemanticQuery(ctx context.Context, cfg Config, tracker *sessionTracker, sessionID string, req semanticQueryRequest) (semanticQueryResponse, error) {
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
		budgetChars = cfg.BudgetChars()
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
		store, closeIntel, openErr := indexstore.Open(cfg.VaultPath, indexstore.Options{Access: indexstore.ReadOnly, Config: &cfg.RepositoryConfig.Code, RequireEnabled: true})
		if openErr != nil {
			return semanticQueryResponse{}, openErr
		}
		if store == nil {
			return semanticQueryResponse{}, fmt.Errorf("search index unavailable; run `rzm index`")
		}
		intelStore = store
		if closeIntel != nil {
			defer closeIntel()
		}
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
	if owner, ok := cfg.Runtime.(interface{ NoteMetadataIndexer() notemeta.Indexer }); ok {
		cfg.NoteMetadata = owner.NoteMetadataIndexer()
	}
	noteFormats, err := cfg.NoteMetadata.FormatRuntime()
	if err != nil {
		return semanticQueryResponse{}, fmt.Errorf("semantic query requires note metadata runtime: %w", err)
	}

	finishSearch := indexingperf.StartSpan(ctx, indexingperf.SemanticQueryPhaseSearch)
	result, err := unifiedsearch.Execute(ctx, unifiedsearch.ApplicationOptions{
		Runtime: unifiedsearch.Options{
			Config:      cfg.RepositoryConfig,
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
			NoteFormats:            noteFormats,
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
	docMatcher := newDocMatcher(cfg.RepositoryConfig)

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
