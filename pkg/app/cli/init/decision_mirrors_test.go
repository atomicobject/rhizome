package init

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDecisionDoesNotReplaceUnlistedMirror(t *testing.T) {
	for _, state := range []string{"local", "declined"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, root, "a", "review me")
			writeTestFile(t, root, "b", "keep my edits")
			record := &generatedFiles{Written: map[string]string{"a": "old", "b": contentFingerprint([]byte("new"))}, Declined: map[string]string{}}
			if state == "declined" {
				record.Declined["b"] = contentFingerprint([]byte("new"))
			}
			plan := &filePlan{files: []generatedFile{{rel: "a", group: "skill", content: []byte("new"), mode: 0o644}, {rel: "b", group: "skill", content: []byte("new"), mode: 0o644}}}
			prepared, err := classifyFilePlan(root, plan, record)
			require.NoError(t, err)
			decisions := prepared.decisions()
			require.Len(t, decisions, 1)
			require.Empty(t, decisions[0].Copies)
			require.NoError(t, prepared.setChoices([]string{"a"}, nil))
			_, err = prepared.apply(nil)
			require.NoError(t, err)
			require.Equal(t, "new", readTestFile(t, root, "a"))
			require.Equal(t, "keep my edits", readTestFile(t, root, "b"))
		})
	}
}

func TestDifferentMirrorEditsHaveSeparateDecisions(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a", "first customization")
	writeTestFile(t, root, "b", "second customization")
	plan := &filePlan{files: []generatedFile{{rel: "a", group: "skill", content: []byte("new"), mode: 0o644}, {rel: "b", group: "skill", content: []byte("new"), mode: 0o644}}}
	prepared, err := classifyFilePlan(root, plan, &generatedFiles{Written: map[string]string{}, Declined: map[string]string{}})
	require.NoError(t, err)
	decisions := prepared.decisions()
	require.Len(t, decisions, 2)
	require.Contains(t, findSetupDecision(t, decisions, "a").Diff, "first customization")
	require.Contains(t, findSetupDecision(t, decisions, "b").Diff, "second customization")
	require.NoError(t, prepared.setChoices([]string{"a"}, []string{"b"}))
	_, err = prepared.apply(nil)
	require.NoError(t, err)
	require.Equal(t, "new", readTestFile(t, root, "a"))
	require.Equal(t, "second customization", readTestFile(t, root, "b"))
}
