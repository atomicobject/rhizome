package init

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSkillOverlayRenderStripsUnusedExtensionSlot(t *testing.T) {
	in := strings.Join([]string{
		"before",
		`<!-- rzm:skill-slot id="context.before-draft" mode="extension" -->`,
		`<!-- /rzm:skill-slot -->`,
		"after",
	}, "\n")

	out, entries, err := renderSkillTemplateMarkdown("specify", "SKILL.md", in, nil)
	require.NoError(t, err)
	require.Equal(t, "before\nafter", out)
	require.Equal(t, []skillOverlayManifestEntry{{
		TargetSkill: "specify",
		Slot:        "context.before-draft",
		Outcome:     "stripped_empty_slot",
	}}, entries)
	require.NotContains(t, out, "rzm:skill-slot")
}

func TestSkillOverlayRenderPreservesReplaceableDefault(t *testing.T) {
	in := strings.Join([]string{
		"before",
		`<!-- rzm:skill-slot id="validation.default-checks" mode="replaceable" -->`,
		"- Run default checks.",
		`<!-- /rzm:skill-slot -->`,
		"after",
	}, "\n")

	out, entries, err := renderSkillTemplateMarkdown("plan", "SKILL.md", in, nil)
	require.NoError(t, err)
	require.Equal(t, "before\n- Run default checks.\nafter", out)
	require.Len(t, entries, 1)
	require.Equal(t, "preserved_default", entries[0].Outcome)
	require.NotEmpty(t, entries[0].ContentHash)
}

func TestSkillOverlayRenderAppliesPrependAppendReplaceAndSuppress(t *testing.T) {
	in := strings.Join([]string{
		`<!-- rzm:skill-slot id="validation.default-checks" mode="replaceable" -->`,
		"- Run default checks.",
		`<!-- /rzm:skill-slot -->`,
		"",
		`<!-- rzm:skill-slot id="context.before-draft" mode="extension" -->`,
		`<!-- /rzm:skill-slot -->`,
	}, "\n")

	out, entries, err := renderSkillTemplateMarkdown("plan", "SKILL.md", in, []resolvedSkillOverlayFragment{
		{OverlayID: "a", Slot: "validation.default-checks", Op: skillOverlayOpPrepend, Order: 20, Content: "- Gather context."},
		{OverlayID: "b", Slot: "validation.default-checks", Op: skillOverlayOpAppend, Order: 30, Content: "- Record evidence."},
		{OverlayID: "c", Slot: "context.before-draft", Op: skillOverlayOpAppend, Order: 10, Content: "- Load domain pack."},
	})
	require.NoError(t, err)
	require.Equal(t, "- Gather context.\n\n- Run default checks.\n\n- Record evidence.\n\n- Load domain pack.", out)
	require.Len(t, entries, 3)
	require.Equal(t, []string{"a", "b", "c"}, []string{entries[0].OverlayID, entries[1].OverlayID, entries[2].OverlayID})

	replaced, _, err := renderSkillTemplateMarkdown("plan", "SKILL.md", in, []resolvedSkillOverlayFragment{
		{OverlayID: "replace", Slot: "validation.default-checks", Op: skillOverlayOpReplace, Content: "- Run replacement checks."},
	})
	require.NoError(t, err)
	require.Contains(t, replaced, "- Run replacement checks.")
	require.NotContains(t, replaced, "- Run default checks.")

	suppressed, entries, err := renderSkillTemplateMarkdown("plan", "SKILL.md", in, []resolvedSkillOverlayFragment{
		{OverlayID: "suppress", Slot: "validation.default-checks", Op: skillOverlayOpSuppress},
	})
	require.NoError(t, err)
	require.Equal(t, "", suppressed)
	require.Equal(t, "suppressed_default", entries[0].Outcome)
}

