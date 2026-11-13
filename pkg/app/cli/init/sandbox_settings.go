// Host sandbox settings that let a sandboxed `rzm agent` reach this
// repository's runtime on 127.0.0.1. The grant is network access to loopback
// only: the command stays sandboxed, because running rzm outside the sandbox
// would also run whatever binary .rhizome/config.yml selects, which a
// sandboxed agent can edit. Rhizome owns one part of each file and nothing
// else in it.
package init

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const sandboxSettingKey = "#rhizome-sandbox"

// generatedSetting is the part of a host settings file Rhizome owns. desired
// is nil when Rhizome's part should go.
type generatedSetting struct {
	rel     string
	format  settingFormat
	desired []byte
}

func (s generatedSetting) key() string { return s.rel + sandboxSettingKey }

// settingFormat reads and writes Rhizome's part of one kind of file.
type settingFormat interface {
	// read returns Rhizome's part as written, nil when absent.
	read(data []byte) ([]byte, error)
	// write sets the part (nil removes it) and returns the file, nil when
	// nothing else is left in it.
	write(data, part []byte) ([]byte, error)
	// conflict says why the part cannot be added to data, or "".
	conflict(data, part []byte) string
}

// codexLoopbackProfile is Codex's own workspace sandbox plus loopback, which
// Codex admits only through its network proxy. Keys are dotted so the block
// can sit above a person's tables.
const codexLoopbackProfile = `# Lets sandboxed commands reach this repository's Rhizome process on 127.0.0.1 and no other host.
default_permissions = "rhizome"
features.network_proxy = true
permissions.rhizome.extends = ":workspace"
permissions.rhizome.network.enabled = true
permissions.rhizome.network.domains."127.0.0.1" = "allow"`

// planSandboxSettings lets Claude Code's sandbox connect to localhost ports
// (macOS) and gives Codex a profile that allows 127.0.0.1 and no other host.
func planSandboxSettings(harnesses AgentHarnesses) []generatedSetting {
	claude := generatedSetting{rel: ".claude/settings.json", format: jsonKey{"sandbox", "network", "allowLocalBinding"}}
	if harnesses.HasClaude {
		claude.desired = []byte("true")
	}
	codex := generatedSetting{rel: ".codex/config.toml", format: tomlBlock("codex-sandbox")}
	if harnesses.HasCodex {
		codex.desired = []byte(codexLoopbackProfile)
	}
	return []generatedSetting{claude, codex}
}

// jsonKey is the value at a path of object keys.
type jsonKey []string

func (path jsonKey) read(data []byte) ([]byte, error) {
	obj, err := parseOrderedObject(data)
	if err != nil {
		return nil, err
	}
	for i, name := range path {
		raw, ok := obj.values[name]
		if !ok {
			return nil, nil
		}
		if i == len(path)-1 {
			return canonicalJSON(raw), nil
		}
		if obj, err = parseOrderedObject(raw); err != nil {
			return nil, nil // not an object here, so the value is absent
		}
	}
	return nil, nil
}

