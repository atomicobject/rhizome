package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	protocol "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func contextPublicationFixture(t *testing.T) (Config, *semdb.Store, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "café世界🙂")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	doc := "# Context\n\n" + strings.Repeat("context evidence ", 61) + "\nUNIQUE_NESTED_FINAL_EVIDENCE\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "CONTEXT.md"), []byte(doc), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644))
	store := newIntelStore(t)
	return Config{Vault: &obsidian.Vault{Name: root}, VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, IntelStore: store, SessionStore: store, NoteMetadata: configuredMCPNoteMetadata(t)}, store, doc
}

type contextPublicationCompressor struct {
	status   string
	err      error
	cancel   context.CancelFunc
	captured context.Context
}

func (c *contextPublicationCompressor) MaxInputChars() int { return 50000 }
func (c *contextPublicationCompressor) Compress(ctx context.Context, req contextpack.CompressRequest) (contextpack.CompressResult, error) {
	c.captured = ctx
	if c.cancel != nil {
		c.cancel()
		return contextpack.CompressResult{}, ctx.Err()
	}
	status := make(map[string]string)
	for _, piece := range req.Pieces {
		status[piece.Key] = c.status
	}
	return contextpack.CompressResult{Text: "compressed overview", PieceStatus: status}, c.err
}

func TestContextSessionPublicationCompressionRetainsRawRetry(t *testing.T) {
	for _, status := range []string{"summarized", "omitted", "full", ""} {
		t.Run(status, func(t *testing.T) {
			cfg, store, _ := contextPublicationFixture(t)
			cfg.BudgetCharsOverride = 8000
			cfg.Compressor = &contextPublicationCompressor{status: status}
			first := callPublicationContext(t, cfg, "file_context", "shared")
			require.Contains(t, first.Text, "compressed overview")
			for _, key := range []string{"note:CONTEXT.md", "code:main.go"} {
				_, seen, err := store.SessionItemFingerprint(context.Background(), "shared", key)
				require.NoError(t, err)
				require.False(t, seen, "compression status cannot establish exact raw delivery")
			}
			cfg.Compressor = nil
			cfg.BudgetCharsOverride = 16000
			second := callPublicationContext(t, cfg, "file_context", "shared")
			require.Contains(t, second.Text, "UNIQUE_NESTED_FINAL_EVIDENCE")
			third := callPublicationContext(t, cfg, "file_context", "shared")
			require.NotContains(t, third.Text, "UNIQUE_NESTED_FINAL_EVIDENCE")
		})
	}
}

func TestContextSessionPublicationCompressionFallbackUsesRawEvidence(t *testing.T) {
	cfg, store, doc := contextPublicationFixture(t)
	cfg.BudgetCharsOverride = 8000
	cfg.Compressor = &contextPublicationCompressor{err: errors.New("synthetic compressor unavailable")}
	first := callPublicationContext(t, cfg, "file_context", "shared")
	require.Contains(t, first.Text, "UNIQUE_NESTED_FINAL_EVIDENCE")
	fp, seen, err := store.SessionItemFingerprint(context.Background(), "shared", "note:CONTEXT.md")
	require.NoError(t, err)
	require.True(t, seen)
	require.Equal(t, fingerprintText(strings.TrimSpace(doc)), fp)
	second := callPublicationContext(t, cfg, "file_context", "shared")
	require.NotContains(t, second.Text, "UNIQUE_NESTED_FINAL_EVIDENCE")
}

func TestContextSessionPublicationCancellationReleasesRawRetry(t *testing.T) {
	for _, tool := range []string{"file_context", "vault_context"} {
		t.Run(tool, func(t *testing.T) {
			cfg, store, _ := contextPublicationFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			compressor := &contextPublicationCompressor{cancel: cancel}
			cfg.Compressor = compressor
			cfg.BudgetCharsOverride = 8000
			handler := FileContextTool(cfg)
			if tool == "vault_context" {
				handler = VaultContextTool(cfg)
			}
			result, err := handler(ctx, protocol.CallToolRequest{Params: protocol.CallToolParams{
				Name: tool, Arguments: map[string]any{"files": []any{"main.go"}, "profile": "code", "sessionId": "shared"},
			}})
			require.NoError(t, err)
			require.True(t, result.IsError)
			require.NotNil(t, compressor.captured)
			require.ErrorIs(t, compressor.captured.Err(), context.Canceled)
			_, seen, err := store.SessionItemFingerprint(context.Background(), "shared", "note:CONTEXT.md")
			require.NoError(t, err)
			require.False(t, seen)
			cfg.Compressor = nil
			retry := callPublicationContext(t, cfg, tool, "shared")
			require.Contains(t, retry.Text, "UNIQUE_NESTED_FINAL_EVIDENCE")
		})
	}
}

