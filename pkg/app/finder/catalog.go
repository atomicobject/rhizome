// Package finder scores the ⌘P catalog of notes and code files (SPEC-0126
// US8). The catalog is an immutable snapshot of Rhizome's own index; Live
// rebuilds it off the request path when the index changes.
package finder

import (
	"path"
	"sort"
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/store"
	"github.com/junegunn/fzf/src/util"
)

// Kind separates notes, which open as tabs, from code files, which open in
// Explorer.
type Kind string

const (
	KindNote Kind = "note"
	KindCode Kind = "code"
)

// Item is one catalog entry. Code files use their file name as the title.
type Item struct {
	Kind  Kind
	Path  string
	Title string
	Type  string
	Lang  string
	Mtime int64
}

type entry struct {
	Item
	title, path util.Chars
	lowerTitle  string
}

// Catalog is an immutable, prepared snapshot. Searches may run concurrently.
type Catalog struct {
	entries []entry
	byPath  map[string]int
	// recentIdx holds up to recentNotes note indexes, newest change first.
	recentIdx []int
}

// NewCatalog prepares index rows for matching. Notes come first so a stable
// sort keeps them ahead of code files on equal scores.
func NewCatalog(notes []store.FinderNoteRow, code []store.FinderCodeRow) *Catalog {
	c := &Catalog{entries: make([]entry, 0, len(notes)+len(code)), byPath: make(map[string]int, len(notes)+len(code))}
	for _, note := range notes {
		title := markdownsyntax.PlainMarkdownTitle(note.Title)
		if title == "" {
			title = strings.TrimSuffix(path.Base(note.Path), path.Ext(note.Path))
		}
		c.add(Item{Kind: KindNote, Path: note.Path, Title: title, Type: note.Type, Mtime: note.Mtime})
	}
	for _, file := range code {
		c.add(Item{Kind: KindCode, Path: file.Path, Title: path.Base(file.Path), Lang: file.Lang})
	}
	for i := range c.entries {
		if c.entries[i].Kind == KindNote {
			c.recentIdx = append(c.recentIdx, i)
		}
	}
	sort.SliceStable(c.recentIdx, func(i, j int) bool {
		return c.entries[c.recentIdx[i]].Mtime > c.entries[c.recentIdx[j]].Mtime
	})
	c.recentIdx = c.recentIdx[:min(len(c.recentIdx), recentNotes)]
	return c
}

func (c *Catalog) add(item Item) {
	if _, dup := c.byPath[item.Path]; dup {
		return
	}
	c.byPath[item.Path] = len(c.entries)
	c.entries = append(c.entries, entry{
		Item:       item,
		title:      util.ToChars([]byte(item.Title)),
		path:       util.ToChars([]byte(item.Path)),
		lowerTitle: strings.ToLower(item.Title),
	})
}

// Len reports the number of items.
func (c *Catalog) Len() int {
	if c == nil {
		return 0
	}
	return len(c.entries)
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}
