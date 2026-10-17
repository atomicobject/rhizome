package init

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/skilloverlay"
)

func ValidateBundledSkillOverlays() (skilloverlay.Manifest, error) {
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return skilloverlay.Manifest{}, err
	}
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	aggregate := skilloverlay.Report{Templates: ids}
	for _, id := range ids {
		effective, err := resolveEffectiveTemplatesWithOptions([]string{id}, nil, false)
		if err != nil {
			issue := skilloverlay.Issue{Code: "template_resolution_error", Skill: id, Message: fmt.Sprint(err)}
			aggregate.Issues = append(aggregate.Issues, issue)
			continue
		}
		_, manifest, err := loadAllSkillTemplatesWithReport(effective)
		aggregate.Entries = append(aggregate.Entries, manifest.Entries...)
		aggregate.Issues = append(aggregate.Issues, manifest.Issues...)
		if err != nil {
			before := len(aggregate.Issues)
			var overlayErr skilloverlay.Error
			if errors.As(err, &overlayErr) {
				aggregate.Issues = append(aggregate.Issues, overlayErr.Issues...)
			}
			if len(aggregate.Issues) == before {
				aggregate.Issues = append(aggregate.Issues, skilloverlay.Issue{Code: "render_error", Skill: strings.Join(effective, ","), Message: fmt.Sprint(err)})
			}
		}
	}
	return aggregate.Manifest(), nil
}

func loadStarterSkillOverlayTemplates(template string, templateOrder ...int) ([]skilloverlay.LoadedOverlay, error) {
	if template == "" {
		return nil, nil
	}
	order := 0
	if len(templateOrder) > 0 {
		order = templateOrder[0]
	}
	root := path.Join("templates", "starters", template, "agents", "skill-overlays")
	if _, err := fs.Stat(agentHelperTemplates, root); err != nil {
		if errorsIsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat skill overlay templates %q: %w", template, err)
	}
	var out []skilloverlay.LoadedOverlay
	if err := fs.WalkDir(agentHelperTemplates, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(p), ".yaml") && !strings.HasSuffix(strings.ToLower(p), ".yml") {
			return skilloverlay.Error{Issues: []skilloverlay.Issue{{Code: "overlay_yaml_decode_error", Path: p, Message: "skill overlay file must be yaml"}}}
		}
		prefix := root + "/"
		if !strings.HasPrefix(p, prefix) {
			return fmt.Errorf("skill overlay template file %q is not under %q", p, root)
		}
		content, err := fs.ReadFile(agentHelperTemplates, p)
		if err != nil {
			return err
		}
		overlay, err := skilloverlay.Decode(p, content)
		if err != nil {
			return err
		}
		out = append(out, skilloverlay.LoadedOverlay{
			Template:      template,
			TemplateOrder: order,
			Path:          strings.TrimPrefix(p, prefix),
			Overlay:       overlay,
		})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read skill overlay template files %q: %w", template, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
