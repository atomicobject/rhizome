// Hook entries in .claude/settings.json and .codex/hooks.json: one
// marker-identified entry per hook point, matching every tool, that runs
// `rzm hook <harness> <event>`. Everything else in those files belongs to
// someone else and is preserved.
//
// Docs:
// - [[agent-surface-integration-modes#^SPEC-0045-US5-AC3]]
// - [[agent-surface-integration-modes#^SPEC-0045-US5-AC8]]
package init

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/atomicobject/rhizome/pkg/app/cli/hook"
)

// hookEntryGOOS picks the platform form of Claude Code entries; tests set it.
var hookEntryGOOS = runtime.GOOS

// hookEntriesKey is the record key suffix for a file's Rhizome entries.
const hookEntriesKey = "#rhizome-hooks"

// hookSettingsFiles are the files each harness reads project hooks from.
var hookSettingsFiles = map[string]string{
	hook.HarnessClaude: ".claude/settings.json",
	hook.HarnessCodex:  ".codex/hooks.json",
}

// generatedHookFile is a harness settings file in which only the Rhizome
// entries belong to Rhizome. desired is nil when the entries should go.
type generatedHookFile struct {
	rel     string
	harness string
	desired map[string]json.RawMessage // host event -> entry
}

func (f generatedHookFile) key() string { return f.rel + hookEntriesKey }

// hookEntry is the entry for one harness and hook point. Claude Code on
// Windows launches rzm.exe without a shell; Codex carries its Windows form
// in commandWindows, which it runs through cmd.exe /C.
func hookEntry(harness, point string) json.RawMessage {
	command := map[string]any{"type": "command", "command": hook.EntryCommand(harness, point), "timeout": hook.HostTimeoutSeconds}
	switch {
	case harness == hook.HarnessClaude && hookEntryGOOS == "windows":
		command["command"] = "rzm.exe"
		command["args"] = []string{"hook", harness, point}
	case harness == hook.HarnessCodex:
		command["commandWindows"] = "rzm.exe hook " + harness + " " + point
	}
	data, _ := json.Marshal(map[string]any{"hooks": []any{command}})
	return data
}

func desiredHookEntries(harness string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, point := range hook.HookPoints {
		out[hook.HostEvent(point)] = hookEntry(harness, point)
	}
	return out
}

// orderedObject is a JSON object that keeps its key order, so editing one
// key leaves the rest of a person's file as it was.
type orderedObject struct {
	keys   []string
	values map[string]json.RawMessage
}

func parseOrderedObject(data []byte) (*orderedObject, error) {
	obj := &orderedObject{values: map[string]json.RawMessage{}}
	if len(bytes.TrimSpace(data)) == 0 {
		return obj, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("not a JSON object")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		key := tok.(string)
		if _, dup := obj.values[key]; !dup {
			obj.keys = append(obj.keys, key)
		}
		obj.values[key] = value
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return obj, nil
}

func (o *orderedObject) set(key string, value json.RawMessage) {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *orderedObject) remove(key string) {
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i:i], o.keys[i+1:]...)
			return
		}
	}
}

