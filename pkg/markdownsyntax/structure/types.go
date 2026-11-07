package structure

import "github.com/atomicobject/rhizome/pkg/markdownsyntax"

type Shape string

const (
	Section      Shape = "SECTION"
	ListItem     Shape = "LIST_ITEM"
	CheckboxItem Shape = "CHECKBOX_ITEM"
)

type ByteRange struct{ Start, End int }

func (r ByteRange) Len() int {
	if r.End <= r.Start {
		return 0
	}
	return r.End - r.Start
}
func (r ByteRange) Valid(n int) bool { return r.Start >= 0 && r.End >= r.Start && r.End <= n }

type InlineFieldSpan struct {
	Key, Value                                  string
	LineRange, KeyRange, ValueRange, WholeRange ByteRange
	PropertyKey, AuthoringStyle                 string
}

func frontmatterRange(content string) ByteRange {
	_, _, end, ok := markdownsyntax.FrontmatterSpan(content)
	if !ok {
		return ByteRange{}
	}
	return ByteRange{End: end}
}
