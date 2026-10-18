package markdownsyntax

// Span is a half-open byte range in authored source. Present distinguishes an
// empty authored component from an absent component.
type Span struct {
	Start, End int
	Present    bool
}

func (s Span) Valid() bool { return s.Present && s.Start >= 0 && s.End >= s.Start }
func (s Span) Text(content string) string {
	if !s.Valid() || s.End > len(content) {
		return ""
	}
	return content[s.Start:s.End]
}
