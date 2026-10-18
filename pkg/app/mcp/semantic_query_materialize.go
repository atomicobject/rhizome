package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/answer"
	answeradapt "github.com/atomicobject/rhizome/pkg/app/answer/adapt"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/search"
	searchplanner "github.com/atomicobject/rhizome/pkg/search/planner"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"path/filepath"
	"strings"
)

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

func enrichSemanticLinkTargets(ctx context.Context, cfg Config, store noderead.ReadStore, matches []SemanticMatchPayload) {
	if len(matches) == 0 || strings.TrimSpace(cfg.VaultPath) == "" {
		return
	}
	schema, err := ontoschema.LoadSchema(cfg.VaultPath)
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
	scope := noderead.NewService(cfg.VaultDef, &obsidian.Note{}, store, schema).NewScope(noderead.ScopeOptions{})
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

func enrichSemanticGroupLinkTargets(ctx context.Context, cfg Config, store noderead.ReadStore, groups []semanticQueryGroup) {
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
