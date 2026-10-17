package ontology

import (
	"fmt"
	"strings"
)

func resolveNarrativeBlock(blocks []NodeBodyBlock, op setNarrativeOp) (*NodeBodyBlock, error) {
	previous := normalizeNarrativeMarkdown(op.PreviousMarkdown)
	if strings.TrimSpace(previous) == "" {
		return nil, fmt.Errorf("%w: narrative replacement requires its previous markdown", errUnsupportedTarget)
	}
	var matching *NodeBodyBlock
	count := 0
	for index := range blocks {
		block := &blocks[index]
		if block.Kind != NodeBodyBlockKindNarrative || normalizeNarrativeMarkdown(block.Markdown) != previous {
			continue
		}
		if block.Range.Start == op.Range.Start && block.Range.End == op.Range.End {
			return block, nil
		}
		matching = block
		count++
	}
	if count == 1 {
		return matching, nil
	}
	if count > 1 {
		return nil, fmt.Errorf("%w: narrative content is ambiguous for %s", errCollectionDrift, op.Ref.String())
	}
	return nil, fmt.Errorf("%w: narrative content changed for %s", errCollectionDrift, op.Ref.String())
}

func normalizeNarrativeMarkdown(markdown string) string {
	return strings.TrimRight(strings.ReplaceAll(markdown, "\r\n", "\n"), " \t\r\n")
}

func preserveNarrativeSuffix(raw, markdown string) string {
	trimmedRaw := strings.TrimRight(raw, " \t\r\n")
	suffix := raw[len(trimmedRaw):]
	prefix := raw[:len(raw)-len(strings.TrimLeft(raw, " \t\r\n"))]
	replacement := strings.TrimRight(markdown, " \t\r\n")
	if !strings.HasPrefix(replacement, "\n") && !strings.HasPrefix(replacement, "\r\n") {
		replacement = strings.TrimRight(prefix, " \t") + replacement
	}
	return replacement + suffix
}
