package skilloverlay

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
	require.Equal(t, []Entry{{
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
		{OverlayID: "a", Slot: "validation.default-checks", Op: Prepend, Order: 20, Content: "- Gather context."},
		{OverlayID: "b", Slot: "validation.default-checks", Op: Append, Order: 30, Content: "- Record evidence."},
		{OverlayID: "c", Slot: "context.before-draft", Op: Append, Order: 10, Content: "- Load domain pack."},
	})
	require.NoError(t, err)
	require.Equal(t, "- Gather context.\n\n- Run default checks.\n\n- Record evidence.\n\n- Load domain pack.", out)
	require.Len(t, entries, 3)
	require.Equal(t, []string{"a", "b", "c"}, []string{entries[0].OverlayID, entries[1].OverlayID, entries[2].OverlayID})

	replaced, _, err := renderSkillTemplateMarkdown("plan", "SKILL.md", in, []resolvedSkillOverlayFragment{
		{OverlayID: "replace", Slot: "validation.default-checks", Op: Replace, Content: "- Run replacement checks."},
	})
	require.NoError(t, err)
	require.Contains(t, replaced, "- Run replacement checks.")
	require.NotContains(t, replaced, "- Run default checks.")

	suppressed, entries, err := renderSkillTemplateMarkdown("plan", "SKILL.md", in, []resolvedSkillOverlayFragment{
		{OverlayID: "suppress", Slot: "validation.default-checks", Op: Suppress},
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
		{OverlayID: "replace", Slot: "context.before-draft", Op: Replace, Content: "bad"},
	})
	requireOverlayIssueCode(t, err, "invalid_operation")
}

func TestApplySkillOverlaysRejectsUnknownTargetAndSlot(t *testing.T) {
	skills := []Skill{{
		Name: "plan",
		Files: []File{{
			Path:       "SKILL.md",
			Content:    []byte(`<!-- rzm:skill-slot id="known" mode="extension" -->` + "\n" + `<!-- /rzm:skill-slot -->`),
			IsMarkdown: true,
		}},
	}}

	_, _, err := Render(skills, []LoadedOverlay{{
		Template: "fake",
		Path:     "fake.yaml",
		Overlay:  Overlay{APIVersion: APIVersion, ID: "bad-target", TargetSkill: "missing", Fragments: []Fragment{{Slot: "known", Op: Append, Content: "x"}}},
	}})
	requireOverlayIssueCode(t, err, "unknown_target_skill")

	_, _, err = Render(skills, []LoadedOverlay{{
		Template: "fake",
		Path:     "fake.yaml",
		Overlay:  Overlay{APIVersion: APIVersion, ID: "bad-slot", TargetSkill: "plan", Fragments: []Fragment{{Slot: "missing", Op: Append, Content: "x"}}},
	}})
	requireOverlayIssueCode(t, err, "unknown_slot")
}

func TestApplySkillOverlaysRejectsDuplicateOverlayIDs(t *testing.T) {
	skills := []Skill{{
		Name: "plan",
		Files: []File{{
			Path:       "SKILL.md",
			Content:    []byte(`<!-- rzm:skill-slot id="known" mode="extension" -->` + "\n" + `<!-- /rzm:skill-slot -->`),
			IsMarkdown: true,
		}},
	}}

	overlay := Overlay{
		APIVersion:  APIVersion,
		ID:          "duplicate",
		TargetSkill: "plan",
		Fragments:   []Fragment{{Slot: "known", Op: Append, Content: "x"}},
	}
	_, _, err := Render(skills, []LoadedOverlay{
		{Template: "a", Path: "a.yaml", Overlay: overlay},
		{Template: "b", Path: "b.yaml", Overlay: overlay},
	})
	requireOverlayIssueCode(t, err, "duplicate_overlay_id")
}

func TestApplySkillOverlaysOrdersFragmentsDeterministically(t *testing.T) {
	skills := []Skill{{
		Name: "plan",
		Files: []File{{
			Path:       "SKILL.md",
			Content:    []byte(`<!-- rzm:skill-slot id="context" mode="extension" -->` + "\n" + `<!-- /rzm:skill-slot -->`),
			IsMarkdown: true,
		}},
	}}

	rendered, report, err := Render(skills, []LoadedOverlay{
		{Template: "b", TemplateOrder: 1, Path: "b.yaml", Overlay: Overlay{APIVersion: APIVersion, ID: "b", TargetSkill: "plan", Fragments: []Fragment{{Slot: "context", Op: Append, Order: 1, Content: "third"}}}},
		{Template: "a", TemplateOrder: 0, Path: "z.yaml", Overlay: Overlay{APIVersion: APIVersion, ID: "a2", TargetSkill: "plan", Fragments: []Fragment{{Slot: "context", Op: Append, Order: 1, Content: "second"}}}},
		{Template: "a", TemplateOrder: 0, Path: "a.yaml", Overlay: Overlay{APIVersion: APIVersion, ID: "a1", TargetSkill: "plan", Fragments: []Fragment{{Slot: "context", Op: Append, Order: 100, Content: "first"}}}},
	}, "a", "b")
	require.NoError(t, err)
	require.Len(t, rendered, 1)
	require.Equal(t, "first\n\nsecond\n\nthird", string(rendered[0].Files[0].Content))
	require.Equal(t, []string{"a1", "a2", "b"}, []string{report.Entries[0].OverlayID, report.Entries[1].OverlayID, report.Entries[2].OverlayID})
	require.Equal(t, []string{"a", "b"}, report.Templates)
	require.Equal(t, APIVersion, report.Manifest().APIVersion)
}

func requireOverlayIssueCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var overlayErr Error
	require.Truef(t, errors.As(err, &overlayErr), "expected skillOverlayError, got %T: %v", err, err)
	for _, issue := range overlayErr.Issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("expected overlay issue code %q in %#v", code, overlayErr.Issues)
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	_, err := Decode("bad.yaml", []byte("apiVersion: rhizome.skill-overlay.v1\nid: bad\ntargetSkill: plan\nunknown: true\nfragments: []\n"))
	requireOverlayIssueCode(t, err, "overlay_yaml_decode_error")
}
