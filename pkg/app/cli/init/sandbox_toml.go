package init

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// tomlBlock is a fenced block at the top of a TOML file, where top-level
// keys must be. When the file already has a [table] for one of the block's
// dotted keys, those keys go in a second fenced block inside that table
// instead, because TOML rejects a table defined both ways.
type tomlBlock string

func (b tomlBlock) fences() (string, string) {
	return "# BEGIN RZM INIT BLOCK: " + string(b), "# END RZM INIT BLOCK: " + string(b)
}

// in is the block that holds this block's keys under a person's [root].
func (b tomlBlock) in(root string) tomlBlock { return tomlBlock("[" + root + "] " + string(b)) }

// split returns the text before, inside, and after the block.
func (b tomlBlock) split(data []byte) (before, body, after string, found bool) {
	begin, end := b.fences()
	text := string(data)
	i := strings.Index(text, begin)
	j := strings.Index(text, end)
	if i < 0 || j < i+len(begin) {
		return text, "", "", false
	}
	// The newline after the fence and the blank line write adds belong to
	// the block, with either line ending.
	after = text[j+len(end):]
	for range 2 {
		after = strings.TrimPrefix(strings.TrimPrefix(after, "\r"), "\n")
	}
	return text[:i], strings.ReplaceAll(strings.TrimSpace(text[i+len(begin):j]), "\r\n", "\n"), after, true
}

// inner finds the block inside a person's [root] table and returns the file
// without it, and its keys with the root put back.
func (b tomlBlock) inner(text, root string) (rest string, keys []string) {
	begin, end := b.in(root).fences()
	i := strings.Index(text, begin)
	j := strings.Index(text, end)
	if i < 0 || j < i+len(begin) {
		return text, nil
	}
	for _, line := range strings.Split(strings.TrimSpace(text[i+len(begin):j]), "\n") {
		keys = append(keys, root+"."+strings.TrimSpace(line))
	}
	after := strings.TrimPrefix(strings.TrimPrefix(text[j+len(end):], "\r"), "\n")
	return text[:i] + after, keys
}

// parts reads Rhizome's blocks out of a file: the rest of the file, and the
// block's lines in their written order, nil when there is no block.
func (b tomlBlock) parts(data []byte) (string, []string) {
	before, body, after, found := b.split(data)
	rest := before + after
	var lines []string
	if found {
		lines = strings.Split(body, "\n")
	}
	for _, root := range tomlBlockRoots {
		var keys []string
		if rest, keys = b.inner(rest, root); keys != nil {
			lines = append(lines, keys...)
		}
	}
	// The profile lists comments first and keys by root, so moving a root's
	// keys into its table reads back the same.
	sort.SliceStable(lines, func(i, j int) bool {
		a, _ := tomlKey(lines[i])
		c, _ := tomlKey(lines[j])
		return a < c
	})
	return rest, lines
}

func (b tomlBlock) read(data []byte) ([]byte, error) {
	if _, lines := b.parts(data); lines != nil {
		return []byte(strings.Join(lines, "\n")), nil
	}
	return nil, nil
}

func (b tomlBlock) write(data, part []byte) ([]byte, error) {
	rest, _ := b.parts(data)
	if part == nil {
		if strings.TrimSpace(rest) == "" {
			return nil, nil
		}
		return []byte(rest), nil
	}
	tables, why := tomlPlacement(rest, part)
	var top []string
	moved := map[string][]string{}
	for _, line := range strings.Split(string(part), "\n") {
		if root, _ := tomlKey(line); why == "" && tables[root] {
			moved[root] = append(moved[root], strings.TrimPrefix(line, root+"."))
		} else {
			top = append(top, line)
		}
	}
	if len(moved) > 0 {
		var out strings.Builder
		lines := strings.SplitAfter(rest, "\n")
		quoted := tomlQuotedLines(lines)
		for i, line := range lines {
			out.WriteString(line)
			root, sub := tomlKey(line)
			if keys := moved[root]; keys != nil && sub == "" && !quoted[i] && tomlHeader(line) == 1 {
				if !strings.HasSuffix(line, "\n") {
					out.WriteString("\n")
				}
				begin, end := b.in(root).fences()
				out.WriteString(begin + "\n" + strings.Join(keys, "\n") + "\n" + end + "\n")
				delete(moved, root)
			}
		}
		rest = out.String()
	}
	begin, end := b.fences()
	block := begin + "\n" + strings.Join(top, "\n") + "\n" + end + "\n"
	if strings.TrimSpace(rest) != "" {
		block += "\n"
	}
	return []byte(block + rest), nil
}

