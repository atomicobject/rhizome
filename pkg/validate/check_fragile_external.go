package validate

// Opt-in fragile-external heading-link audit.
//
// WHY: SPEC-0054 keeps heading-text external links out of the default hygiene
// path because many are intentionally human-facing, but drifted or ambiguous
// `note#Heading` links need a deliberate repair surface. Candidate upgrades
// route through NodeLinkService so identifier-backed block IDs stay canonical.
// Coderefs: [[heading-rename-safety-and-fragile-external-links#^spec-0054-us1]]

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type FragileExternalClass string

const (
	FragileExternalLinkable  FragileExternalClass = "linkable"
	FragileExternalDrifted   FragileExternalClass = "heading_drifted"
	FragileExternalAmbiguous FragileExternalClass = "heading_ambiguous"
)

type FragileExternalData struct {
	SourceNote     string                  `json:"sourceNote"`
	TargetNote     string                  `json:"targetNote"`
	Fragment       string                  `json:"fragment"`
	Classification FragileExternalClass    `json:"classification"`
	StartByte      int                     `json:"startByte,omitempty"`
	EndByte        int                     `json:"endByte,omitempty"`
	Line           int                     `json:"line,omitempty"`
	Candidates     []FragileExternalTarget `json:"candidates,omitempty"`
	NearMatch      *FragileExternalTarget  `json:"nearMatch,omitempty"`
}

type FragileExternalTarget struct {
	Ref     ontology.NodeRef `json:"ref"`
	Heading string           `json:"heading"`
	Line    int              `json:"line,omitempty"`
	BlockID string           `json:"blockId,omitempty"`
}

type fragileHeadingRef struct {
	source   string
	target   string
	fragment string
	link     obsidian.WikilinkDetail
}

