package semantic

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

func stableNonEmpty(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.Join(strings.Fields(strings.TrimSpace(item)), " ")
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

const (
	defaultRelatedDocLimit       = 5
	defaultModuleRelatedDocLimit = 8
)

// AnchorDocLinkSource provides batched doc-link lookup for code embedding synthesis.
type AnchorDocLinkSource interface {
	DocLinksForAnchors(ctx context.Context, anchorIDs []string, limitPerAnchor int) (map[string][]codeanchor.DocLink, error)
}

func relatedDocLabelsForAnchors(ctx context.Context, source IntelSource, anchors []codeanchor.IntelAnchor, limitPerAnchor int) map[string][]string {
	linkSource, ok := source.(AnchorDocLinkSource)
	if !ok || len(anchors) == 0 {
		return nil
	}
	if limitPerAnchor <= 0 {
		limitPerAnchor = defaultRelatedDocLimit
	}
	ids := make([]string, 0, len(anchors))
	for _, anchor := range anchors {
		if id := strings.TrimSpace(anchor.AnchorID); id != "" {
			ids = append(ids, id)
		}
	}
	linksByAnchor, err := linkSource.DocLinksForAnchors(ctx, ids, limitPerAnchor)
	if err != nil || len(linksByAnchor) == 0 {
		return nil
	}

	out := make(map[string][]string, len(linksByAnchor))
	for id, links := range linksByAnchor {
		labels := make([]string, 0, len(links))
		for _, link := range links {
			if label := docLinkLabel(link); label != "" {
				labels = append(labels, label)
			}
		}
		labels = stableNonEmpty(labels)
		if len(labels) > limitPerAnchor {
			labels = labels[:limitPerAnchor]
		}
		if len(labels) > 0 {
			out[id] = labels
		}
	}
	return out
}

func attachRelatedDocs(anchors []codeanchor.IntelAnchor, relatedByAnchor map[string][]string) []codeanchor.IntelAnchor {
	if len(anchors) == 0 || len(relatedByAnchor) == 0 {
		return anchors
	}
	out := append([]codeanchor.IntelAnchor(nil), anchors...)
	moduleDocs := relatedDocsForModule(out, relatedByAnchor)
	for i := range out {
		out[i].RelatedDocs = relatedByAnchor[out[i].AnchorID]
		if strings.EqualFold(out[i].Kind, "module") && len(moduleDocs) > 0 {
			out[i].RelatedDocs = moduleDocs
		}
	}
	return out
}

func attachRelatedDocsByPath(moduleByPath map[string][]codeanchor.IntelAnchor, relatedByAnchor map[string][]string) map[string][]codeanchor.IntelAnchor {
	if len(moduleByPath) == 0 || len(relatedByAnchor) == 0 {
		return moduleByPath
	}
	out := make(map[string][]codeanchor.IntelAnchor, len(moduleByPath))
	for path, anchors := range moduleByPath {
		out[path] = attachRelatedDocs(anchors, relatedByAnchor)
	}
	return out
}

func flattenPathAnchors(moduleByPath map[string][]codeanchor.IntelAnchor) []codeanchor.IntelAnchor {
	if len(moduleByPath) == 0 {
		return nil
	}
	out := make([]codeanchor.IntelAnchor, 0)
	for _, anchors := range moduleByPath {
		out = append(out, anchors...)
	}
	return out
}

func relatedDocsForModule(anchors []codeanchor.IntelAnchor, relatedByAnchor map[string][]string) []string {
	if len(anchors) == 0 {
		return nil
	}
	var docs []string
	for _, anchor := range anchors {
		docs = append(docs, relatedByAnchor[anchor.AnchorID]...)
		docs = append(docs, anchor.RelatedDocs...)
	}
	docs = stableNonEmpty(docs)
	if len(docs) > defaultModuleRelatedDocLimit {
		docs = docs[:defaultModuleRelatedDocLimit]
	}
	return docs
}

func docLinkLabel(link codeanchor.DocLink) string {
	label := strings.Join(strings.Fields(strings.TrimSpace(link.Label)), " ")
	path := strings.TrimSpace(firstNonEmpty(link.SrcPath, link.DstPath))
	if label == "" && path != "" {
		base := filepath.Base(filepath.ToSlash(path))
		label = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if label == "" {
		return ""
	}
	label = truncateDeterministic(label, 100)
	if path == "" || strings.EqualFold(label, path) {
		return label
	}
	return truncateDeterministic(label+" ("+filepath.ToSlash(path)+")", 160)
}
