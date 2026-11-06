package init

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestAOKBKnowledgeFixture(t *testing.T) {
	root := installAOKBFixture(t)
	vault := obsidian.VaultDefinition{Name: "booking", Root: root, Includes: []string{"**/*.md", ".kb/curation/_conflicts.md"}, Links: obsidian.LinkTypeBoth}
	metadata := testkit.NoteMetadataIndexer(t)
	runtime, cleanup, err := ontologytest.Open(context.Background(), metadata, vault, &obsidian.Note{})
	require.NoError(t, err)
	defer cleanup()
	require.True(t, runtime.Ready)
	exec, err := ontologyquery.BuildExecutableSchema(runtime.Schema)
	require.NoError(t, err)
	formats, err := metadata.FormatRuntime()
	require.NoError(t, err)
	deps := ontologyquery.Deps{VaultDef: vault, NoteReader: &obsidian.Note{}, NoteFormats: formats, Store: runtime.Store}
	query := func(t *testing.T, s string) map[string]any {
		t.Helper()
		prepared, errs := ontologyquery.Prepare(exec, s)
		require.Empty(t, errs)
		result := ontologyquery.ExecutePrepared(context.Background(), deps, runtime.Schema, exec, prepared)
		require.Empty(t, result.Errors)
		return result.Data
	}
	t.Run("nested provenance and map-shaped frontmatter connections", func(t *testing.T) {
		row := func(slug string) map[string]any {
			rows := query(t, `{ assertion(path:"assertions/`+slug+`.md") { provenanceSource provenanceLocator provenanceSpeaker provenanceSpeakerRole provenanceQuoteHash provenanceExtractedBy frontmatterSupports frontmatterRelatesTo frontmatterRefines } }`)["assertion"].([]any)
			require.Len(t, rows, 1)
			return rows[0].(map[string]any)
		}
		origin := row("clinic-patient-books-own-slot")
		require.Equal(t, "s-000000000001", origin["provenanceSource"], "recorded origin stays the authored logical id")
		visits := row("video-visits-approved")
		require.Equal(t, "L12-L18", visits["provenanceLocator"])
		require.Equal(t, "Morgan, clinic operations", visits["provenanceSpeaker"])
		require.Equal(t, "stakeholder", visits["provenanceSpeakerRole"])
		require.Equal(t, "llm@ao-kb@fixture", visits["provenanceExtractedBy"])
		require.Contains(t, visits["provenanceQuoteHash"], "sha256:")
		require.Equal(t, []any{"a-000000000001"}, visits["frontmatterSupports"], "map with inline list")
		require.Equal(t, []any{"a-00000000000d"}, row("eligibility-question-old")["frontmatterRelatesTo"], "map with block sequence")
		require.Empty(t, row("eligibility-question-specific")["frontmatterRefines"], "typed-map sequences are not projected; the check action reports them")
		require.Nil(t, row("minors-need-guardian")["provenanceSpeaker"], "missing nested values stay missing")
	})
	t.Run("fields and native identity", func(t *testing.T) {
		rows := query(t, `{ assertion(path:"assertions/clinic-patient-books-own-slot.md") { aoId nodeId path headline kind confidence attestation reviewedBy reviewedAt updatedAt contradictsRows supportsRows frontmatter content concepts { aoId } } }`)["assertion"].([]any)
		require.Len(t, rows, 1)
		n := rows[0].(map[string]any)
		require.Equal(t, "a-000000000001", n["aoId"])
		require.Equal(t, "note:assertions/clinic-patient-books-own-slot.md", n["nodeId"])
		require.Equal(t, "Patients book their own slots", n["headline"])
		require.Equal(t, "2026-10-20T10:00:00Z", n["updatedAt"])
		require.Equal(t, "direct", n["attestation"])
		require.Equal(t, []any{"feature:FEAT-BOOK-03"}, n["contradictsRows"])
		require.Contains(t, n["content"], "Patients can choose an available appointment")
		fm := n["frontmatter"].(map[string]any)
		require.Equal(t, "", fm["provenance"], "indexed frontmatter JSON drops nested objects; read typed provenance fields instead")
		require.Contains(t, n["content"], `speaker: "Morgan, clinic operations"`)
		require.Contains(t, n["content"], `source: "s-000000000001"`)

		require.Len(t, n["concepts"], 2)
	})
	t.Run("sources preserve section order and duplicate occurrences", func(t *testing.T) {
		rows := query(t, `{ assertion(path:"assertions/clinic-patient-books-own-slot.md") { sourcesSection { citations { recordedLink content sources { aoId } } } } }`)["assertion"].([]any)
		citations := rows[0].(map[string]any)["sourcesSection"].(map[string]any)["citations"].([]any)
		require.Len(t, citations, 3)
		for i, link := range []string{"[[patient-booking-interview]]", "[[clinic-operations-sync]]", "[[patient-booking-interview]]"} {
			require.Equal(t, link, citations[i].(map[string]any)["recordedLink"])
		}
		require.Equal(t, []any{map[string]any{"aoId": "s-000000000001"}}, citations[1].(map[string]any)["sources"])
		require.Empty(t, citations[0].(map[string]any)["sources"], "duplicate unanchored occurrences expose a named identity deficiency; the authored target remains inspectable")
		target := query(t, `{ node(ref:"sources/patient-booking-interview.md") { path } }`)["node"].(map[string]any)
		require.Equal(t, "sources/patient-booking-interview.md", target["path"])

	})
	t.Run("connections and declared inverse", func(t *testing.T) {
		rows := query(t, `{ assertion(path:"assertions/clinic-patient-books-own-slot.md") { connectionsSection { corroboratedBy { aoId } supportedBy { aoId } supports { aoId } } supportedBy { notePath } } }`)["assertion"].([]any)
		n := rows[0].(map[string]any)
		c := n["connectionsSection"].(map[string]any)
		require.Len(t, c["corroboratedBy"], 2)
		require.Equal(t, []any{map[string]any{"aoId": "a-00000000000a"}}, c["supportedBy"])
		require.Equal(t, []any{map[string]any{"aoId": "a-000000000016"}}, c["supports"])
		require.Len(t, n["supportedBy"], 2)
	})
	t.Run("raw source exclusion and extraction identity gap", func(t *testing.T) {
		rawNode := query(t, `{ node(ref:"a-000000000001") { path } }`)["node"]
		require.NotNil(t, rawNode)
		n := rawNode.(map[string]any)
		require.Equal(t, "assertions/clinic-patient-books-own-slot.md", n["path"])
		sources := query(t, `{ source(first:30) { aoId path extractedAssertionIds extractedAssertions { aoId } } }`)["source"].([]any)
		require.Len(t, sources, 9)
		for _, raw := range sources {
			n := raw.(map[string]any)
			require.NotContains(t, n["path"], "/_raw/")
			if n["aoId"] == "s-000000000001" {
				require.Len(t, n["extractedAssertionIds"], 14)
				require.Empty(t, n["extractedAssertions"], "logical ID extraction is an evidenced indexer deficiency, not a slug/alias fixture workaround")
			}
		}
	})
	t.Run("source path order and scalar coercion", func(t *testing.T) {
		rows := query(t, `{ source(path:"sources/referral-attachments.md") { sourcePaths locatorScheme } }`)["source"].([]any)
		require.Equal(t, []any{"sources/_raw/referrals/cover.txt", "sources/_raw/referrals/approval.md", "https://example.invalid/referral-context"}, rows[0].(map[string]any)["sourcePaths"])
		rows = query(t, `{ source(path:"sources/clinic-operations-sync.md") { sourcePaths } }`)["source"].([]any)
		require.Equal(t, []any{"sources/_raw/meetings/clinic-operations-sync.txt"}, rows[0].(map[string]any)["sourcePaths"])
	})
	t.Run("authored states and missing attestation", func(t *testing.T) {
		data := query(t, `{
            missing: assertion(path:"assertions/no-sources-eligibility-check.md") { attestation speakerRole sourcesSection { content } }
            archived: assertion(path:"assertions/legacy-paper-booking.md") { status }
            expired: assertion(path:"assertions/saturday-hours-through-october.md") { validUntil }
            older: assertion(path:"assertions/reminders-forty-eight-hours.md") { supersededBy { notePath } }
            question: assertion(path:"assertions/same-day-video-question.md") { resolvedBy { notePath } }
        }`)
		missing := data["missing"].([]any)[0].(map[string]any)
		require.Nil(t, missing["attestation"])
		require.Nil(t, missing["sourcesSection"])
		require.Equal(t, "team", missing["speakerRole"])
		require.Equal(t, "archived", data["archived"].([]any)[0].(map[string]any)["status"])
		require.Equal(t, "2026-10-31", data["expired"].([]any)[0].(map[string]any)["validUntil"])
		require.Equal(t, []any{map[string]any{"notePath": "assertions/reminders-twenty-four-hours.md"}}, data["older"].([]any)[0].(map[string]any)["supersededBy"])
		require.Equal(t, []any{map[string]any{"notePath": "assertions/video-visits-approved.md"}}, data["question"].([]any)[0].(map[string]any)["resolvedBy"])
	})
	t.Run("related concepts scope", func(t *testing.T) {
		rows := query(t, `{ concept(path:"concepts/self-service.md") { relatedConceptsSection { relatedConcepts { aoId } } } }`)["concept"].([]any)
		related := rows[0].(map[string]any)["relatedConceptsSection"].(map[string]any)["relatedConcepts"]
		require.Equal(t, []any{map[string]any{"aoId": "cpt-000000000002"}}, related)
	})
	t.Run("artifact references", func(t *testing.T) {
		rows := query(t, `{ artifact(path:"artifacts/booking-prd.md") { graphSyncedAt lastReviewed supportedBy { aoId } additionalAssertionReferences { aoId } conceptReferences { aoId } sourceReferences { aoId } artifactReferences { aoId } } }`)["artifact"].([]any)
		n := rows[0].(map[string]any)
		require.Equal(t, "2026-10-21T10:00:00Z", n["graphSyncedAt"])
		for key, count := range map[string]int{"supportedBy": 2, "additionalAssertionReferences": 1, "conceptReferences": 1, "sourceReferences": 1, "artifactReferences": 1} {
			require.Len(t, n[key], count, key)
		}
	})
	t.Run("conflicts retain every field in authored order", func(t *testing.T) {
		rows := query(t, `{ conflictRegistry(first:5) { groups { entries { title locator { sourceLocator } assertions { aoId } fields { label value } } } } }`)["conflictRegistry"].([]any)
		require.Len(t, rows, 1)
		groups := rows[0].(map[string]any)["groups"].([]any)
		require.Len(t, groups, 2)
		entries := groups[0].(map[string]any)["entries"].([]any)
		require.Len(t, entries, 1)
		entry := entries[0].(map[string]any)
		require.Equal(t, "cf-001: Same-day cancellation policy", entry["title"])
		require.ElementsMatch(t, []any{map[string]any{"aoId": "a-000000000005"}, map[string]any{"aoId": "a-000000000006"}}, entry["assertions"])
		require.Contains(t, entry["locator"].(map[string]any)["sourceLocator"], ".kb/curation/_conflicts.md#")
		fields := entry["fields"].([]any)
		require.Len(t, fields, 15)
		require.Equal(t, map[string]any{"label": "State", "value": "open"}, fields[1])
		require.Equal(t, map[string]any{"label": "Impact override", "value": "blocking"}, fields[13])
		require.Equal(t, map[string]any{"label": "Clinic annotation", "value": "Keep both authored claims visible."}, fields[14])
	})
	t.Run("diagnostics retain unknown and dangling declarations", func(t *testing.T) {
		rows := query(t, `{ assertion(path:"assertions/malformed-passage-locator.md") { connectionsSection { content } } }`)["assertion"].([]any)
		text := rows[0].(map[string]any)["connectionsSection"].(map[string]any)["content"]
		require.Contains(t, text, "teleports-to:: [[clinic-patient-books-own-slot]]")
		require.Contains(t, text, "depends-on:: [[missing-booking-rule]]")
	})
}

func installAOKBFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join("..", "..", "..", "..", "testdata", "ao-kb", "booking")
	require.NoError(t, fs.WalkDir(os.DirFS(source), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(root, path)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := os.ReadFile(filepath.Join(source, path))
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o644)
	}))
	env := testkit.Env(t)
	opts := RunOptions{Env: env, Dir: root, Workflow: "none", Addons: "ao-kb", Agents: "none", Hooks: "off", Search: "off", BinaryManager: "external", Stdout: io.Discard, Stderr: io.Discard}
	require.NoError(t, Run(opts))
	opts.Addons = ""
	opts.Check = true
	require.NoError(t, Run(opts))
	return root
}