func TestContextSessionPublicationFailedPersistenceReleasesRawRetry(t *testing.T) {
	cfg, store, _ := contextPublicationFixture(t)
	cfg.BudgetCharsOverride = 8000
	failing := &failCommitSessionStore{Store: store, commitErr: errors.New("synthetic commit failure")}
	cfg.SessionStore = failing
	first := callPublicationContext(t, cfg, "file_context", "shared")
	require.Contains(t, first.Text, "UNIQUE_NESTED_FINAL_EVIDENCE")
	require.True(t, failing.releaseHasDeadline)
	require.NoError(t, failing.releaseErrAtCall)
	_, seen, err := store.SessionItemFingerprint(context.Background(), "shared", "note:CONTEXT.md")
	require.NoError(t, err)
	require.False(t, seen)
	cfg.SessionStore = store
	second := callPublicationContext(t, cfg, "file_context", "shared")
	require.Contains(t, second.Text, "UNIQUE_NESTED_FINAL_EVIDENCE")
}

func TestContextSessionPublicationIndexedWarningRetainsDeliveryEvidence(t *testing.T) {
	for _, budget := range []int{1, 4, 1000, 1400, 1800, 8000} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			cfg, store, doc := contextPublicationFixture(t)
			cfg.IndexedReadOnlyFileContext = true
			cfg.IndexedContextUnavailable = &actions.IndexedContextFreshness{
				State: actions.IndexedContextMissing, WarningCode: "indexed-context-missing", Remediation: "rzm index",
			}
			cfg.BudgetCharsOverride = budget
			first := callPublicationContext(t, cfg, "file_context", "shared")
			require.LessOrEqual(t, len(first.Text), budget)
			require.True(t, utf8.ValidString(first.Text))
			fp, seen, err := store.SessionItemFingerprint(context.Background(), "shared", "note:CONTEXT.md")
			require.NoError(t, err)
			fullSeen := seen && fp == fingerprintText(strings.TrimSpace(doc))
			if budget == 8000 {
				require.Contains(t, first.Text, "indexed-context-missing")
				require.Contains(t, first.Text, "UNIQUE_NESTED_FINAL_EVIDENCE")
				require.True(t, fullSeen, "an appended warning must preserve complete source delivery")
			}
			if !strings.Contains(first.Text, "UNIQUE_NESTED_FINAL_EVIDENCE") {
				require.False(t, fullSeen, "warning trimming cannot publish an undelivered source")
			}
			cfg.BudgetCharsOverride = 8000
			retry := callPublicationContext(t, cfg, "file_context", "shared")
			if fullSeen {
				require.NotContains(t, retry.Text, "UNIQUE_NESTED_FINAL_EVIDENCE")
			} else {
				require.Contains(t, retry.Text, "UNIQUE_NESTED_FINAL_EVIDENCE")
			}
		})
	}
}

