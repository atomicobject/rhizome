package validate

import ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

// Opt-in orphan-block-id cleanup check.
//
// WHY: SPEC-0023's usage-driven block-id lifecycle says block IDs earn their
// keep through external citation. This check finds anchors with no external
// inbound reference and offers reviewable removal. Broken external references
// with a fuzzy near-match to a candidate orphan are treated as soft holds and
// excluded from the auto-applied removal batch.
//
// Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us4]]

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

// fuzzyNearMatchDistance is the Levenshtein threshold used by the orphan check
// to classify a broken inbound reference as a near-match for a candidate
// orphan. A small constant keeps false positives low; the threshold is left
// configurable as a SPEC-0023 open question.
const fuzzyNearMatchDistance = 2

// OrphanBlockIDClass discriminates how an orphan candidate should be treated.
type OrphanBlockIDClass string

const (
	OrphanClassRemovable      OrphanBlockIDClass = "removable"
	OrphanClassFuzzyNearMatch OrphanBlockIDClass = "fuzzy_near_match"
)

// OrphanBlockIDData is the structured payload for orphan_block_id issues.
type OrphanBlockIDData struct {
	BlockID             string             `json:"blockId"`
	Class               OrphanBlockIDClass `json:"class"`
	NearMatchFrom       string             `json:"nearMatchFrom,omitempty"`
	NearMatchCandidates []string           `json:"nearMatchCandidates,omitempty"`
}

// orphanAnchor records one block-id anchor in a note plus the identity needed
// by the remove-fix apply path.
type orphanAnchor struct {
	notePath   string
	blockID    string
	nodeID     string
	structural string
	// identifierBacked is true when the anchor's source representation is an
	// `id:: ^...` line on a derivable-identity field (SPEC-0023.US8) rather
	// than a standalone `^block-id` line. The remove-apply path inspects this
	// to decide whether to strip the whole `id::` line or just the anchor.
	identifierBacked bool
}

