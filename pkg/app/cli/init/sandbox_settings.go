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
	conflict(data []byte) string
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
func (path jsonKey) conflict(data []byte) string {
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
// keys must be.
type tomlBlock string

func (b tomlBlock) fences() (string, string) {
	return "# BEGIN RZM INIT BLOCK: " + string(b), "# END RZM INIT BLOCK: " + string(b)
}

// split returns the text before, inside, and after the block.
func (b tomlBlock) split(data []byte) (before, body, after string, found bool) {
	begin, end := b.fences()
	text := string(data)
	i := strings.Index(text, begin+"\n")
	j := strings.Index(text, end)
	if i < 0 || j < i {
		return text, "", "", false
	}
	// The newline after the fence and the blank line write adds belong to
	// the block.
	after = strings.TrimPrefix(strings.TrimPrefix(text[j+len(end):], "\n"), "\n")
	return text[:i], strings.TrimSpace(text[i+len(begin)+1 : j]), after, true
}

func (b tomlBlock) read(data []byte) ([]byte, error) {
	if _, body, _, found := b.split(data); found {
		return []byte(body), nil
	}
	return nil, nil
}

func (b tomlBlock) write(data, part []byte) ([]byte, error) {
	before, _, after, _ := b.split(data)
	rest := before + after
	if part == nil {
		if strings.TrimSpace(rest) == "" {
			return nil, nil
		}
		return []byte(rest), nil
	}
	begin, end := b.fences()
	block := begin + "\n" + string(part) + "\n" + end + "\n"
	if strings.TrimSpace(rest) != "" {
		block += "\n"
	}
	return []byte(block + rest), nil
}

// conflict reports a definition outside the block that the block's keys
// would redefine, which TOML rejects.
func (b tomlBlock) conflict(data []byte) string {
	before, _, after, _ := b.split(data)
	for _, line := range strings.Split(before+after, "\n") {
		line = strings.TrimSpace(line)
		for _, name := range []string{"default_permissions", "features", "permissions"} {
			if strings.HasPrefix(line, "["+name) || strings.HasPrefix(line, "[["+name) ||
				strings.HasPrefix(line, name+" ") || strings.HasPrefix(line, name+"=") || strings.HasPrefix(line, name+".") {
				return fmt.Sprintf("it already defines %s; add the loopback profile Rhizome writes by hand", name)
			}
		}
	}
	return ""
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
	if part == nil && s.format.conflict(current) != "" {
		return nil, nil // applySetting says why
	}
	c := &change{key: key, rel: s.rel, group: key, current: part, desired: s.desired}
	c.desiredF = contentFingerprint(c.desired)
	c.state = classify(part != nil, part, c.desiredF, contentFingerprint, record, key)
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
	if why := s.format.conflict(current); why != "" {
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
