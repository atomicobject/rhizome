// Package actiondef parses module action declarations,
// `.rhizome/modules/<module>/actions/<name>.yaml`
// ([[module-actions|SPEC-0131]]), and checks an action's input against its
// declaration. It runs nothing, so the actions runner and validation share
// one parser.
package actiondef

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
)

const (
	// ModulePlaceholder in a command stands for the module folder relative to
	// the repository root, as in hook commands.
	ModulePlaceholder = "{module}"

	DefaultTimeout = 60 * time.Second
	MaxTimeout     = 5 * time.Minute
)

// Input kinds.
const (
	KindString = "string"
	KindInt    = "int"
	KindBool   = "bool"
	KindList   = "list"
)

var inputNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// ErrInvalidInput marks input that does not match an action's declaration.
var ErrInvalidInput = errors.New("invalid action input")

// Input declares one named input.
type Input struct {
	Kind     string   `yaml:"kind" json:"kind"`
	Required bool     `yaml:"required,omitempty" json:"required,omitempty"`
	Enum     []string `yaml:"enum,omitempty" json:"enum,omitempty"`
}

// Run holds the one way an action runs.
type Run struct {
	Command []string `yaml:"command,omitempty"`
	Script  string   `yaml:"script,omitempty"`
}

// Action is one declaration. The JSON form is what callers list.
type Action struct {
	ID          string           `yaml:"-" json:"id"`
	Module      string           `yaml:"-" json:"module"`
	Name        string           `yaml:"-" json:"name"`
	Description string           `yaml:"description" json:"description"`
	Inputs      map[string]Input `yaml:"inputs,omitempty" json:"inputs"`
	Run         Run              `yaml:"run" json:"-"`
	Timeout     time.Duration    `yaml:"-" json:"-"`
	// Path is the declaration's repository-relative path.
	Path string `yaml:"-" json:"-"`
}

// ModuleDir is the action's module folder, relative to the repository root.
func (a Action) ModuleDir() string { return assetroots.ModuleRoot(a.Module) }

// Command returns the command with {module} expanded.
func (a Action) Command() []string {
	out := make([]string, len(a.Run.Command))
	for i, arg := range a.Run.Command {
		out[i] = strings.ReplaceAll(arg, ModulePlaceholder, a.ModuleDir())
	}
	return out
}

// Problem is one declaration the runner cannot use.
type Problem struct {
	Path    string
	Message string
}

// IsActionFile reports whether a repository-relative path is a declaration:
// a `.yaml` or `.yml` file directly inside a module's actions folder.
func IsActionFile(rel string) bool {
	root, kind, sub, ok := assetroots.Classify(rel)
	return ok && kind == assetroots.Actions && root.Module != "" && !strings.Contains(sub, "/") &&
		(strings.HasSuffix(sub, ".yaml") || strings.HasSuffix(sub, ".yml"))
}