// RunOrphanBlockIDs implements the opt-in orphan-block-id check.
func RunOrphanBlockIDs(ctx context.Context, runCtx RunContext) CheckResult {
	result := CheckResult{Name: CheckOrphanBlockIDs, OK: true}
	inventory, err := validationLinksFor(ctx, runCtx)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	// Phase 1: per-note anchor inventory.
	anchorsByNote := map[string]map[string]orphanAnchor{}
	schema, schemaErr := ontoschema.LoadSchema(runCtx.VaultPath)
	for _, notePath := range inventory.paths {
		source := inventory.sources[notePath]
		if source.Format != noteformat.FormatID("markdown") {
			continue
		}
		body := source.Content
		if body == "" {
			continue
		}
		blocks := map[string]orphanAnchor{}
		if schemaErr == nil {
			if snapshot, err := ontology.BuildDocumentSnapshot(notePath, body, time.Time{}); err == nil && snapshot != nil {
				collectAnchorsFromSnapshot(snapshot.Sections, schema, snapshot, notePath, blocks)
			}
		}
		for _, target := range source.FragmentTargets {
			if target.Kind != noteformat.FragmentTargetBlock || !standaloneBlockIDFact(body, target) {
				continue
			}
			id := strings.TrimPrefix(target.Text, "^")
			if _, present := blocks[id]; !present {
				blocks[id] = orphanAnchor{notePath: notePath, blockID: id}
			}
		}
		if len(blocks) > 0 {
			anchorsByNote[notePath] = blocks
		}
	}

	if len(anchorsByNote) == 0 {
		result.Summary = "no block IDs to evaluate"
		return result
	}

	// Phase 2: collect inbound block-id references from every other note.
	resolvedRefs := map[string]map[string]struct{}{}
	brokenRefs := map[string]map[string]struct{}{}
	for _, link := range inventory.links {
		if !link.Exists || link.Source == link.Target || !strings.HasPrefix(link.Fact.Fragment, "^") {
			continue
		}
		source := inventory.sources[link.Source]
		if source.Format == noteformat.FormatID("markdown") {
			if _, bodyStart, ok := markdownsyntax.Frontmatter(source.Content); ok && link.Fact.RawRange.StartByte < bodyStart {
				continue
			}
		}
		id := strings.TrimPrefix(link.Fact.Fragment, "^")
		if _, present := anchorsByNote[link.Target][id]; present {
			if resolvedRefs[link.Target] == nil {
				resolvedRefs[link.Target] = map[string]struct{}{}
			}
			resolvedRefs[link.Target][id] = struct{}{}
		} else {
			if brokenRefs[link.Target] == nil {
				brokenRefs[link.Target] = map[string]struct{}{}
			}
			brokenRefs[link.Target][id] = struct{}{}
		}
	}

	// Phase 3: classify each anchor.
	type orphan struct {
		anchor      orphanAnchor
		class       OrphanBlockIDClass
		nearMatches []string
	}
	var orphans []orphan
	for notePath, anchors := range anchorsByNote {
		resolved := resolvedRefs[notePath]
		broken := brokenRefs[notePath]
		for id, a := range anchors {
			if _, ok := resolved[id]; ok {
				continue
			}
			class := OrphanClassRemovable
			var nearMatches []string
			for brokenID := range broken {
				if brokenID == id {
					continue
				}
				if levenshteinAtMost(brokenID, id, fuzzyNearMatchDistance) {
					nearMatches = append(nearMatches, brokenID)
				}
			}
			sort.Strings(nearMatches)
			if len(nearMatches) > 0 {
				class = OrphanClassFuzzyNearMatch
			}
			orphans = append(orphans, orphan{anchor: a, class: class, nearMatches: nearMatches})
		}
	}

	sort.SliceStable(orphans, func(i, j int) bool {
		if orphans[i].anchor.notePath != orphans[j].anchor.notePath {
			return orphans[i].anchor.notePath < orphans[j].anchor.notePath
		}
		return orphans[i].anchor.blockID < orphans[j].anchor.blockID
	})

	result.IssueCount = len(orphans)
	result.Issues = make([]Issue, 0, len(orphans))
	result.Fixes = make([]FixAction, 0, len(orphans))
	for _, o := range orphans {
		nearMatch := ""
		if len(o.nearMatches) > 0 {
			nearMatch = o.nearMatches[0]
		}
		data := OrphanBlockIDData{
			BlockID:             o.anchor.blockID,
			Class:               o.class,
			NearMatchFrom:       nearMatch,
			NearMatchCandidates: append([]string(nil), o.nearMatches...),
		}
		message := fmt.Sprintf("block id ^%s has no resolved external references", o.anchor.blockID)
		if o.class == OrphanClassFuzzyNearMatch {
			message = fmt.Sprintf("block id ^%s has no resolved external references; broken inbound references ^%s are near-matches — verify before removing", o.anchor.blockID, strings.Join(o.nearMatches, ", ^"))
		}
		issue := Issue{
			Code:    "orphan_block_id",
			Path:    o.anchor.notePath,
			Source:  o.anchor.notePath,
			Message: message,
			Data:    mustMarshal(data),
		}
		issueKey, err := StableIssueKey(CheckOrphanBlockIDs, issue)
		if err != nil {
			result.OK = false
			result.IssueCount = 0
			result.Error = fmt.Sprintf("identify orphan-block-id issue: %v", err)
			return result
		}
		issue.Key = issueKey
		result.Issues = append(result.Issues, issue)
		safety := FixSafetySafe
		title := fmt.Sprintf("Remove unreferenced block ID ^%s in %s", o.anchor.blockID, o.anchor.notePath)
		summary := fmt.Sprintf("delete ^%s — no external reference targets it", o.anchor.blockID)
		if o.class == OrphanClassFuzzyNearMatch {
			safety = FixSafetyConfirm
			title = fmt.Sprintf("Remove block ID ^%s (verify near-match first)", o.anchor.blockID)
			summary = fmt.Sprintf("near-match warning: broken inbound references ^%s may have meant ^%s", strings.Join(o.nearMatches, ", ^"), o.anchor.blockID)
		}
		result.Fixes = append(result.Fixes, FixAction{
			ID:            fmt.Sprintf("remove-block-id:%s:%s", o.anchor.notePath, o.anchor.blockID),
			Check:         CheckOrphanBlockIDs,
			IssueCode:     "orphan_block_id",
			Kind:          FixKindRemoveBlockID,
			Safety:        safety,
			Title:         title,
			Summary:       summary,
			InstanceCount: 1,
			IssueKeys:     []string{issueKey},
			AffectedPaths: []string{o.anchor.notePath},
			Edits: []FixEdit{{
				Kind:       FixKindRemoveBlockID,
				NotePath:   o.anchor.notePath,
				NodeID:     o.anchor.nodeID,
				Structural: o.anchor.structural,
				BlockID:    o.anchor.blockID,
			}},
		})
	}
	switch {
	case len(orphans) == 0:
		result.Summary = "no orphan block IDs"
	default:
		result.Summary = fmt.Sprintf("%d orphan block IDs", len(orphans))
	}
	return result
}

