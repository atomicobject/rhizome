// Package taskdef parses scheduled task files, `.rhizome/tasks/<id>.yml`
// ([[scheduled-tasks|SPEC-0127]]). It has no I/O so the task runner and
// validation share one parser.
package taskdef

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// TasksDir holds task files, relative to the repository root.
	TasksDir = ".rhizome/tasks"
	// TagKey starts the line that tags a task's commits.
	TagKey = "Rhizome-Task"

	ScopePerson = "person"
	ScopeTeam   = "team"

	defaultTimeout = time.Hour
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Check is a task's optional pre-flight check: an argument list, or a
// code-mode script run by `rzm agent code execute`.
type Check struct {
	Command []string `yaml:"command,omitempty"`
	Script  string   `yaml:"script,omitempty"`
}

// Definition is one task file, `.rhizome/tasks/<id>.yml`.
type Definition struct {
	ID          string `yaml:"-"`
	Path        string `yaml:"-"`
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Every       string `yaml:"every"`
	Scope       string `yaml:"scope,omitempty"`
	Check       *Check `yaml:"check,omitempty"`
	Prompt      string `yaml:"prompt,omitempty"`
	Timeout     string `yaml:"timeout,omitempty"`

	Cadence time.Duration `yaml:"-"`
	Limit   time.Duration `yaml:"-"`
}

// Tag is the line that marks a commit as this task's work.
func (d Definition) Tag() string { return TagKey + ": " + d.ID }

// Problem is a task file that could not be used.
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ParseDefinition parses and validates one task file. The id comes from the
// file name.
func ParseDefinition(filePath string, data []byte) (Definition, error) {
	base := path.Base(filePath)
	id := strings.TrimSuffix(strings.TrimSuffix(base, ".yml"), ".yaml")
	if !idPattern.MatchString(id) {
		return Definition{}, fmt.Errorf("task id %q must be lowercase letters, digits, and hyphens", id)
	}
	var def Definition
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&def); err != nil {
		return Definition{}, fmt.Errorf("parse: %w", err)
	}
	def.ID, def.Path = id, filePath
	if strings.TrimSpace(def.Name) == "" {
		return Definition{}, errors.New("name is required")
	}
	cadence, err := ParseDuration(def.Every)
	if err != nil {
		return Definition{}, fmt.Errorf("every: %w", err)
	}
	def.Cadence = cadence
	switch def.Scope {
	case "":
		def.Scope = ScopePerson
	case ScopePerson, ScopeTeam:
	default:
		return Definition{}, fmt.Errorf("scope must be %q or %q", ScopePerson, ScopeTeam)
	}
	if def.Check != nil && (len(def.Check.Command) > 0) == (def.Check.Script != "") {
		return Definition{}, errors.New("check needs exactly one of command or script")
	}
	if def.Check == nil && strings.TrimSpace(def.Prompt) == "" {
		return Definition{}, errors.New("a task needs a check, a prompt, or both")
	}
	def.Limit = defaultTimeout
	if def.Timeout != "" {
		if def.Limit, err = ParseDuration(def.Timeout); err != nil {
			return Definition{}, fmt.Errorf("timeout: %w", err)
		}
	}
	return def, nil
}

// ParseDuration accepts Go durations plus whole days (`7d`) and weeks (`2w`).
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("is required")
	}
	unit := map[byte]time.Duration{'d': 24 * time.Hour, 'w': 7 * 24 * time.Hour}[s[len(s)-1]]
	var d time.Duration
	if unit != 0 {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		d = time.Duration(n) * unit
	} else {
		var err error
		if d, err = time.ParseDuration(s); err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration %q must be positive", s)
	}
	return d, nil
}

// Load parses task files in order. A file that cannot be read or parsed, or
// whose id an earlier file already declared, is reported as a problem and
// does not affect the others.
func Load(files []string, read func(file string) ([]byte, error)) ([]Definition, []Problem) {
	var defs []Definition
	var problems []Problem
	seen := map[string]string{}
	for _, file := range files {
		data, err := read(file)
		if err != nil {
			problems = append(problems, Problem{Path: file, Message: err.Error()})
			continue
		}
		def, err := ParseDefinition(file, data)
		if err != nil {
			problems = append(problems, Problem{Path: file, Message: err.Error()})
			continue
		}
		if other, dup := seen[def.ID]; dup {
			problems = append(problems, Problem{Path: file, Message: fmt.Sprintf("task id %q is also declared by %s", def.ID, other)})
			continue
		}
		seen[def.ID] = file
		defs = append(defs, def)
	}
	return defs, problems
}
