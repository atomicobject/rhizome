package skilloverlay

import (
	"fmt"
)

const APIVersion = "rhizome.skill-overlay.v1"

type skillOverlaySlotMode string

const (
	skillOverlaySlotModeExtension   skillOverlaySlotMode = "extension"
	skillOverlaySlotModeReplaceable skillOverlaySlotMode = "replaceable"
)

type Op string

const (
	Prepend  Op = "prepend"
	Append   Op = "append"
	Replace  Op = "replace"
	Suppress Op = "suppress"
)

type Overlay struct {
	APIVersion  string     `yaml:"apiVersion"`
	ID          string     `yaml:"id"`
	TargetSkill string     `yaml:"targetSkill"`
	Fragments   []Fragment `yaml:"fragments"`
}

type Fragment struct {
	File    string `yaml:"file,omitempty"`
	Slot    string `yaml:"slot"`
	Op      Op     `yaml:"op"`
	Order   int    `yaml:"order,omitempty"`
	Content string `yaml:"content,omitempty"`
}

type LoadedOverlay struct {
	Template      string
	TemplateOrder int
	Path          string
	Overlay       Overlay
}

type resolvedSkillOverlayFragment struct {
	Template      string
	TemplateOrder int
	OverlayID     string
	SourcePath    string
	FragmentIndex int
	File          string
	Slot          string
	Op            Op
	Order         int
	Content       string
}

type skillOverlaySlot struct {
	ID        string
	Mode      skillOverlaySlotMode
	Start     int
	End       int
	BodyStart int
	BodyEnd   int
	Body      string
}

type Issue struct {
	Code    string
	Message string
	Skill   string
	Path    string
	Slot    string
	Overlay string
}

type Report struct {
	Templates []string
	Entries   []Entry
	Issues    []Issue
}

type Manifest struct {
	APIVersion string   `json:"apiVersion"`
	Templates  []string `json:"templates"`
	Entries    []Entry  `json:"entries"`
	Issues     []Issue  `json:"issues,omitempty"`
}

type Entry struct {
	TargetSkill    string `json:"targetSkill"`
	Slot           string `json:"slot"`
	Operation      string `json:"operation"`
	OverlayID      string `json:"overlayId"`
	SourceTemplate string `json:"sourceTemplate"`
	SourcePath     string `json:"sourcePath"`
	Order          int    `json:"order"`
	Outcome        string `json:"outcome"`
	ContentHash    string `json:"contentHash,omitempty"`
}

type skillOverlaySlotRender struct {
	Content string
	Entries []Entry
}

type Error struct {
	Issues []Issue
}

func (e Error) Error() string {
	if len(e.Issues) == 0 {
		return "skill overlay error"
	}
	return fmt.Sprintf("skill overlay %s: %s", e.Issues[0].Code, e.Issues[0].Message)
}

func skillOverlayIssuef(code, skill, filePath, slot, overlay, format string, args ...any) Issue {
	return Issue{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Skill:   skill,
		Path:    filePath,
		Slot:    slot,
		Overlay: overlay,
	}
}

func skillOverlayErr(issues ...Issue) error {
	return Error{Issues: issues}
}

func (r Report) Manifest() Manifest {
	return Manifest{
		APIVersion: APIVersion,
		Templates:  append([]string(nil), r.Templates...),
		Entries:    append([]Entry(nil), r.Entries...),
		Issues:     append([]Issue(nil), r.Issues...),
	}
}

func validSkillOverlayOp(op Op) bool {
	switch op {
	case Prepend, Append, Replace, Suppress:
		return true
	default:
		return false
	}
}

type File struct {
	Path       string
	Content    []byte
	IsMarkdown bool
}

type Skill struct {
	Name  string
	Files []File
}