func (o *orderedObject) marshal() []byte {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		name, _ := json.Marshal(key)
		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(o.values[key])
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

// isRhizomeEntry reports whether an entry (a matcher group) runs a Rhizome
// hook command.
func isRhizomeEntry(raw json.RawMessage) bool {
	var group struct {
		Hooks []struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"hooks"`
	}
	if json.Unmarshal(raw, &group) != nil {
		return false
	}
	for _, h := range group.Hooks {
		if hook.IsEntryCommand(h.Command, h.Args) {
			return true
		}
	}
	return false
}

// hookEntriesOf parses a settings file and returns its hooks object and the
// Rhizome entries in it, by host event.
func hookEntriesOf(data []byte) (*orderedObject, *orderedObject, map[string][]json.RawMessage, error) {
	top, err := parseOrderedObject(data)
	if err != nil {
		return nil, nil, nil, err
	}
	hooks := &orderedObject{values: map[string]json.RawMessage{}}
	if raw, ok := top.values["hooks"]; ok {
		if hooks, err = parseOrderedObject(raw); err != nil {
			return nil, nil, nil, fmt.Errorf("hooks: %w", err)
		}
	}
	ours := map[string][]json.RawMessage{}
	for _, event := range hooks.keys {
		var entries []json.RawMessage
		if json.Unmarshal(hooks.values[event], &entries) != nil {
			continue
		}
		for _, entry := range entries {
			if isRhizomeEntry(entry) {
				var compact bytes.Buffer
				_ = json.Compact(&compact, entry)
				ours[event] = append(ours[event], compact.Bytes())
			}
		}
	}
	return top, hooks, ours, nil
}

// hookEntriesBody is the canonical text of a set of Rhizome entries, which
// the record fingerprints.
func hookEntriesBody(entries map[string][]json.RawMessage) []byte {
	if len(entries) == 0 {
		return nil
	}
	data, _ := json.Marshal(entries)
	return data
}

func desiredBody(desired map[string]json.RawMessage) []byte {
	if desired == nil {
		return nil
	}
	entries := map[string][]json.RawMessage{}
	for event, entry := range desired {
		entries[event] = []json.RawMessage{entry}
	}
	return hookEntriesBody(entries)
}

// renderHookFile replaces the Rhizome entries in a settings file with the
// desired ones, keeping every other key and entry in place. It returns nil
// when nothing but Rhizome's entries was in the file and they are removed.
func renderHookFile(existing []byte, desired map[string]json.RawMessage) ([]byte, error) {
	top, hooks, ours, err := hookEntriesOf(existing)
	if err != nil {
		return nil, err
	}
	events := append([]string(nil), hooks.keys...)
	for event := range desired {
		if _, ok := hooks.values[event]; !ok {
			events = append(events, event)
		}
	}
	sort.SliceStable(events[len(hooks.keys):], func(i, j int) bool {
		return hookEventOrder(events[len(hooks.keys)+i]) < hookEventOrder(events[len(hooks.keys)+j])
	})
	for _, event := range events {
		var entries []json.RawMessage
		if raw, ok := hooks.values[event]; ok && json.Unmarshal(raw, &entries) != nil {
			continue
		}
		kept := entries[:0:0]
		for _, entry := range entries {
			if !isRhizomeEntry(entry) {
				kept = append(kept, entry)
			}
		}
		if entry, ok := desired[event]; ok {
			kept = append(kept, entry)
		}
		if len(kept) == 0 && len(ours[event]) > 0 {
			hooks.remove(event)
			continue
		}
		if len(kept) == 0 {
			continue
		}
		data, _ := json.Marshal(kept)
		hooks.set(event, data)
	}
	if len(hooks.keys) > 0 {
		top.set("hooks", hooks.marshal())
	} else {
		top.remove("hooks")
	}
	if len(top.keys) == 0 {
		return nil, nil
	}
	var out bytes.Buffer
	if err := json.Indent(&out, top.marshal(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

func hookEventOrder(event string) int {
	for i, point := range hook.HookPoints {
		if hook.HostEvent(point) == event {
			return i
		}
	}
	return len(hook.HookPoints)
}

// classifyHookFile reports the change a hook file needs, or nil when there
// is nothing to do. Entries without a record count as Rhizome's, as fenced
// managed blocks do, because the command marks them.
func classifyHookFile(projectRoot string, file generatedHookFile, record *generatedFiles) (*change, error) {
	current, exists, err := readIfExists(filepath.Join(projectRoot, filepath.FromSlash(file.rel)))
	if err != nil {
		return nil, err
	}
	key := file.key()
	// Without entries to write, a file that never mentions rzm is left
	// unparsed, so a broken unrelated settings file does not stop init.
	if file.desired == nil && (!exists || !bytes.Contains(current, []byte("rzm"))) {
		record.forget(key)
		return nil, nil
	}
	var body []byte
	if exists {
		_, _, ours, err := hookEntriesOf(current)
		if err != nil {
			return nil, fmt.Errorf("read hook entries in %s: %w; fix the file or run rzm init --hooks off", file.rel, err)
		}
		body = hookEntriesBody(ours)
	}
	c := &change{key: key, rel: file.rel, group: key, current: body}
	written, recorded := record.Written[key]
	if file.desired == nil {
		if body == nil {
			record.forget(key)
			return nil, nil
		}
		c.removal = true
		switch {
		case recorded && written != contentFingerprint(body) && record.Declined[key] == declinedRemoval:
			c.state = stateDeclined
		case recorded && written != contentFingerprint(body):
			c.state = stateEdited
		default:
			c.state = stateUnedited
		}
		return c, nil
	}
	c.desired = desiredBody(file.desired)
	c.desiredF = contentFingerprint(c.desired)
	c.state = classify(body != nil, body, c.desiredF, contentFingerprint, record, key)
	if c.state == stateUnknown {
		c.state = stateUnedited
	}
	return c, nil
}

// applyHookFile writes a decided hook file change.
func applyHookFile(projectRoot string, file generatedHookFile, c *change, record *generatedFiles, report *syncReport) error {
	if c == nil {
		return nil
	}
	switch {
	case c.state == stateCurrent:
		record.recordWritten(c.key, c.desiredF)
		return nil
	case !c.take:
		if c.asked {
			declined := c.desiredF
			if c.removal {
				declined = declinedRemoval
			}
			record.recordDeclined(c.key, declined)
			report.Declined = append(report.Declined, c.key)
		} else if c.state == stateEdited || c.state == stateUnknown {
			report.Kept = append(report.Kept, c.key)
		}
		return nil
	}
	path := filepath.Join(projectRoot, filepath.FromSlash(file.rel))
	current, exists, err := readIfExists(path)
	if err != nil {
		return err
	}
	desired := file.desired
	if c.removal {
		desired = nil
	}
	next, err := renderHookFile(current, desired)
	if err != nil {
		return fmt.Errorf("update hook entries in %s: %w", file.rel, err)
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
		report.Removed = append(report.Removed, file.rel)
		return nil
	}
	if err := ensureDirExists(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.WriteFile(path, next, 0o644); err != nil {
		return err
	}
	if exists {
		report.Updated = append(report.Updated, file.rel)
	} else {
		report.Created = append(report.Created, file.rel)
	}
	return nil
}
