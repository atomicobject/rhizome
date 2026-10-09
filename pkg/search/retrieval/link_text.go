package retrieval

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/queryframe"
)

// LinkTextSource reads note links whose label or line contains a term.
type LinkTextSource interface {
	NoteLinkTextMatches(ctx context.Context, terms []string, limit int) ([]semdb.LinkTextRow, error)
}

// LinkTextRetriever finds notes by the words other notes use when they link
// to them (SPEC-0120 US1). A label counts fully and the line around the link
// counts half; each linking note counts once and at most linkTextSourceWeight,
// notes that link to many targets count less, and agreeing notes combine as
// independent evidence, so one stray label cannot match a label many notes use.
type LinkTextRetriever struct {
	Store LinkTextSource
}

const (
	// linkTextLineWeight is how much a match in the line around a link counts
	// relative to a match in its label.
	linkTextLineWeight = 0.5
	// linkTextHubTargets is how many distinct targets a note may link to
	// before each of its links counts less.
	linkTextHubTargets = 20
	// linkTextAliasNotes is how many linking notes must share a label before
	// it counts like an alias of the target.
	linkTextAliasNotes = 2
	// linkTextSourceWeight is the most one linking note can contribute.
	linkTextSourceWeight = 0.5
)

func (r *LinkTextRetriever) Name() string { return "link_text" }

func (r *LinkTextRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	frame := queryframe.Extract(spec.Text)
	if r.Store == nil || len(frame.SupportTermGroups) == 0 || len(spec.Filters.NoteTypes) > 0 {
		return nil, nil
	}
	var terms []string
	for _, group := range frame.SupportTermGroups {
		terms = append(terms, group...)
	}
	rows, err := r.Store.NoteLinkTextMatches(ctx, terms, 0)
	if err != nil {
		return nil, err
	}

	type link struct {
		src    string
		score  float64
		label  string
		labels []string
	}
	// One linking note counts once per target, even when it links by both
	// wiki and Markdown syntax and so has two edge rows.
	bySource := map[[2]string]link{}
	for _, row := range rows {
		dst, ok := cleanTypedNotePath(row.DstPath)
		if !ok || row.SrcPath == row.DstPath || !pathMatchesPrefix(dst, spec.Filters.PathPrefixes) || !spec.Filters.AllowsTestPath(dst) {
			continue
		}
		best := link{src: row.SrcPath}
		for _, entry := range semdb.ParseLinkText(row.LinkText) {
			if entry.Label != "" && !slices.Contains(best.labels, entry.Label) {
				best.labels = append(best.labels, entry.Label)
			}
			if score := frame.ConceptCoverage(entry.Label); score > best.score {
				best.score, best.label = score, entry.Label
			}
			if score := linkTextLineWeight * frame.ConceptCoverage(entry.Line); score > best.score {
				best.score, best.label = score, ""
			}
		}
		if best.score == 0 {
			continue
		}
		if row.SrcTargets > linkTextHubTargets {
			best.score *= float64(linkTextHubTargets) / float64(row.SrcTargets)
		}
		key := [2]string{dst, row.SrcPath}
		if prior, ok := bySource[key]; ok {
			for _, label := range prior.labels {
				if !slices.Contains(best.labels, label) {
					best.labels = append(best.labels, label)
				}
			}
			if prior.score >= best.score {
				best.score, best.label = prior.score, prior.label
			}
		}
		bySource[key] = best
	}
	byTarget := map[string][]link{}
	for key, l := range bySource {
		byTarget[key[0]] = append(byTarget[key[0]], l)
	}

	type scored struct {
		path  string
		score float64
		links []link
		alias string
	}
	// Only labels that name every query concept compete to be the agreed one.
	naming := func(l link) []string {
		return slices.DeleteFunc(slices.Clone(l.labels), func(label string) bool { return frame.ConceptCoverage(label) < 1 })
	}
	targets := make([]scored, 0, len(byTarget))
	for dst, links := range byTarget {
		sort.Slice(links, func(i, j int) bool {
			if links[i].score != links[j].score {
				return links[i].score > links[j].score
			}
			return links[i].src < links[j].src
		})
		missing := 1.0
		for _, l := range links {
			missing *= 1 - linkTextSourceWeight*l.score
		}
		targets = append(targets, scored{path: dst, score: 1 - missing, links: links, alias: consensusLabel(links, naming)})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].score != targets[j].score {
			return targets[i].score > targets[j].score
		}
		return targets[i].path < targets[j].path
	})
	limit := spec.Limits.Total
	if limit <= 0 {
		limit = 25
	}
	// A target with an agreed name stays past the limit, because pruning
	// protects it later and partial matches must not crowd it out first.
	kept := targets[:0]
	for i, t := range targets {
		if i < limit || t.alias != "" {
			kept = append(kept, t)
		}
	}
	targets = kept

	out := make([]search.Candidate, 0, len(targets))
	for _, t := range targets {
		sources := make([]string, 0, 3)
		label := ""
		for _, l := range t.links[:min(3, len(t.links))] {
			sources = append(sources, l.src)
			if label == "" {
				label = l.label
			}
		}
		details := map[string]string{"sources": strings.Join(sources, ", "), "linking_notes": strconv.Itoa(len(t.links))}
		if label != "" {
			details["label"] = label
		}
		if t.alias != "" {
			details[search.LinkTextAliasDetail] = t.alias
		}
		h := knowledge.NoteHandle(t.path)
		out = append(out, search.Candidate{
			Handle:     h,
			Owner:      h,
			Evidence:   []search.Evidence{{Type: "link_text_match", RawScore: t.score, Source: "link_text", Details: details}},
			Type:       "note",
			NoteID:     t.path,
			Path:       t.path,
			Title:      titleFromPath(t.path),
			ChunkIndex: -1,
		})
	}
	return out, nil
}

// consensusLabel returns the label the most linking notes use, case-insensitively,
// when at least linkTextAliasNotes of them agree. One note's label is an
// opinion; several notes using the same words make it a name.
func consensusLabel[T any](links []T, labels func(T) []string) string {
	counts := map[string]int{}
	spelling := map[string]string{}
	for _, l := range links {
		seen := map[string]bool{}
		for _, label := range labels(l) {
			key := strings.ToLower(label)
			if seen[key] {
				continue
			}
			seen[key] = true
			counts[key]++
			if spelling[key] == "" {
				spelling[key] = label
			}
		}
	}
	best := ""
	for key, n := range counts {
		if n < linkTextAliasNotes {
			continue
		}
		if best == "" || n > counts[best] || (n == counts[best] && key < best) {
			best = key
		}
	}
	return spelling[best]
}
