package viewconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// WHY: an experience is a folder of views Rhizome runs as one site
// (SPEC-0136 R1). Its identity is the views root plus the folder, so it works
// the same from the flat folder, a team's module, a bundled starter's module,
// or an ejected copy; experience.yml declares it, not a module manifest.

// ExperienceFile names the file that declares an experience.
const ExperienceFile = "experience.yml"

// Experience is a loaded experience.yml. Every view definition in its folder
// or below points to it through ViewDefinition.Experience.
type Experience struct {
	Title string   `yaml:"title"`
	Group string   `yaml:"group,omitempty"`
	Types []string `yaml:"types,omitempty"`
	Panel string   `yaml:"panel,omitempty"`
	// Tailwind says whether the experience's frames load the runtime Tailwind
	// compiler (R5); unset means true.
	Tailwind *bool `yaml:"tailwind,omitempty"`

	// Path is the experience.yml file: absolute on disk, or a slash path
	// inside the filesystem LoadFS read.
	Path string `yaml:"-"`
	// Folder is the experience's folder as a slash path relative to its views
	// root, "." for the root itself.
	Folder string `yaml:"-"`
	// Module is the module whose views folder holds the experience ("" for the
	// flat .rhizome/views); Bundled marks the build's bundled views.
	Module  string `yaml:"-"`
	Bundled bool   `yaml:"-"`

	unknown   []string
	decodeErr string
	// nested lists experience.yml files below this experience's folder; they
	// declare nothing and validation reports each one.
	nested []string
}

// Key is the opaque identity the catalog gives the browser. It depends only
// on the views root and the folder, never on the folder's contents.
func (e *Experience) Key() string {
	root := "views"
	switch {
	case e.Bundled:
		root = "bundled"
	case e.Module != "":
		root = "module/" + e.Module
	}
	sum := sha256.Sum256([]byte(root + "\x00" + e.Folder))
	return "exp_" + hex.EncodeToString(sum[:8])
}

// UsesTailwind reports whether the experience's frames load the runtime
// Tailwind compiler.
func (e *Experience) UsesTailwind() bool {
	return e.Tailwind == nil || *e.Tailwind
}

// experienceLess is views-root order (the flat folder, then modules by name,
// then bundled views), then folder path.
func experienceLess(a, b *Experience) bool {
	rank := func(e *Experience) int {
		switch {
		case e.Bundled:
			return 2
		case e.Module != "":
			return 1
		}
		return 0
	}
	if rank(a) != rank(b) {
		return rank(a) < rank(b)
	}
	if a.Module != b.Module {
		return a.Module < b.Module
	}
	return a.Folder < b.Folder
}

var experienceFields = map[string]bool{"title": true, "group": true, "types": true, "panel": true, "tailwind": true}

func decodeExperience(data []byte, filePath string) *Experience {
	exp := &Experience{Path: filePath}
	var fields map[string]any
	if err := yaml.Unmarshal(data, &fields); err != nil {
		exp.decodeErr = err.Error()
		return exp
	}
	for key := range fields {
		if !experienceFields[key] {
			exp.unknown = append(exp.unknown, key)
		}
	}
	sort.Strings(exp.unknown)
	if err := yaml.Unmarshal(data, exp); err != nil {
		exp.decodeErr = err.Error()
	}
	exp.Title = strings.TrimSpace(exp.Title)
	exp.Group = strings.TrimSpace(exp.Group)
	exp.Panel = strings.TrimSpace(exp.Panel)
	for i := range exp.Types {
		exp.Types[i] = strings.TrimSpace(exp.Types[i])
	}
	return exp
}

