package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
)

type semanticTextBody struct {
	key, fingerprint string
	start, end       int
}

type semanticDeliveryGroup struct {
	item   actions.DedupeItem
	bodies []actions.DedupeItem
}

func finalizeSemanticQueryResponse(tracker *sessionTracker, original semanticQueryResponse, encoded []byte, budgetChars int) ([]byte, error) {
	if tracker == nil {
		return encoded, nil
	}
	if err := tracker.operationContext().Err(); err != nil {
		return nil, err
	}
	if original.Compact != nil {
		return finalizeCompactSemanticQueryResponse(tracker, encoded, budgetChars)
	}
	var rendered semanticQueryResponse
	if err := json.Unmarshal(encoded, &rendered); err != nil {
		return nil, err
	}
	groups := detailedSemanticDeliveryGroups(rendered)
	if len(groups) == 0 {
		return encoded, nil
	}
	items := make([]actions.DedupeItem, 0, len(groups))
	for _, group := range groups {
		items = append(items, group.item)
	}
	reservation := tracker.Reserve(items)
	var emitted []actions.DedupeItem
	defer func() { reservation.Commit(emitted) }()

	blocked := make(map[actions.DedupeItem]bool)
	for _, group := range groups {
		if !reservation.Allowed(group.item.Key, group.item.Fingerprint) {
			for _, body := range group.bodies {
				blocked[body] = true
				rendered.DedupeHits++
			}
		}
	}
	for i := range rendered.Matches {
		match := &rendered.Matches[i]
		item, ok := detailedSemanticDeliveryItem(*match)
		if !ok || !blocked[item] {
			continue
		}
		match.FullFileContent, match.ExcerptContent, match.OutlineContent, match.SignatureContent = "", "", "", ""
		match.ContentDeduped = true
		match.Included = false
	}
	rendered.Text = suppressSemanticTextBodies(original, rendered.Text, blocked)

	// As in compact output, final qualifications must fit without shortening a
	// reserved body. Failed shaping releases ownership without publishing it.
	diagnostics := rendered.Diagnostics
	rendered.Diagnostics = nil
	final, err := json.Marshal(rendered)
	if err != nil {
		return nil, err
	}
	if budgetChars > 0 && len(final) > budgetChars {
		return nil, fmt.Errorf("detailed response exceeds budget %d while preserving dedupe qualifications", budgetChars)
	}
	if diagnostics != nil {
		rendered.Diagnostics = diagnostics
		final, err = json.Marshal(rendered)
		if err != nil {
			return nil, err
		}
	}
	if err := tracker.operationContext().Err(); err != nil {
		return nil, err
	}
	for _, item := range items {
		if reservation.Allowed(item.Key, item.Fingerprint) {
			emitted = append(emitted, item)
		}
	}
	return final, nil
}

func detailedSemanticDeliveryGroups(resp semanticQueryResponse) []semanticDeliveryGroup {
	var groups []semanticDeliveryGroup
	byKey := make(map[string]int)
	seen := make(map[actions.DedupeItem]bool)
	for _, match := range resp.Matches {
		if item, ok := detailedSemanticDeliveryItem(match); ok && !seen[item] {
			seen[item] = true
			index, exists := byKey[item.Key]
			if !exists {
				index = len(groups)
				byKey[item.Key] = index
				groups = append(groups, semanticDeliveryGroup{item: item})
			}
			groups[index].bodies = append(groups[index].bodies, item)
		}
	}
	for i := range groups {
		group := &groups[i]
		if len(group.bodies) < 2 {
			continue
		}
		// The store owns one fingerprint per canonical source. Multiple surviving
		// representations share one decision without claiming any unseen sibling.
		fingerprints := make([]string, 0, len(group.bodies))
		for _, body := range group.bodies {
			fingerprints = append(fingerprints, body.Fingerprint)
		}
		sort.Strings(fingerprints)
		group.item.Fingerprint = fingerprintText("semantic-body-set-v1\x00" + strings.Join(fingerprints, "\x00"))
	}
	return groups
}

func detailedSemanticDeliveryItem(match SemanticMatchPayload) (actions.DedupeItem, bool) {
	body := compactSourceBody(match)
	key := semanticMatchKey(match)
	if body == nil || body.Deduped || body.Kind == string(planStub) || body.Content == "" || key == "" {
		return actions.DedupeItem{}, false
	}
	return actions.DedupeItem{Key: key, Fingerprint: fingerprintText(body.Content)}, true
}

func suppressSemanticTextBodies(original semanticQueryResponse, text string, blocked map[actions.DedupeItem]bool) string {
	if len(blocked) == 0 || text == "" {
		return text
	}
	// Budget trimming retains an authored prefix, possibly with a trim marker.
	// Remove only renderer-owned ranges within that prefix, never matching body
	// strings against unrelated previews or other sources' identical content.
	prefix := 0
	for prefix < len(text) && prefix < len(original.Text) && text[prefix] == original.Text[prefix] {
		prefix++
	}
	for prefix > 0 && prefix < len(text) && !utf8.RuneStart(text[prefix]) {
		prefix--
	}
	var b strings.Builder
	cursor := 0
	for _, body := range original.textBodies {
		if !blocked[actions.DedupeItem{Key: body.key, Fingerprint: body.fingerprint}] || body.start >= prefix {
			continue
		}
		b.WriteString(text[cursor:body.start])
		cursor = min(body.end, prefix)
	}
	b.WriteString(text[cursor:])
	return strings.TrimSpace(b.String())
}