func (path jsonKey) write(data, part []byte) ([]byte, error) {
	next, err := withJSONValue(data, path, part)
	if err != nil || next == nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, next, "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// conflict reports a non-object value where the path needs an object.
func (path jsonKey) conflict(data, _ []byte) string {
	obj, err := parseOrderedObject(data)
	for i, name := range path[:len(path)-1] {
		if err != nil {
			return fmt.Sprintf("%s is not a JSON object", strings.Join(path[:i], "."))
		}
		raw, ok := obj.values[name]
		if !ok {
			return ""
		}
		obj, err = parseOrderedObject(raw)
	}
	if err != nil {
		return fmt.Sprintf("%s is not a JSON object", strings.Join(path[:len(path)-1], "."))
	}
	return ""
}

// withJSONValue sets (or, with a nil value, removes) the value at path,
// keeping every other key in place and pruning objects the removal empties.
func withJSONValue(raw []byte, path []string, value json.RawMessage) ([]byte, error) {
	obj, err := parseOrderedObject(raw)
	if err != nil {
		return nil, err
	}
	name := path[0]
	next := value
	if len(path) > 1 {
		if next, err = withJSONValue(obj.values[name], path[1:], value); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	if next == nil {
		obj.remove(name)
	} else {
		obj.set(name, next)
	}
	if len(obj.keys) == 0 {
		return nil, nil
	}
	return obj.marshal(), nil
}

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
				end := strings.Index(line, open)
				if end < 0 {
					break
				}
				line, open = line[end+3:], ""
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
			// A one-line string: skip past its closing quote; a basic
			// string's quote can be escaped.
			end := start + 1
			for end < len(line) && line[end] != line[start] {
				if line[start] == '"' && line[end] == '\\' {
					end++
				}
				end++
			}
			if end >= len(line) {
				break
			}
			line = line[end+1:]
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

func tomlKeyPart(t string) (part, rest string) {
	if t != "" && (t[0] == '"' || t[0] == '\'') {
		if end := strings.IndexByte(t[1:], t[0]); end >= 0 {
			return t[1 : end+1], t[end+2:]
		}
	}
	end := strings.IndexAny(t, " \t.=]\"'")
	if end < 0 {
		return t, ""
	}
	return t[:end], t[end:]
}

// classifySetting reports the change a setting needs, or nil. A value with
// no record is someone's own choice, so it is never removed.
func classifySetting(projectRoot string, s generatedSetting, record *generatedFiles) (*change, error) {
	current, exists, err := readIfExists(filepath.Join(projectRoot, filepath.FromSlash(s.rel)))
	if err != nil {
		return nil, err
	}
	key := s.key()
	var part []byte
	if exists {
		if part, err = s.format.read(current); err != nil {
			if s.desired == nil {
				return nil, nil // leave an unrelated broken file alone
			}
			return nil, fmt.Errorf("read %s: %w", s.rel, err)
		}
	}
	written, recorded := record.Written[key]
	if s.desired == nil {
		if part == nil || !recorded || written != contentFingerprint(part) {
			record.forget(key)
			return nil, nil
		}
		return &change{key: key, rel: s.rel, group: key, removal: true, current: part, state: stateUnedited}, nil
	}
	if part == nil && s.format.conflict(current, s.desired) != "" {
		return nil, nil // applySetting says why
	}
	c := &change{key: key, rel: s.rel, group: key, current: part, desired: s.desired}
	c.desiredF = contentFingerprint(c.desired)
	c.state = classify(part != nil, part, c.desiredF, contentFingerprint, record, key)
	if c.state == stateCurrent && !recorded {
		return nil, nil // a matching value Rhizome did not write stays the person's
	}
	return c, nil
}

// settingConflict says why a wanted setting is left out, or "".
func settingConflict(projectRoot string, s generatedSetting) string {
	if s.desired == nil {
		return ""
	}
	current, _, err := readIfExists(filepath.Join(projectRoot, filepath.FromSlash(s.rel)))
	if err != nil {
		return ""
	}
	if part, _ := s.format.read(current); part != nil {
		return ""
	}
	if why := s.format.conflict(current, s.desired); why != "" {
		return fmt.Sprintf("skipping %s: %s", s.rel, why)
	}
	return ""
}

// printSettingConflicts warns about sandbox settings init cannot add, which
// otherwise leave no trace in a plan with nothing else to do.
func printSettingConflicts(out io.Writer, root string, res resolved) {
	for _, s := range res.files.plan.settings {
		if why := settingConflict(root, s); why != "" {
			fmt.Fprintf(out, "%s: %s\n", styleWarn(out, "Warning"), why)
		}
	}
}

// renderSetting is the settings file after a decided change, nil when
// nothing else was in it.
func renderSetting(current []byte, s generatedSetting, c *change) ([]byte, error) {
	part := s.desired
	if c.removal {
		part = nil
	}
	return s.format.write(current, part)
}

// applySetting writes a decided setting change.
func applySetting(projectRoot string, s generatedSetting, c *change, record *generatedFiles, report *syncReport) error {
	path := filepath.Join(projectRoot, filepath.FromSlash(s.rel))
	if c == nil {
		if why := settingConflict(projectRoot, s); why != "" {
			report.Warnings = append(report.Warnings, why)
		}
		return nil
	}
	if c.state == stateCurrent {
		record.recordWritten(c.key, c.desiredF)
		return nil
	}
	if !c.take {
		if c.asked {
			record.recordDeclined(c.key, c.desiredF)
			report.Declined = append(report.Declined, c.key)
		} else if c.state == stateEdited || c.state == stateUnknown {
			report.Kept = append(report.Kept, c.key)
		}
		return nil
	}
	current, exists, err := readIfExists(path)
	if err != nil {
		return err
	}
	next, err := renderSetting(current, s, c)
	if err != nil {
		return fmt.Errorf("update %s: %w", s.rel, err)
	}
	if c.removal {
		record.forget(c.key)
	} else {
		record.recordWritten(c.key, c.desiredF)
	}
	if next == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		report.Removed = append(report.Removed, s.rel)
		return nil
	}
	if err := ensureDirExists(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.WriteFile(path, next, 0o644); err != nil {
		return err
	}
	if exists {
		report.Updated = append(report.Updated, s.rel)
	} else {
		report.Created = append(report.Created, s.rel)
	}
	return nil
}