// Parse reads one declaration at a repository-relative path. It returns
// every problem it finds; an action with problems must not run.
func Parse(rel string, data []byte) (Action, []string) {
	root, _, sub, _ := assetroots.Classify(rel)
	name := strings.TrimSuffix(strings.TrimSuffix(sub, ".yml"), ".yaml")
	a := Action{Module: root.Module, Name: name, ID: assetroots.Qualify(root.Module, name), Path: rel}
	var raw struct {
		Description string           `yaml:"description"`
		Inputs      map[string]Input `yaml:"inputs"`
		Run         Run              `yaml:"run"`
		Timeout     string           `yaml:"timeout"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return a, []string{"cannot parse: " + err.Error()}
	}
	var problems []string
	if !assetroots.ValidModuleName(name) {
		problems = append(problems, fmt.Sprintf("action name %q must match ^[a-z0-9][a-z0-9-]{0,62}$", name))
	}
	a.Description = strings.TrimSpace(raw.Description)
	if a.Description == "" {
		problems = append(problems, "description is required")
	}
	a.Inputs = raw.Inputs
	if a.Inputs == nil {
		a.Inputs = map[string]Input{}
	}
	for _, input := range sortedKeys(a.Inputs) {
		spec := a.Inputs[input]
		if !inputNamePattern.MatchString(input) {
			problems = append(problems, fmt.Sprintf("input %q must be lowercase words joined by hyphens", input))
		}
		switch spec.Kind {
		case KindString, KindList:
		case KindInt, KindBool:
			if spec.Enum != nil {
				problems = append(problems, fmt.Sprintf("input %q: an enum needs kind string or list, not %s", input, spec.Kind))
			}
		default:
			problems = append(problems, fmt.Sprintf("input %q: unknown kind %q (use string, int, bool, or list)", input, spec.Kind))
		}
	}
	a.Run = raw.Run
	switch {
	case len(a.Run.Command) > 0 && a.Run.Script != "":
		problems = append(problems, "run must hold one of command or script, not both")
	case len(a.Run.Command) == 0 && a.Run.Script == "":
		problems = append(problems, "run must hold command or script")
	case a.Run.Script != "":
		if clean := path.Clean(a.Run.Script); path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(a.Run.Script, `\`) {
			problems = append(problems, fmt.Sprintf("run.script %q must be a path inside the module folder", a.Run.Script))
		}
	case a.Run.Command[0] == "":
		problems = append(problems, "run.command must start with a program")
	}
	a.Timeout = DefaultTimeout
	if raw.Timeout != "" {
		d, err := time.ParseDuration(raw.Timeout)
		switch {
		case err != nil || d <= 0:
			problems = append(problems, fmt.Sprintf("timeout %q must be a positive duration like 30s", raw.Timeout))
		case d > MaxTimeout:
			problems = append(problems, fmt.Sprintf("timeout %s is over the %s limit", d, MaxTimeout))
		default:
			a.Timeout = d
		}
	}
	return a, problems
}

// Load reads every declaration in the repository's module actions folders.
// It returns the usable actions sorted by id and a problem for each file it
// could not use; err is a root that could not be listed.
func Load(repoRoot string) ([]Action, []Problem, error) {
	roots, err := assetroots.Roots(repoRoot, assetroots.Actions)
	var actions []Action
	var problems []Problem
	for _, root := range roots {
		entries, readErr := os.ReadDir(root.Abs(repoRoot))
		if readErr != nil {
			err = errors.Join(err, readErr)
			continue
		}
		for _, entry := range entries {
			rel := path.Join(root.Rel, entry.Name())
			if entry.IsDir() || !IsActionFile(rel) {
				continue
			}
			data, readErr := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
			if readErr != nil {
				problems = append(problems, Problem{Path: rel, Message: "cannot read: " + readErr.Error()})
				continue
			}
			a, found := Parse(rel, data)
			for _, message := range found {
				problems = append(problems, Problem{Path: rel, Message: message})
			}
			if len(found) == 0 {
				actions = append(actions, a)
			}
		}
	}
	// `a.yaml` and `a.yml` would both claim one id.
	sort.SliceStable(actions, func(i, j int) bool { return actions[i].ID < actions[j].ID })
	unique := actions[:0]
	for _, a := range actions {
		if n := len(unique); n > 0 && unique[n-1].ID == a.ID {
			problems = append(problems, Problem{Path: a.Path, Message: fmt.Sprintf("action %s is also declared in %s", a.ID, unique[n-1].Path)})
			continue
		}
		unique = append(unique, a)
	}
	return unique, problems, err
}

// MissingPaths lists the files an action names inside its module folder
// that do not exist: its script, and each command argument holding
// {module}. Paths are repository-relative.
func MissingPaths(repoRoot string, a Action) []string {
	var candidates []string
	if a.Run.Script != "" {
		candidates = append(candidates, path.Join(a.ModuleDir(), a.Run.Script))
	}
	for i, arg := range a.Run.Command {
		if strings.Contains(arg, ModulePlaceholder) {
			candidates = append(candidates, a.Command()[i])
		}
	}
	var missing []string
	for _, rel := range candidates {
		if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(rel))); errors.Is(err, fs.ErrNotExist) {
			missing = append(missing, rel)
		}
	}
	return missing
}

// CheckInput rejects names the action does not declare, values of the wrong
// kind, missing required inputs, and values outside an enum. Values are
// JSON-decoded: strings, float64 or int numbers, bools, and lists of strings.
func (a Action) CheckInput(input map[string]any) error {
	for _, name := range sortedKeys(input) {
		spec, ok := a.Inputs[name]
		if !ok {
			return fmt.Errorf("%w: %s declares no input %q", ErrInvalidInput, a.ID, name)
		}
		if err := spec.check(input[name]); err != nil {
			return fmt.Errorf("%w: input %q %v", ErrInvalidInput, name, err)
		}
	}
	for _, name := range sortedKeys(a.Inputs) {
		if _, ok := input[name]; a.Inputs[name].Required && !ok {
			return fmt.Errorf("%w: input %q is required", ErrInvalidInput, name)
		}
	}
	return nil
}

func (spec Input) check(value any) error {
	inEnum := func(s string) error {
		if spec.Enum != nil && !slices.Contains(spec.Enum, s) {
			return fmt.Errorf("must be one of %s, not %q", strings.Join(spec.Enum, ", "), s)
		}
		return nil
	}
	switch spec.Kind {
	case KindString:
		s, ok := value.(string)
		if !ok {
			return errors.New("must be a string")
		}
		return inEnum(s)
	case KindInt:
		switch v := value.(type) {
		case int, int64:
			return nil
		case float64:
			if v == math.Trunc(v) && !math.IsInf(v, 0) {
				return nil
			}
		}
		return errors.New("must be an integer")
	case KindBool:
		if _, ok := value.(bool); !ok {
			return errors.New("must be true or false")
		}
		return nil
	case KindList:
		var items []string
		switch v := value.(type) {
		case []string:
			items = v
		case []any:
			for _, item := range v {
				s, ok := item.(string)
				if !ok {
					return errors.New("must be a list of strings")
				}
				items = append(items, s)
			}
		default:
			return errors.New("must be a list of strings")
		}
		for _, item := range items {
			if err := inEnum(item); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("has unknown kind %q", spec.Kind)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