func TestSkillOverlayRenderRejectsInvalidSlotMarkup(t *testing.T) {
	cases := []struct {
		name string
		body string
		code string
	}{
		{
			name: "duplicate slot",
			body: strings.Join([]string{
				`<!-- rzm:skill-slot id="x" mode="extension" -->`,
				`<!-- /rzm:skill-slot -->`,
				`<!-- rzm:skill-slot id="x" mode="extension" -->`,
				`<!-- /rzm:skill-slot -->`,
			}, "\n"),
			code: "duplicate_slot_id",
		},
		{
			name: "unknown attr",
			body: `<!-- rzm:skill-slot id="x" mode="extension" extra="bad" -->` + "\n" + `<!-- /rzm:skill-slot -->`,
			code: "invalid_slot_markup",
		},
		{
			name: "invalid mode",
			body: `<!-- rzm:skill-slot id="x" mode="append" -->` + "\n" + `<!-- /rzm:skill-slot -->`,
			code: "invalid_slot_mode",
		},
		{
			name: "unterminated",
			body: `<!-- rzm:skill-slot id="x" mode="extension" -->`,
			code: "unterminated_slot",
		},
		{
			name: "nested",
			body: strings.Join([]string{
				`<!-- rzm:skill-slot id="x" mode="extension" -->`,
				`<!-- rzm:skill-slot id="y" mode="extension" -->`,
				`<!-- /rzm:skill-slot -->`,
			}, "\n"),
			code: "nested_slot",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := renderSkillTemplateMarkdown("skill", "SKILL.md", tc.body, nil)
			requireOverlayIssueCode(t, err, tc.code)
		})
	}
}