// attachExperiences points each definition at the experience whose folder
// holds it. files maps each experience.yml's folder (a slash path relative to
// the views root) to its decoded file; folderOf gives a definition's folder in
// the same form. An experience.yml inside another experience's folder
// declares nothing: its views stay in the outer experience.
func attachExperiences(views []ViewDefinition, files map[string]*Experience, folderOf func(ViewDefinition) string) {
	if len(files) == 0 {
		return
	}
	folders := make([]string, 0, len(files))
	for folder := range files {
		folders = append(folders, folder)
	}
	sort.Slice(folders, func(i, j int) bool {
		if di, dj := folderDepth(folders[i]), folderDepth(folders[j]); di != dj {
			return di < dj
		}
		return folders[i] < folders[j]
	})
	var outer []*Experience
	for _, folder := range folders {
		exp := files[folder]
		exp.Folder = folder
		if parent := containingExperience(outer, folder); parent != nil {
			parent.nested = append(parent.nested, exp.Path)
			continue
		}
		outer = append(outer, exp)
	}
	for i := range views {
		views[i].Experience = containingExperience(outer, folderOf(views[i]))
	}
}

func containingExperience(experiences []*Experience, folder string) *Experience {
	for _, exp := range experiences {
		if exp.Folder == "." || folder == exp.Folder || strings.HasPrefix(folder, exp.Folder+"/") {
			return exp
		}
	}
	return nil
}

func folderDepth(folder string) int {
	if folder == "." {
		return 0
	}
	return strings.Count(folder, "/") + 1
}

func isExperienceFile(name string) bool {
	return path.Base(strings.ReplaceAll(name, "\\", "/")) == ExperienceFile
}

func experienceIssue(exp *Experience, code, field, message string, severity IssueSeverity) Issue {
	return Issue{Code: code, Severity: severity, Message: message, Path: exp.Path, Line: 1, Field: field}
}

// validateExperiences checks each experience its views point to, reports
// views that keep their own frame, and drops invalid experiences so their
// views work as ordinary views. issues are those found so far; a view with a
// fatal one does not count as one of its experience's views.
func validateExperiences(views []ViewDefinition, issues []Issue, opts ValidateOptions) []Issue {
	invalidView := map[string]bool{}
	for _, issue := range issues {
		if issue.View != "" && (issue.Severity == "" || issue.Severity == IssueFatal) {
			invalidView[issue.View] = true
		}
	}
	var out []Issue
	var order []*Experience
	members := map[*Experience][]*ViewDefinition{}
	for i := range views {
		view := &views[i]
		exp := view.Experience
		if exp == nil {
			if view.Mount.Kind == MountKindPanel {
				out = append(out, issue(*view, "invalid_panel_mount", "mount.kind", "a panel mount is valid only for a view inside an experience's folder"))
			}
			continue
		}
		if _, seen := members[exp]; !seen {
			order = append(order, exp)
		}
		members[exp] = append(members[exp], view)
		if view.SourceSpec.Kind != SourceKindCustom {
			continue
		}
		page, problem := experiencePage(*view, opts)
		view.ExperiencePage = page
		if problem != "" {
			out = append(out, issueWithSeverity(*view, "experience_view_own_frame", "source.entry", problem, IssueWarning))
		}
	}
	invalid := map[*Experience]bool{}
	for _, exp := range order {
		found := experienceFileIssues(exp, members[exp], invalidView, opts)
		for _, nested := range exp.nested {
			found = append(found, Issue{Code: "nested_experience", Message: fmt.Sprintf("an experience.yml inside the experience at %s declares nothing; its views stay in the outer experience", exp.Path), Path: nested, Line: 1})
		}
		for _, issue := range found {
			if (issue.Severity == "" || issue.Severity == IssueFatal) && issue.Path == exp.Path {
				invalid[exp] = true
			}
		}
		out = append(out, found...)
	}
	for i := range views {
		view := &views[i]
		exp := view.Experience
		if exp == nil {
			continue
		}
		if view.Mount.Kind == MountKindPanel {
			switch {
			case invalid[exp]:
				out = append(out, issue(*view, "invalid_panel_mount", "mount.kind", fmt.Sprintf("the experience at %s is invalid, so its panel view cannot be used", exp.Path)))
			case exp.Panel != view.ID:
				out = append(out, issue(*view, "invalid_panel_mount", "mount.kind", fmt.Sprintf("a panel mount must be named by its experience's panel field (%s)", exp.Path)))
			}
		}
		if invalid[exp] {
			view.Experience = nil
			view.ExperiencePage = false
		}
	}
	if opts.Schema != nil {
		var valid []ViewDefinition
		for _, view := range views {
			if view.Experience != nil && !invalidView[view.ID] {
				valid = append(valid, view)
			}
		}
		out = append(out, NewPanelResolver(opts.Schema, valid).tieIssues()...)
	}
	return out
}

