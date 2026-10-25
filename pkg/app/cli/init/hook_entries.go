// Hook entries in .claude/settings.json and .codex/hooks.json: one
// marker-identified command per hook point, in a matcher group of its own
// that matches every tool, that runs `rzm hook <harness> <event>`. Rhizome
// owns those commands and nothing else: other commands, even in the same
// group, and every other key belong to someone else and are preserved. A
// key added to a group holding Rhizome's commands, such as a matcher, changes
// what they run on, so it counts as an edit to them.
//
// Docs:
// - [[agent-surface-integration-modes#^SPEC-0045-US5-AC3]]
// - [[agent-surface-integration-modes#^SPEC-0045-US5-AC8]]
package init

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/atomicobject/rhizome/pkg/app/cli/hook"
)

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
	desired map[string]json.RawMessage // host event -> command
}

func (f generatedHookFile) key() string { return f.rel + hookEntriesKey }

// hookEntry is the command for one harness and hook point. Claude Code runs
// the command-and-arguments form without a shell and finds rzm on PATH (rzm.exe
// on Windows), so its command is the same on every OS. Codex runs command
// through a shell and carries its Windows form in commandWindows, which it
// runs through cmd.exe /C.
func hookEntry(harness, point string) json.RawMessage {
	command := map[string]any{"type": "command", "timeout": hook.HostTimeoutSeconds}
	if harness == hook.HarnessClaude {
		command["command"] = "rzm"
		command["args"] = []string{"hook", harness, point}
	} else {
		command["command"] = hook.EntryCommand(harness, point)
		command["commandWindows"] = "rzm.exe hook " + harness + " " + point
	}
	data, _ := json.Marshal(command)
	return data
}

// desiredHookEntries are a harness's entries, with the before-tool-call one
// only when guarded.
func desiredHookEntries(harness string, guarded bool) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, point := range hook.HookPoints {
		if point == hook.PreToolUse && !guarded {
			continue
		}
		out[hook.HostEvent(point)] = hookEntry(harness, point)
	}
	return out
}

// guarded reports whether a guard declaration has an approved rule, which
// is what installs the before-tool-call entry.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US6-AC2]]
func guarded(root string, handlers []hook.Handler) bool {
	for _, h := range handlers {
		if h.GuardRules == "" {
			continue
		}
		if rules, _ := hook.LoadGuardRules(context.Background(), root, h.GuardRules); len(rules) > 0 {
			return true
		}
	}
	return false
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

// isRhizomeCommand reports whether one hook command is a Rhizome entry's.
func isRhizomeCommand(raw json.RawMessage) bool {
	var command struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	return json.Unmarshal(raw, &command) == nil && hook.IsEntryCommand(command.Command, command.Args)
}

// canonicalJSON re-encodes a value with sorted keys and no spacing, so key
// order and formatting do not count as edits.
func canonicalJSON(raw json.RawMessage) json.RawMessage {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	data, _ := json.Marshal(v)
	return data
}

// hookEntriesOf parses a settings file and returns its hooks object and,
// by host event, each group holding Rhizome commands with only those
// commands: a group's other keys, such as matcher, change what the commands
// run on, so they count as part of the entries.
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
		var groups []json.RawMessage
		if json.Unmarshal(hooks.values[event], &groups) != nil {
			continue
		}
		for _, group := range groups {
			var commands []json.RawMessage
			for _, command := range groupCommands(group) {
				if isRhizomeCommand(command) {
					commands = append(commands, command)
				}
			}
			if len(commands) == 0 {
				continue
			}
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(group, &fields)
			fields["hooks"], _ = json.Marshal(commands)
			data, _ := json.Marshal(fields)
			ours[event] = append(ours[event], canonicalJSON(data))
		}
	}
	return top, hooks, ours, nil
}

// groupCommands returns the commands of one matcher group.
func groupCommands(group json.RawMessage) []json.RawMessage {
	var g struct {
		Hooks []json.RawMessage `json:"hooks"`
	}
	_ = json.Unmarshal(group, &g)
	return g.Hooks
}

// hookEntriesBody is the canonical text of the groups holding Rhizome
// commands, which the record fingerprints.
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
	for event, command := range desired {
		entries[event] = []json.RawMessage{canonicalJSON(desiredGroup(command))}
	}
	return hookEntriesBody(entries)
}

// desiredGroup is the group Rhizome writes a command in: no matcher, so it
// runs on every tool.
func desiredGroup(command json.RawMessage) json.RawMessage {
	group, _ := json.Marshal(map[string]any{"hooks": []json.RawMessage{command}})
	return group
}

// withoutRhizomeCommands returns a matcher group with Rhizome's commands
// taken out, keeping its other keys and commands in place. It returns nil
// when only Rhizome's commands were in it.
func withoutRhizomeCommands(group json.RawMessage) (json.RawMessage, error) {
	commands := groupCommands(group)
	kept := commands[:0:0]
	for _, command := range commands {
		if !isRhizomeCommand(command) {
			kept = append(kept, command)
		}
	}
	switch {
	case len(kept) == len(commands):
		return group, nil
	case len(kept) == 0:
		return nil, nil
	}
	obj, err := parseOrderedObject(group)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(kept)
	obj.set("hooks", data)
	return obj.marshal(), nil
}

// renderHookFile replaces the Rhizome commands in a settings file with the
// desired ones, each in a group of its own, keeping every other key, group,
// and command in place. It returns nil when nothing but Rhizome's commands
// was in the file and they are removed.
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
		var groups []json.RawMessage
		if raw, ok := hooks.values[event]; ok && json.Unmarshal(raw, &groups) != nil {
			continue
		}
		kept := groups[:0:0]
		for _, group := range groups {
			next, err := withoutRhizomeCommands(group)
			if err != nil {
				return nil, fmt.Errorf("hooks.%s: %w", event, err)
			}
			if next != nil {
				kept = append(kept, next)
			}
		}
		if command, ok := desired[event]; ok {
			kept = append(kept, desiredGroup(command))
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
// is nothing to do. Rhizome commands without a record may be someone's own,
// so changing or removing them needs a decision, as for any generated file
// Rhizome cannot tell it wrote.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US3-AC4]]
func classifyHookFile(projectRoot string, file generatedHookFile, record *generatedFiles) (*change, error) {
	current, exists, err := readIfExists(filepath.Join(projectRoot, filepath.FromSlash(file.rel)))
	if err != nil {
		return nil, err
	}
	key := file.key()
	// Without commands to write, a file that never mentions rzm is left
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
		case record.Declined[key] == declinedRemoval && written != contentFingerprint(body):
			c.state = stateDeclined
		case !recorded:
			c.state = stateUnknown
		case written != contentFingerprint(body):
			c.state = stateEdited
		default:
			c.state = stateUnedited
		}
		return c, nil
	}
	c.desired = desiredBody(file.desired)
	c.desiredF = contentFingerprint(c.desired)
	c.state = classify(body != nil, body, c.desiredF, contentFingerprint, record, key)
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
