package ontology

import (
	"sort"
	"strings"
)

// lineIndex holds the byte offset at which every line of a document starts.
// Build it once per traversal and resolve line numbers by binary search, so
// emitting N issues costs O(document + N log lines) rather than
// O(N x document).
type lineIndex []int

func newLineIndex(content string) lineIndex {
	starts := make(lineIndex, 1, strings.Count(content, "\n")+1)
	starts[0] = 0
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// lineAt returns the 1-based line containing offset. A negative offset yields
// 0; an offset at or past the end resolves to the last line start, which
// counts a trailing newline as opening a final empty line.
func (l lineIndex) lineAt(offset int) int {
	if offset < 0 {
		return 0
	}
	return sort.Search(len(l), func(i int) bool { return l[i] > offset })
}