func TestSkillOverlayRenderIgnoresMarkerLikeTextInFencedCode(t *testing.T) {
	in := strings.Join([]string{
		"```md",
		`<!-- rzm:skill-slot id="x" mode="extension" -->`,
		`<!-- /rzm:skill-slot -->`,
		"```",
		"after",
	}, "\n")

	out, _, err := renderSkillTemplateMarkdown("skill", "SKILL.md", in, nil)
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestSkillOverlayRenderRejectsInvalidOperations(t *testing.T) {
	in := strings.Join([]string{
		`<!-- rzm:skill-slot id="context.before-draft" mode="extension" -->`,
		`<!-- /rzm:skill-slot -->`,
	}, "\n")

	_, _, err := renderSkillTemplateMarkdown("specify", "SKILL.md", in, []resolvedSkillOverlayFragment{
		{OverlayID: "replace", Slot: "context.before-draft", Op: skillOverlayOpReplace, Content: "bad"},
	})
	requireOverlayIssueCode(t, err, "invalid_operation")
}

func TestLoadStarterSkillOverlayTemplatesStrict(t *testing.T) {
	overlays, err := loadStarterSkillOverlayTemplates(templateAgenticEngineering)
	require.NoError(t, err)
	require.Empty(t, overlays)

	_, err = decodeSkillOverlayFile("bad.yaml", []byte("apiVersion: rhizome.skill-overlay.v1\nid: bad\ntargetSkill: plan\nunknown: true\nfragments: []\n"))
	requireOverlayIssueCode(t, err, "overlay_yaml_decode_error")
}

func TestLoadAllSkillTemplatesStripsAgenticEngineeringSlotMarkers(t *testing.T) {
	skills, err := loadAllSkillTemplates([]string{templateAgenticEngineering})
	require.NoError(t, err)

	for _, name := range []string{"agentic-engineering", "ingest-transcript"} {
		for _, file := range requireSkillTemplate(t, skills, name).Files {
			body := string(file.Content)
			require.NotContains(t, body, "rzm:skill-slot")
			require.NotContains(t, body, "/rzm:skill-slot")
		}
	}
}

func TestLoadAllSkillTemplatesAppliesComplexDomainOverlays(t *testing.T) {
	skills, manifest, err := loadAllSkillTemplatesWithReport([]string{templateAgenticEngineering, templateComplexDomain, templateActionItems})
	require.NoError(t, err)

	for _, name := range []string{"agentic-engineering", "ingest-transcript"} {
		for _, file := range requireSkillTemplate(t, skills, name).Files {
			body := string(file.Content)
			require.NotContains(t, body, "rzm:skill-slot")
			require.NotContains(t, body, "/rzm:skill-slot")
		}
	}

	router := requireSkillTemplate(t, skills, "agentic-engineering")
	specify := string(requireSkillTemplateFile(t, router, "references/specification.md").Content)
	require.Contains(t, specify, "domain-topic-survey")
	require.Contains(t, specify, "candidate requirements that should not yet become delivery commitments")

	plan := string(requireSkillTemplateFile(t, router, "references/planning.md").Content)
	require.Contains(t, plan, "spec-domain-context-pack")
	require.Contains(t, plan, "complex-domain.uncovered-requirements")

	implement := string(requireSkillTemplateFile(t, router, "references/implementation.md").Content)
	require.Contains(t, implement, "requirement-trace-pack")
	require.Contains(t, implement, "domain-backport")
	require.Contains(t, implement, "misunderstood domain rules")

	compounding := string(requireSkillTemplateFile(t, router, "references/compounding.md").Content)
	require.Contains(t, compounding, "coverage and stale-source views")

	require.ElementsMatch(t, []string{
		"agentic-engineering:alignment.additional-context",
		"agentic-engineering:reconciliation.additional-targets",
		"agentic-engineering:routing.additional-compounding",
		"agentic-engineering:scope.additional-context",
		"agentic-engineering:inputs.additional-context",
		"agentic-engineering:docs.additional-traceability",
		"ingest-transcript:source.additional-preflight",
		"ingest-transcript:synthesis.additional-tracks",
		"agentic-engineering:integration-map.additional-dimensions",
		"agentic-engineering:architecture-decisions.additional-checks",
		"agentic-engineering:context.after-discovery",
		"agentic-engineering:context.constraint-extraction",
	}, uniqueManifestEntryTargets(manifest))
}

func TestApplySkillOverlaysRejectsUnknownTargetAndSlot(t *testing.T) {
	skills := []skillTemplate{{
		Name: "plan",
		Files: []skillTemplateFile{{
			Path:       "SKILL.md",
			Content:    []byte(`<!-- rzm:skill-slot id="known" mode="extension" -->` + "\n" + `<!-- /rzm:skill-slot -->`),
			IsMarkdown: true,
		}},
	}}

	_, _, err := applySkillOverlays(skills, []loadedSkillOverlay{{
		Template: "fake",
		Path:     "fake.yaml",
		Overlay:  skillOverlay{APIVersion: skillOverlayAPIVersion, ID: "bad-target", TargetSkill: "missing", Fragments: []skillOverlayFragment{{Slot: "known", Op: skillOverlayOpAppend, Content: "x"}}},
	}})
	requireOverlayIssueCode(t, err, "unknown_target_skill")

	_, _, err = applySkillOverlays(skills, []loadedSkillOverlay{{
		Template: "fake",
		Path:     "fake.yaml",
		Overlay:  skillOverlay{APIVersion: skillOverlayAPIVersion, ID: "bad-slot", TargetSkill: "plan", Fragments: []skillOverlayFragment{{Slot: "missing", Op: skillOverlayOpAppend, Content: "x"}}},
	}})
	requireOverlayIssueCode(t, err, "unknown_slot")
}

func TestApplySkillOverlaysRejectsDuplicateOverlayIDs(t *testing.T) {
	skills := []skillTemplate{{
		Name: "plan",
		Files: []skillTemplateFile{{
			Path:       "SKILL.md",
			Content:    []byte(`<!-- rzm:skill-slot id="known" mode="extension" -->` + "\n" + `<!-- /rzm:skill-slot -->`),
			IsMarkdown: true,
		}},
	}}

	overlay := skillOverlay{
		APIVersion:  skillOverlayAPIVersion,
		ID:          "duplicate",
		TargetSkill: "plan",
		Fragments:   []skillOverlayFragment{{Slot: "known", Op: skillOverlayOpAppend, Content: "x"}},
	}
	_, _, err := applySkillOverlays(skills, []loadedSkillOverlay{
		{Template: "a", Path: "a.yaml", Overlay: overlay},
		{Template: "b", Path: "b.yaml", Overlay: overlay},
	})
	requireOverlayIssueCode(t, err, "duplicate_overlay_id")
}

func TestApplySkillOverlaysOrdersFragmentsDeterministically(t *testing.T) {
	skills := []skillTemplate{{
		Name: "plan",
		Files: []skillTemplateFile{{
			Path:       "SKILL.md",
			Content:    []byte(`<!-- rzm:skill-slot id="context" mode="extension" -->` + "\n" + `<!-- /rzm:skill-slot -->`),
			IsMarkdown: true,
		}},
	}}

	rendered, report, err := applySkillOverlays(skills, []loadedSkillOverlay{
		{Template: "b", TemplateOrder: 1, Path: "b.yaml", Overlay: skillOverlay{APIVersion: skillOverlayAPIVersion, ID: "b", TargetSkill: "plan", Fragments: []skillOverlayFragment{{Slot: "context", Op: skillOverlayOpAppend, Order: 1, Content: "third"}}}},
		{Template: "a", TemplateOrder: 0, Path: "z.yaml", Overlay: skillOverlay{APIVersion: skillOverlayAPIVersion, ID: "a2", TargetSkill: "plan", Fragments: []skillOverlayFragment{{Slot: "context", Op: skillOverlayOpAppend, Order: 1, Content: "second"}}}},
		{Template: "a", TemplateOrder: 0, Path: "a.yaml", Overlay: skillOverlay{APIVersion: skillOverlayAPIVersion, ID: "a1", TargetSkill: "plan", Fragments: []skillOverlayFragment{{Slot: "context", Op: skillOverlayOpAppend, Order: 100, Content: "first"}}}},
	}, "a", "b")
	require.NoError(t, err)
	require.Len(t, rendered, 1)
	require.Equal(t, "first\n\nsecond\n\nthird", string(rendered[0].Files[0].Content))
	require.Equal(t, []string{"a1", "a2", "b"}, []string{report.Entries[0].OverlayID, report.Entries[1].OverlayID, report.Entries[2].OverlayID})
	require.Equal(t, []string{"a", "b"}, report.Templates)
	require.Equal(t, skillOverlayAPIVersion, report.Manifest().APIVersion)
}

func requireSkillTemplate(t *testing.T, skills []skillTemplate, name string) skillTemplate {
	t.Helper()
	for _, skill := range skills {
		if skill.Name == name {
			return skill
		}
	}
	t.Fatalf("expected skill template %q in %#v", name, skills)
	return skillTemplate{}
}

func requireSkillTemplateFile(t *testing.T, skill skillTemplate, filePath string) skillTemplateFile {
	t.Helper()
	for _, file := range skill.Files {
		if file.Path == filePath {
			return file
		}
	}
	t.Fatalf("expected skill template %q file %q in %#v", skill.Name, filePath, skill.Files)
	return skillTemplateFile{}
}

func manifestEntryTargets(manifest SkillOverlayManifest) []string {
	out := make([]string, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		out = append(out, entry.TargetSkill+":"+entry.Slot)
	}
	return out
}

func uniqueManifestEntryTargets(manifest SkillOverlayManifest) []string {
	seen := make(map[string]struct{}, len(manifest.Entries))
	var targets []string
	for _, target := range manifestEntryTargets(manifest) {
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	return targets
}

func requireOverlayIssueCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var overlayErr skillOverlayError
	require.Truef(t, errors.As(err, &overlayErr), "expected skillOverlayError, got %T: %v", err, err)
	for _, issue := range overlayErr.Issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("expected overlay issue code %q in %#v", code, overlayErr.Issues)
}