// conflict reports a definition outside the blocks that the block's keys
// would redefine, which TOML rejects.
func (b tomlBlock) conflict(data, part []byte) string {
	rest, _ := b.parts(data)
	_, why := tomlPlacement(rest, part)
	return why
}

// tomlBlockRoots are the top-level names the Codex profile defines.
var tomlBlockRoots = []string{"default_permissions", "features", "permissions"}

// tomlPlacement finds the roots of part's keys that a person already has as
// a plain [root] table, where those keys must go, and says why part cannot
// be added at all: the person defines the same key, or defines the root in a
// way a block cannot join.
func tomlPlacement(rest string, part []byte) (map[string]bool, string) {
	want := map[string]map[string]bool{}
	for _, line := range strings.Split(string(part), "\n") {
		if root, sub := tomlKey(line); root != "" {
			if want[root] == nil {
				want[root] = map[string]bool{}
			}
			want[root][sub] = true
		}
	}
	defines := func(name string) string {
		return fmt.Sprintf("it already defines %s; add the loopback profile Rhizome writes by hand", name)
	}
	tables := map[string]bool{}
	table, topLevel := "", true
	lines := strings.Split(rest, "\n")
	quoted := tomlQuotedLines(lines)
	for i, line := range lines {
		root, sub := tomlKey(line)
		if root == "" || quoted[i] {
			continue
		}
		if kind := tomlHeader(line); kind > 0 {
			topLevel, table = false, ""
			if want[root] == nil {
				continue
			}
			// A plain [root] can take the keys; a key that is the root
			// itself, a sub-table, or an array of tables cannot.
			if kind == 2 || sub != "" || want[root][""] {
				return nil, defines(root)
			}
			tables[root], table = true, root
			continue
		}
		if topLevel && want[root] != nil {
			return nil, defines(root)
		}
		if table != "" && want[table][root] {
			return nil, defines(table + "." + root)
		}
	}
	return tables, ""
}

// tomlQuotedLines marks the lines that start inside a multiline string,
// which are text, not keys or tables.
func tomlQuotedLines(lines []string) []bool {
	quoted := make([]bool, len(lines))
	open := ""
	for i, line := range lines {
		quoted[i] = open != ""
		for line != "" {
			if open != "" {
				end := tomlStringEnd(line, open)
				if end < 0 {
					break
				}
				line, open = line[end:], ""
				continue
			}
			start := strings.IndexAny(line, `"'#`)
			if start < 0 || line[start] == '#' {
				break
			}
			if delim := line[start:min(start+3, len(line))]; delim == `"""` || delim == "'''" {
				line, open = line[start+3:], delim
				continue
			}
			end := tomlStringEnd(line[start+1:], line[start:start+1])
			if end < 0 {
				break
			}
			line = line[start+1+end:]
		}
	}
	return quoted
}

// tomlHeader is 1 for a [table] line, 2 for an [[array]] line, else 0.
func tomlHeader(line string) int {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "[[") {
		return 2
	}
	if strings.HasPrefix(t, "[") {
		return 1
	}
	return 0
}

// tomlKey is the first two parts of a header's or assignment's key, without
// quotes; both are empty for a comment or blank line.
func tomlKey(line string) (root, sub string) {
	t := strings.TrimLeft(strings.TrimSpace(line), "[ \t")
	if t == "" || t[0] == '#' {
		return "", ""
	}
	root, t = tomlKeyPart(t)
	t = strings.TrimLeft(t, " \t")
	if strings.HasPrefix(t, ".") {
		sub, _ = tomlKeyPart(strings.TrimLeft(t[1:], " \t"))
	}
	return root, sub
}

// tomlStringEnd is the index just past quote, the closing delimiter of a
// string whose text starts s, or -1. A basic string's quote can be escaped.
func tomlStringEnd(s, quote string) int {
	for i := 0; i < len(s); i++ {
		if quote[0] == '"' && s[i] == '\\' {
			i++
		} else if strings.HasPrefix(s[i:], quote) {
			return i + len(quote)
		}
	}
	return -1
}

func tomlKeyPart(t string) (part, rest string) {
	if t != "" && (t[0] == '"' || t[0] == '\'') {
		if end := tomlStringEnd(t[1:], t[:1]); end >= 0 {
			part, rest = t[1:end], t[end+1:]
			if t[0] == '"' {
				// Escapes name the same key as the plain spelling.
				if unquoted, err := strconv.Unquote(`"` + part + `"`); err == nil {
					part = unquoted
				}
			}
			return part, rest
		}
	}
	end := strings.IndexAny(t, " \t.=]\"'")
	if end < 0 {
		return t, ""
	}
	return t[:end], t[end:]
}
