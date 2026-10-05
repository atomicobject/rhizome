package init

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed templates templates/**/*
var agentHelperTemplates embed.FS

func loadAgentHelperTemplate(name string) (string, error) {
	p := path.Join("templates", name)
	b, err := fs.ReadFile(agentHelperTemplates, p)
	if err != nil {
		return "", fmt.Errorf("read agent helper template %q: %w", name, err)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", fmt.Errorf("agent helper template %q is empty", name)
	}
	return s, nil
}

type agentTemplate struct {
	Name    string
	Content string
}

type skillTemplateFile struct {
	Path       string
	Content    []byte
	IsMarkdown bool
}

type skillTemplate struct {
	Name  string
	Files []skillTemplateFile
}

type starterTemplateFile struct {
	Path    string
	Content []byte
}

func loadCommandTemplates() ([]agentTemplate, error) {
	const glob = "templates/commands/*"
	matches, err := fs.Glob(agentHelperTemplates, glob)
	if err != nil {
		return nil, fmt.Errorf("list agent templates under \"commands\": %w", err)
	}
	var out []agentTemplate
	for _, m := range matches {
		b, err := fs.ReadFile(agentHelperTemplates, m)
		if err != nil {
			return nil, fmt.Errorf("read agent template %q: %w", m, err)
		}
		name := path.Base(m)
		content := strings.TrimSpace(string(b))
		if content == "" {
			return nil, fmt.Errorf("agent template %q is empty", name)
		}
		out = append(out, agentTemplate{
			Name:    name,
			Content: content,
		})
	}
	return out, nil
}

// loadSkillTemplates loads the core rhizome skills shipped for every repo,
// regardless of template choice (ontology, note-authoring, onboarding,
// skill-creator). Template-specific skills are layered on via
// loadStarterSkillTemplates.
//
// Docs: [[init-starter-workflow#^spec-0038-us2-ac3]]
func loadSkillTemplates() ([]skillTemplate, error) {
	groupRoot := path.Join("templates", "skills", "markdown")
	entries, err := fs.ReadDir(agentHelperTemplates, groupRoot)
	if err != nil {
		return nil, fmt.Errorf("list skill templates under %q: %w", groupRoot, err)
	}
	var out []skillTemplate
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("skill template entry %q is not a directory", entry.Name())
		}
		name := entry.Name()
		skillRoot := path.Join(groupRoot, name)
		tmpl, err := readSkillTemplate(name, skillRoot)
		if err != nil {
			return nil, err
		}
		out = append(out, tmpl)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// loadStarterSkillTemplates loads skill templates bundled under a template's
// skills/ subdirectory. Returns nil without error when the template is empty
// or has no skills directory.
//
// Docs: [[init-template-architecture#^spec-0039-us2-ac2]]
func loadStarterSkillTemplates(template string) ([]skillTemplate, error) {
	if template == "" {
		return nil, nil
	}
	root := path.Join("templates", "starters", template, "agents", "skills")
	entries, err := fs.ReadDir(agentHelperTemplates, root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list starter skill templates under %q: %w", root, err)
	}
	var out []skillTemplate
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("starter skill template entry %q is not a directory", entry.Name())
		}
		name := entry.Name()
		skillRoot := path.Join(root, name)
		tmpl, err := readSkillTemplate(name, skillRoot)
		if err != nil {
			return nil, err
		}
		out = append(out, tmpl)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Docs:
// - [[init-starter-workflow#^spec-0038-us2-ac3]]
// - [[init-template-architecture#^spec-0039-us2-ac1]]
// loadAllSkillTemplates merges core rhizome skills with all selected
// templates' bundled skills. Name collisions across core/template bundles are
// explicit errors so templates cannot silently shadow one another.
func loadAllSkillTemplates(templates []string) ([]skillTemplate, error) {
	skills, _, err := loadAllSkillTemplatesWithReport(templates)
	return skills, err
}

