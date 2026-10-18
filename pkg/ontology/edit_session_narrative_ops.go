package ontology

import (
	"fmt"
	"strings"
)

func (op setNarrativeOp) apply(state *documentState) error {
	snapshot, projection, err := projectState(state, op.Ref)
	if err != nil {
		return err
	}
	blocks := BuildNodeBody(projection, state.schema)
	block, err := resolveNarrativeBlock(blocks, op)
	if err != nil {
		return err
	}
	raw := snapshot.Content[block.Range.Start:block.Range.End]
	replacement := preserveNarrativeSuffix(raw, insertedDocumentText(state.content, op.Markdown))
	state.setContent(replaceRange(state.content, ByteRange{
		Start: block.Range.Start,
		End:   block.Range.End,
	}, replacement))
	return nil
}

func (op insertNarrativeOp) apply(state *documentState) error {
	snapshot, projection, err := projectState(state, op.Ref)
	if err != nil {
		return err
	}
	for _, block := range BuildNodeBody(projection, state.schema) {
		if block.Kind == NodeBodyBlockKindNarrative || block.Kind == NodeBodyBlockKindChildSection || block.Kind == NodeBodyBlockKindCollection {
			return fmt.Errorf("%w: %s already has body content", errUnsupportedTarget, op.Ref.String())
		}
	}
	bodyRange, _, ok := nodeBodyContentRange(projection)
	if !ok || bodyRange.Start < 0 || bodyRange.Start > len(snapshot.Content) {
		return fmt.Errorf("%w: no safe narrative insertion point for %s", errUnsupportedTarget, op.Ref.String())
	}
	replacement := strings.TrimRight(insertedDocumentText(state.content, op.Markdown), " \t\r\n") + documentEOL(state.content) + documentEOL(state.content)
	state.setContent(snapshot.Content[:bodyRange.Start] + replacement + snapshot.Content[bodyRange.Start:])
	return nil
}