func RunFragileExternal(ctx context.Context, runCtx RunContext, opts Options) CheckResult {
	result := CheckResult{Name: CheckFragileExternal, OK: true}
	sources, err := markdownValidationSources(ctx, runCtx)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	allNotes := make([]string, 0, len(sources))
	for _, source := range sources {
		allNotes = append(allNotes, source.Path.String())
	}

	scopeNote := normalizeScopePath(opts.ScopeNote)
	scopeTarget := normalizeScopePath(opts.ScopeTarget)
	scopeRef := strings.TrimSpace(opts.ScopeRef)
	contentByPath := markdownSourceContentByPath(sources)
	getContent := func(p string) string {
		if c, ok := contentByPath[p]; ok {
			return c
		}
		return ""
	}

	cache := obsidian.BuildNotePathCacheWithAliases(allNotes, markdownSourceAliases(sources))
	sourceNotes := allNotes
	if scopeNote != "" {
		sourceNotes = []string{scopeNote}
	}

	var refs []fragileHeadingRef
	for _, source := range sourceNotes {
		body := getContent(source)
		if body == "" {
			continue
		}
		for _, link := range obsidian.ScanWikilinks(body, obsidian.DefaultWikilinkOptions) {
			if link.InsideCodeBlock {
				continue
			}
			rawTarget, fragment := splitFragment(link.Target)
			if rawTarget == "" || fragment == "" || strings.HasPrefix(fragment, "^") {
				continue
			}
			resolved, ok := cache.ResolveNote(rawTarget)
			if !ok || resolved == source {
				continue
			}
			if scopeTarget != "" && resolved != scopeTarget {
				continue
			}
			refs = append(refs, fragileHeadingRef{source: source, target: resolved, fragment: fragment, link: link})
		}
	}
	if len(refs) == 0 {
		result.Summary = "no fragile external heading references"
		return result
	}

	schema, _ := ontology.LoadSchema(runCtx.VaultPath)
	sectionsByPath := map[string][]FragileExternalTarget{}
	targetsFor := func(notePath string) []FragileExternalTarget {
		if cached, ok := sectionsByPath[notePath]; ok {
			return cached
		}
		body := getContent(notePath)
		snapshot, _ := ontology.BuildDocumentSnapshot(notePath, body, time.Time{})
		headingLine := map[int]obsidian.HeadingInfo{}
		for _, h := range obsidian.EnumerateHeadings(body) {
			headingLine[h.Line] = h
		}
		var targets []FragileExternalTarget
		if snapshot != nil {
			walkSections(snapshot.Sections, func(node *ontology.SectionNode) {
				if node == nil {
					return
				}
				line := 1 + strings.Count(body[:node.StartByte], "\n")
				heading := node.Title
				if info, ok := headingLine[line]; ok {
					heading = info.Text
				}
				ref := ontology.NodeRef{
					NotePath:  notePath,
					NodeID:    node.ID,
					Kind:      ontology.NodeKindSection,
					StartByte: node.StartByte,
					EndByte:   node.EndByte,
					Fragment:  heading,
				}
				if schema != nil {
					if projection, err := ontology.ProjectBoundNodeFromSnapshot(snapshot, schema, ref); err == nil && projection != nil {
						ref = projection.Ref
					}
				}
				targets = append(targets, FragileExternalTarget{
					Ref:     ref,
					Heading: heading,
					Line:    line,
					BlockID: strings.TrimPrefix(strings.TrimSpace(node.BlockID), "^"),
				})
			})
		}
		sectionsByPath[notePath] = targets
		return targets
	}

	var findings []Issue
	var fixes []FixAction
	for _, ref := range refs {
		normalizedFragment := obsidian.NormalizeWikilinkFragment(ref.fragment)
		matches := matchingTargets(targetsFor(ref.target), normalizedFragment)
		class := FragileExternalLinkable
		switch len(matches) {
		case 0:
			class = FragileExternalDrifted
		case 1:
			continue
		default:
			class = FragileExternalAmbiguous
		}

		candidates := matches
		var near *FragileExternalTarget
		if len(candidates) == 0 {
			if candidate, ok := nearHeadingTarget(targetsFor(ref.target), normalizedFragment); ok {
				near = &candidate
				candidates = []FragileExternalTarget{candidate}
			}
		}
		if scopeRef != "" && !targetsContainRef(candidates, scopeRef) {
			continue
		}
		data := FragileExternalData{
			SourceNote:     ref.source,
			TargetNote:     ref.target,
			Fragment:       ref.fragment,
			Classification: class,
			StartByte:      ref.link.Start,
			EndByte:        ref.link.End,
			Line:           ref.link.Line,
			Candidates:     candidates,
			NearMatch:      near,
		}
		message := fmt.Sprintf("%s links to missing heading %q in %s", ref.source, ref.fragment, ref.target)
		if class == FragileExternalAmbiguous {
			message = fmt.Sprintf("%s links to ambiguous heading %q in %s", ref.source, ref.fragment, ref.target)
		}
		issue := Issue{
			Code:    CheckFragileExternal,
			Path:    ref.source,
			Source:  ref.source,
			Target:  ref.target + "#" + ref.fragment,
			Line:    ref.link.Line,
			Message: message,
			Data:    mustMarshal(data),
		}
		issueKey, err := StableIssueKey(CheckFragileExternal, issue)
		if err != nil {
			result.OK = false
			result.Error = fmt.Sprintf("identify fragile-external issue: %v", err)
			return result
		}
		issue.Key = issueKey
		findings = append(findings, issue)
		if len(candidates) == 1 && candidates[0].Ref.Kind == ontology.NodeKindEmbedded {
			target := candidates[0]
			fixes = append(fixes, FixAction{
				ID:            fmt.Sprintf("upgrade-heading-link:%s:%d", ref.source, ref.link.Start),
				Check:         CheckFragileExternal,
				IssueCode:     CheckFragileExternal,
				Kind:          FixKindUpgradeToBlockID,
				Safety:        FixSafetyConfirm,
				Title:         fmt.Sprintf("Upgrade %s#%s to block ID", ref.target, ref.fragment),
				Summary:       "route the candidate node through the link-target service so identifier-backed anchors are preferred",
				InstanceCount: 1,
				IssueKeys:     []string{issueKey},
				AffectedPaths: []string{ref.source, ref.target},
				Edits: []FixEdit{{
					Kind:       FixKindUpgradeToBlockID,
					NotePath:   target.Ref.NotePath,
					SourcePath: ref.source,
					OldTarget:  ref.link.Target,
					StartByte:  ref.link.Start,
					EndByte:    ref.link.End,
					NodeID:     target.Ref.NodeID,
					Structural: target.Ref.Structural,
					BlockID:    target.BlockID,
				}},
			})
		}
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Line < findings[j].Line
	})
	result.IssueCount = len(findings)
	result.Issues = append(result.Issues, findings...)
	result.Fixes = append(result.Fixes, fixes...)
	if result.IssueCount == 0 {
		result.Summary = "no fragile external heading drift"
	} else {
		result.Summary = fmt.Sprintf("%d fragile external heading reference(s)", result.IssueCount)
	}
	return result
}

func normalizeScopePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return string(paths.Normalize(obsidian.NormalizeWithDefaultExt(path, ".md")))
}

func walkSections(nodes []*ontology.SectionNode, visit func(*ontology.SectionNode)) {
	for _, node := range nodes {
		if node == nil {
			continue
		}
		visit(node)
		walkSections(node.Children, visit)
	}
}

func matchingTargets(targets []FragileExternalTarget, normalized string) []FragileExternalTarget {
	var out []FragileExternalTarget
	for _, target := range targets {
		if obsidian.NormalizeWikilinkFragment(target.Heading) == normalized {
			out = append(out, target)
		}
	}
	return out
}

func nearHeadingTarget(targets []FragileExternalTarget, normalized string) (FragileExternalTarget, bool) {
	for _, target := range targets {
		if levenshteinAtMost(obsidian.NormalizeWikilinkFragment(target.Heading), normalized, fuzzyNearMatchDistance) {
			return target, true
		}
	}
	return FragileExternalTarget{}, false
}

func targetsContainRef(targets []FragileExternalTarget, raw string) bool {
	for _, target := range targets {
		if target.Ref.String() == raw || target.Ref.NodeID == raw || target.Ref.Structural == raw {
			return true
		}
	}
	return false
}