func loadAllSkillTemplatesWithReport(templates []string) ([]skillTemplate, SkillOverlayManifest, error) {
	base, err := loadSkillTemplates()
	if err != nil {
		return nil, SkillOverlayManifest{}, err
	}
	seen := make(map[string]struct{}, len(base))
	for _, t := range base {
		seen[t.Name] = struct{}{}
	}
	var overlays []loadedSkillOverlay
	for order, template := range templates {
		templateSkills, err := loadStarterSkillTemplates(template)
		if err != nil {
			return nil, SkillOverlayManifest{}, err
		}
		for _, t := range templateSkills {
			if _, ok := seen[t.Name]; ok {
				return nil, SkillOverlayManifest{}, fmt.Errorf("template skill %q collides with an existing bundled skill", t.Name)
			}
			seen[t.Name] = struct{}{}
			base = append(base, t)
		}
		templateOverlays, err := loadStarterSkillOverlayTemplates(template, order)
		if err != nil {
			return nil, SkillOverlayManifest{}, err
		}
		overlays = append(overlays, templateOverlays...)
	}
	base, report, err := applySkillOverlays(base, overlays, templates...)
	if err != nil {
		return nil, report.Manifest(), err
	}
	sort.Slice(base, func(i, j int) bool { return base[i].Name < base[j].Name })
	return base, report.Manifest(), nil
}

// readSkillTemplate walks a single skill directory and returns its files.
// Shared by loadSkillTemplates and loadStarterSkillTemplates.
func readSkillTemplate(name, skillRoot string) (skillTemplate, error) {
	var files []skillTemplateFile
	hasSkill := false
	if err := fs.WalkDir(agentHelperTemplates, skillRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		prefix := skillRoot + "/"
		if !strings.HasPrefix(p, prefix) {
			return fmt.Errorf("skill template path %q is not under %q", p, skillRoot)
		}
		rel := strings.TrimPrefix(p, prefix)
		if rel == "" {
			return nil
		}
		content, err := fs.ReadFile(agentHelperTemplates, p)
		if err != nil {
			return err
		}
		isMarkdown := strings.HasSuffix(strings.ToLower(rel), ".md")
		if isMarkdown {
			trimmed := strings.TrimSpace(string(content))
			if trimmed == "" {
				return fmt.Errorf("skill template %q file %q is empty", name, rel)
			}
			files = append(files, skillTemplateFile{
				Path:       rel,
				Content:    []byte(trimmed),
				IsMarkdown: true,
			})
			if strings.EqualFold(rel, "SKILL.md") {
				hasSkill = true
			}
			return nil
		}
		if len(content) == 0 {
			return fmt.Errorf("skill template %q file %q is empty", name, rel)
		}
		files = append(files, skillTemplateFile{
			Path:       rel,
			Content:    content,
			IsMarkdown: false,
		})
		return nil
	}); err != nil {
		return skillTemplate{}, fmt.Errorf("read skill template %q: %w", name, err)
	}
	if !hasSkill {
		return skillTemplate{}, fmt.Errorf("skill template %q missing SKILL.md", name)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return skillTemplate{Name: name, Files: files}, nil
}