func experienceFileIssues(exp *Experience, views []*ViewDefinition, invalidView map[string]bool, opts ValidateOptions) []Issue {
	if exp.decodeErr != "" {
		return []Issue{experienceIssue(exp, "experience_yaml_decode_error", "", exp.decodeErr, IssueFatal)}
	}
	var out []Issue
	for _, field := range exp.unknown {
		out = append(out, experienceIssue(exp, "unknown_experience_field", field, fmt.Sprintf("experience.yml field %q is not supported; it is ignored", field), IssueWarning))
	}
	if exp.Title == "" {
		out = append(out, experienceIssue(exp, "missing_required_field", "title", "title is required", IssueFatal))
	}
	if opts.CheckReferences {
		if exp.Group != "" && opts.GroupNames != nil && !contains(opts.GroupNames, exp.Group) {
			out = append(out, experienceIssue(exp, "invalid_experience_reference", "group", fmt.Sprintf("display group %q does not exist in navigation", exp.Group), IssueFatal))
		}
		for _, name := range exp.Types {
			if !contains(opts.TypeNames, name) && !contains(opts.InterfaceNames, name) {
				out = append(out, experienceIssue(exp, "invalid_experience_reference", "types", fmt.Sprintf("ontology type or interface %q does not exist", name), IssueFatal))
			}
		}
	}
	if exp.Panel != "" {
		var panel *ViewDefinition
		for _, view := range views {
			if view.ID == exp.Panel {
				panel = view
			}
		}
		switch {
		case panel == nil || panel.Mount.Kind != MountKindPanel:
			out = append(out, experienceIssue(exp, "invalid_experience_panel", "panel", fmt.Sprintf("panel %q names no view with mount kind panel in the experience's folder", exp.Panel), IssueFatal))
		case panel.SourceSpec.Kind != SourceKindCustom || !IsScriptEntry(panel.SourceSpec.Entry) || !panel.ExperiencePage:
			out = append(out, experienceIssue(exp, "invalid_experience_panel", "panel", fmt.Sprintf("panel %q must be a TSX view that exports its component as the module default", exp.Panel), IssueFatal))
		case invalidView[panel.ID]:
			out = append(out, experienceIssue(exp, "invalid_experience_panel", "panel", fmt.Sprintf("panel view %q is invalid", exp.Panel), IssueFatal))
		}
	}
	return out
}

// experiencePage reports whether a custom view in an experience renders as a
// page of the experience frame (R1): a script entry that exports its component
// as the module default. Otherwise it keeps its own frame, and problem says
// why. Without a scanner the export is not checked.
func experiencePage(view ViewDefinition, opts ValidateOptions) (bool, string) {
	entry := view.SourceSpec.Entry
	if !IsScriptEntry(entry) {
		return false, "an HTML entry renders in its own frame, not in its experience's frame; use a TSX entry that exports its component as the module default"
	}
	if opts.ScanDefaultExport == nil || view.Source.Path == "" {
		return true, ""
	}
	source, err := readEntry(opts.EntryFS, view)
	if err != nil {
		return false, ""
	}
	ok, err := opts.ScanDefaultExport(entry, source)
	if err != nil {
		return false, ""
	}
	if !ok {
		return false, "the entry does not export its component as the module default, so it renders in its own frame, not in its experience's frame"
	}
	return true, ""
}