func TestContextSessionPublicationNestedNoteRetry(t *testing.T) {
	for _, budget := range []int{1000, 1800, 8000} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			cfg, store, _ := contextPublicationFixture(t)
			root := cfg.VaultPath
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome/ontology"), 0o755))
			require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/ontology/schema.graphql"), []byte(`
type Host @node(paths: ["notes/host.md"]) {
  details: Detail @link(source: "details", contextInclude: true)
}
type Detail @node(paths: ["notes/details.md"]) {
  title: String
}
`), 0o644))
			doc := "# Details\n\n" + strings.Repeat("nested note evidence ", 40) + "\nUNIQUE_NOTE_FINAL_EVIDENCE\n"
			require.NoError(t, os.WriteFile(filepath.Join(root, "notes/details.md"), []byte(doc), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(root, "notes/host.md"), []byte("---\ndetails: notes/details.md\n---\n\n# Host\n"), 0o644))
			call := func(session string) ContextTextResponse {
				response, err := FileContextTool(cfg)(context.Background(), protocol.CallToolRequest{Params: protocol.CallToolParams{
					Name: "file_context", Arguments: map[string]any{"files": []any{"notes/host.md"}, "profile": "vault", "sessionId": session},
				}})
				require.NoError(t, err)
				require.False(t, response.IsError, "%+v", response.Content)
				var out ContextTextResponse
				require.NoError(t, json.Unmarshal([]byte(response.Content[0].(protocol.TextContent).Text), &out))
				return out
			}
			cfg.BudgetCharsOverride = budget
			first := call("shared")
			fp, seen, err := store.SessionItemFingerprint(context.Background(), "shared", "note:notes/details.md")
			require.NoError(t, err)
			fullSeen := seen && fp == fingerprintText(strings.TrimSpace(doc))
			if budget == 8000 {
				require.True(t, fullSeen)
			}
			if !strings.Contains(first.Text, "UNIQUE_NOTE_FINAL_EVIDENCE") {
				require.False(t, fullSeen)
			}
			cfg.BudgetCharsOverride = 8000
			second, fresh := call("shared"), call("fresh")
			require.Contains(t, fresh.Text, "UNIQUE_NOTE_FINAL_EVIDENCE")
			if fullSeen {
				require.NotContains(t, second.Text, "UNIQUE_NOTE_FINAL_EVIDENCE")
			} else {
				require.Contains(t, second.Text, "UNIQUE_NOTE_FINAL_EVIDENCE")
			}
		})
	}
}

func callPublicationContext(t *testing.T, cfg Config, tool, session string) ContextTextResponse {
	t.Helper()
	handler := FileContextTool(cfg)
	if tool == "vault_context" {
		handler = VaultContextTool(cfg)
	}
	result, err := handler(context.Background(), protocol.CallToolRequest{Params: protocol.CallToolParams{
		Name: tool, Arguments: map[string]any{"files": []any{"main.go"}, "profile": "code", "sessionId": session},
	}})
	require.NoError(t, err)
	require.False(t, result.IsError, "%+v", result.Content)
	var out ContextTextResponse
	require.NoError(t, json.Unmarshal([]byte(result.Content[0].(protocol.TextContent).Text), &out))
	return out
}

func TestContextSessionPublicationRetainsUndeliveredNestedTail(t *testing.T) {
	for _, tool := range []string{"file_context", "vault_context"} {
		for _, budget := range []int{1, 4, 10, 70, 180, 800, 1000, 1200, 1400, 1800, 2400, 3400, 4000, 4400, 5000, 6000, 8000} {
			t.Run(fmt.Sprintf("%s/%d", tool, budget), func(t *testing.T) {
				cfg, store, doc := contextPublicationFixture(t)
				cfg.BudgetCharsOverride = budget
				first := callPublicationContext(t, cfg, tool, "shared")
				require.LessOrEqual(t, len(first.Text), budget)
				require.True(t, utf8.ValidString(first.Text))
				fp, seen, err := store.SessionItemFingerprint(context.Background(), "shared", "note:CONTEXT.md")
				require.NoError(t, err)
				fullSeen := seen && fp == fingerprintText(strings.TrimSpace(doc))
				if budget == 8000 {
					require.Contains(t, first.Text, "UNIQUE_NESTED_FINAL_EVIDENCE")
					require.True(t, fullSeen, "fully delivered nested documents still dedupe")
				}
				if !strings.Contains(first.Text, "UNIQUE_NESTED_FINAL_EVIDENCE") {
					require.False(t, fullSeen, "a clipped container cannot publish its full nested fingerprint")
				}
				cfg.BudgetCharsOverride = 8000
				second := callPublicationContext(t, cfg, tool, "shared")
				fresh := callPublicationContext(t, cfg, tool, "fresh")
				require.Contains(t, fresh.Text, "UNIQUE_NESTED_FINAL_EVIDENCE")
				if fullSeen {
					require.NotContains(t, second.Text, "UNIQUE_NESTED_FINAL_EVIDENCE")
				} else {
					require.Contains(t, second.Text, "UNIQUE_NESTED_FINAL_EVIDENCE", "a larger retry must recover undelivered context")
				}
			})
		}
	}
}