// loadStarterTemplates returns the starter's repo/ tree, which installs into
// the target repository path for path.
//
// Docs: [[init-template-architecture#^spec-0039-us2-ac2]]
func loadStarterTemplates(template string) ([]starterTemplateFile, error) {
	root := path.Join("templates", "starters", template, "repo")
	var files []starterTemplateFile
	if err := fs.WalkDir(agentHelperTemplates, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		prefix := root + "/"
		if !strings.HasPrefix(p, prefix) {
			return fmt.Errorf("template file %q is not under %q", p, root)
		}
		rel := strings.TrimPrefix(p, prefix)
		if rel == "" {
			return nil
		}
		content, err := fs.ReadFile(agentHelperTemplates, p)
		if err != nil {
			return err
		}
		if strings.HasSuffix(strings.ToLower(rel), ".md") {
			content = []byte(strings.TrimSpace(string(content)))
		}
		if len(content) == 0 {
			return fmt.Errorf("template %q file %q is empty", template, rel)
		}
		files = append(files, starterTemplateFile{Path: rel, Content: content})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read template files %q: %w", template, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// loadStarterOntologyTemplates returns every `.graphql` file under a template's
// `ontology/` directory, preserving the source filename. Templates name their
// schemas after themselves (e.g. `spec-driven.graphql`) so multiple templates
// can be installed side by side without overwriting each other.
func loadStarterOntologyTemplates(template string) ([]starterTemplateFile, error) {
	if template == "" {
		return nil, nil
	}
	root := path.Join("templates", "starters", template, "rhizome", "ontology")
	if _, err := fs.Stat(agentHelperTemplates, root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat ontology templates %q: %w", template, err)
	}
	var files []starterTemplateFile
	if err := fs.WalkDir(agentHelperTemplates, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(p), ".graphql") {
			return fmt.Errorf("non-graphql file %q under %s", p, root)
		}
		prefix := root + "/"
		if !strings.HasPrefix(p, prefix) {
			return fmt.Errorf("ontology template file %q is not under %q", p, root)
		}
		rel := strings.TrimPrefix(p, prefix)
		if rel == "" {
			return nil
		}
		content, err := fs.ReadFile(agentHelperTemplates, p)
		if err != nil {
			return err
		}
		if len(content) == 0 {
			return fmt.Errorf("ontology template %q file %q is empty", template, rel)
		}
		files = append(files, starterTemplateFile{Path: rel, Content: content})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read ontology template files %q: %w", template, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func loadStarterQueryRecipeTemplates(template string) ([]starterTemplateFile, error) {
	return loadStarterFamilyTemplates(template, "query-recipes", "query recipe")
}

func loadStarterViewTemplates(template string) ([]starterTemplateFile, error) {
	return loadStarterFamilyTemplates(template, "views", "view")
}

func loadStarterFamilyTemplates(template, family, label string) ([]starterTemplateFile, error) {
	root := path.Join("templates", "starters", template, "rhizome", family)
	if _, err := fs.Stat(agentHelperTemplates, root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat %s templates %q: %w", label, template, err)
	}
	var files []starterTemplateFile
	if err := fs.WalkDir(agentHelperTemplates, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		prefix := root + "/"
		if !strings.HasPrefix(p, prefix) {
			return fmt.Errorf("%s template file %q is not under %q", label, p, root)
		}
		rel := strings.TrimPrefix(p, prefix)
		if rel == "" {
			return nil
		}
		content, err := fs.ReadFile(agentHelperTemplates, p)
		if err != nil {
			return err
		}
		if len(content) == 0 {
			return fmt.Errorf("%s template %q file %q is empty", label, template, rel)
		}
		files = append(files, starterTemplateFile{Path: rel, Content: content})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read %s template files %q: %w", label, template, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func loadStarterManagedDocTemplate(template, name string) (string, error) {
	if template == "" {
		return "", nil
	}
	p := path.Join("templates", "starters", template, "agents", name)
	b, err := fs.ReadFile(agentHelperTemplates, p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read managed doc template %q for %q: %w", name, template, err)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", fmt.Errorf("managed doc template %q for %q is empty", name, template)
	}
	return s, nil
}

func applyPlaceholders(body string, replacements map[string]string) string {
	if len(replacements) == 0 {
		return body
	}
	var pairs []string
	for k, v := range replacements {
		pairs = append(pairs, k, v)
	}
	r := strings.NewReplacer(pairs...)
	return r.Replace(body)
}

func expandAgentTemplateEmbeds(body string) (string, error) {
	if !strings.Contains(body, "![[") {
		return body, nil
	}

	// We only expand Obsidian-style embeds (e.g., ![[Title]]) that appear in normal
	// prose. Ignore anything inside fenced code blocks or inline code spans so
	// templates can mention embed syntax safely.
	//
	// This is intentionally a small, purpose-built markdown scanner rather than
	// a full parser.
	var b strings.Builder

	inFence := false
	var fenceChar byte
	var fenceLen int

	inCodeSpan := false
	var codeSpanLen int

	for i := 0; i < len(body); {
		atLineStart := i == 0 || body[i-1] == '\n'
		if atLineStart && !inCodeSpan {
			if ok, fc, fl, next := parseMarkdownFenceLine(body, i); ok {
				// Fence delimiter line; copy as-is and toggle fence state.
				b.WriteString(body[i:next])
				if !inFence {
					inFence = true
					fenceChar = fc
					fenceLen = fl
				} else if fc == fenceChar && fl >= fenceLen {
					inFence = false
					fenceChar = 0
					fenceLen = 0
				}
				i = next
				continue
			}
			if inFence {
				// Inside fenced code block; copy the whole line and keep going.
				next := nextLineStart(body, i)
				b.WriteString(body[i:next])
				i = next
				continue
			}
		}

		// Not in a fenced code block.
		if body[i] == '`' {
			runLen := countRun(body, i, '`')
			b.WriteString(body[i : i+runLen])
			if inCodeSpan {
				if runLen == codeSpanLen {
					inCodeSpan = false
					codeSpanLen = 0
				}
			} else {
				inCodeSpan = true
				codeSpanLen = runLen
			}
			i += runLen
			continue
		}

		if !inCodeSpan && strings.HasPrefix(body[i:], "![[") {
			raw, consumed, ok := parseObsidianEmbed(body[i:])
			if !ok {
				b.WriteByte(body[i])
				i++
				continue
			}
			target, err := normalizeEmbedTarget(raw)
			if err != nil {
				return "", err
			}
			content, err := loadRhizomeMdTemplate(target)
			if err != nil {
				return "", fmt.Errorf("expand embed %q: %w", raw, err)
			}
			b.WriteString(content)
			i += consumed
			continue
		}

		b.WriteByte(body[i])
		i++
	}
	return b.String(), nil
}

func nextLineStart(s string, start int) int {
	if start >= len(s) {
		return len(s)
	}
	if idx := strings.IndexByte(s[start:], '\n'); idx >= 0 {
		return start + idx + 1
	}
	return len(s)
}

// parseMarkdownFenceLine detects CommonMark-style fenced code block delimiters.
// It assumes start is at the beginning of a line.
//
// Returns ok plus the fence char/length, and next which is the index of the
// start of the next line.
func parseMarkdownFenceLine(s string, start int) (ok bool, fenceChar byte, fenceLen int, next int) {
	i := start
	spaces := 0
	for i < len(s) && spaces < 3 && s[i] == ' ' {
		i++
		spaces++
	}
	if i >= len(s) {
		return false, 0, 0, nextLineStart(s, start)
	}
	c := s[i]
	if c != '`' && c != '~' {
		return false, 0, 0, nextLineStart(s, start)
	}
	run := countRun(s, i, c)
	if run < 3 {
		return false, 0, 0, nextLineStart(s, start)
	}
	return true, c, run, nextLineStart(s, start)
}

func countRun(s string, start int, c byte) int {
	i := start
	for i < len(s) && s[i] == c {
		i++
	}
	return i - start
}

// parseObsidianEmbed parses a string starting with "![[", returning the raw
// target (inside the brackets) and how many bytes were consumed.
func parseObsidianEmbed(s string) (raw string, consumed int, ok bool) {
	if !strings.HasPrefix(s, "![[") {
		return "", 0, false
	}
	close := strings.Index(s[3:], "]]")
	if close < 0 {
		return "", 0, false
	}
	raw = strings.TrimSpace(s[3 : 3+close])
	return raw, 3 + close + 2, true
}

func normalizeEmbedTarget(raw string) (string, error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", fmt.Errorf("embed target is empty")
	}
	if cut := strings.IndexAny(target, "|#"); cut >= 0 {
		target = strings.TrimSpace(target[:cut])
	}
	if target == "" {
		return "", fmt.Errorf("embed target %q is empty", raw)
	}
	if !strings.HasSuffix(strings.ToLower(target), ".md") {
		target += ".md"
	}
	return target, nil
}

// LoadAgentHelperTemplate returns the embedded helper template content.
func LoadAgentHelperTemplate(name string) (string, error) {
	return loadAgentHelperTemplate(name)
}