func collectAnchorsFromSnapshot(nodes []*ontology.SectionNode, schema *ontoschema.Schema, snapshot *ontology.DocumentSnapshot, notePath string, out map[string]orphanAnchor) {
	for _, node := range nodes {
		if node == nil {
			continue
		}
		raw := strings.TrimSpace(node.BlockID)
		if raw != "" {
			id := strings.TrimPrefix(raw, "^")
			ref := ontology.NodeRef{
				NotePath: notePath,
				NodeID:   node.ID,
				Kind:     ontology.NodeKindEmbedded,
			}
			structural := ""
			identifierBacked := false
			if projection, err := ontology.ProjectBoundNodeFromSnapshot(snapshot, schema, ref); err == nil && projection != nil {
				field := preferredIdentifierFieldFor(projection)
				if sectionHasIdentifierBlockID(node, identifierFieldNameOf(field), id) {
					// WHY: per SPEC-0023.US8, identifier-backed lines on
					// derivable-identity fields whose populate policy is
					// ON_LINK are opt-in linkability rather
					// than durable identity — orphan-cleanup may strip them
					// when no inbound external references remain. Required-
					// identity identifier fields stay preserved as before.
					if field == nil || !field.IsDerivableIdentifier {
						collectAnchorsFromSnapshot(node.Children, schema, snapshot, notePath, out)
						continue
					}
					identifierBacked = true
				}
				structural = projection.Ref.Structural
			}
			if sectionHasInlinePropertyBlockID(node, id) && !identifierBacked {
				collectAnchorsFromSnapshot(node.Children, schema, snapshot, notePath, out)
				continue
			}
			out[id] = orphanAnchor{
				notePath:         notePath,
				blockID:          id,
				nodeID:           node.ID,
				structural:       structural,
				identifierBacked: identifierBacked,
			}
		}
		collectAnchorsFromSnapshot(node.Children, schema, snapshot, notePath, out)
	}
}

func sectionHasInlinePropertyBlockID(node *ontology.SectionNode, blockID string) bool {
	if node == nil {
		return false
	}
	target := "^" + strings.TrimSpace(strings.TrimPrefix(blockID, "^"))
	for _, line := range strings.Split(ontology.SectionOwnContent(node), "\n") {
		trimmed := strings.TrimSpace(line)
		idx := strings.Index(trimmed, "::")
		if idx <= 0 {
			continue
		}
		if strings.TrimSpace(trimmed[idx+2:]) == target {
			return true
		}
	}
	return false
}

func standaloneBlockIDFact(content string, target noteformat.FragmentTargetFact) bool {
	if !target.Range.Present {
		return false
	}
	start, end := target.Range.Range.StartByte, target.Range.Range.EndByte
	start = strings.LastIndexByte(content[:start], '\n') + 1
	if next := strings.IndexByte(content[end:], '\n'); next >= 0 {
		end += next
	} else {
		end = len(content)
	}
	return strings.TrimSpace(content[start:end]) == "^"+strings.TrimPrefix(target.Text, "^")
}

func splitFragment(target string) (string, string) {
	idx := strings.Index(target, "#")
	if idx < 0 {
		return target, ""
	}
	return target[:idx], target[idx+1:]
}

// levenshteinAtMost reports whether the edit distance between a and b is no
// more than the given limit.
func levenshteinAtMost(a, b string, limit int) bool {
	if absInt(len(a)-len(b)) > limit {
		return false
	}
	if a == b {
		return true
	}
	la, lb := len(a), len(b)
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		minRow := curr[0]
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
			if curr[j] < minRow {
				minRow = curr[j]
			}
		}
		if minRow > limit {
			return false
		}
		prev, curr = curr, prev
	}
	return prev[lb] <= limit
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
