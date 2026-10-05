package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
)

// projectedFactsNoteReader carries provider-projected note facts into neutral
// CLI renderers. The wrapped reader remains available for operations that own
// their own source semantics, but neutral readers cannot parse its bytes.
type projectedFactsNoteReader struct {
	obsidian.NoteReader
	facts actions.NoteFacts
}

func (r projectedFactsNoteReader) NoteFacts() actions.NoteFacts { return r.facts }

// withProjectedNoteFacts adds current persisted projection facts when the
// live runtime has published them. Cold or stale metadata deliberately yields
// no facts; handlers omit syntax-derived detail instead of parsing Markdown.
func withProjectedNoteFacts(ctx context.Context, config Config, reader obsidian.NoteReader) obsidian.NoteReader {
	store := config.GetIntelStore()
	if reader == nil || store == nil || config.NoteMetadata.Validate() != nil {
		return reader
	}
	sources, err := config.NoteMetadata.LoadNoteSourceSnapshots(ctx, config.VaultDef, reader, store, nil)
	if err != nil {
		return reader
	}
	return projectedFactsNoteReader{NoteReader: reader, facts: actions.NewNoteFacts(sources)}
}

const (
	fileTypeNote = "note"
	fileTypeCode = "code"

	contentOmittedBudget    = "budget"
	contentOmittedDeduped   = "deduped"
	contentOmittedReadError = "read_error"
)

func titleFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func normalizeDocPath(vaultPaths paths.VaultPaths, path string) string {
	if vaultPaths.Root() == "" {
		rel, err := paths.CleanRelPath(path)
		if err != nil {
			return ""
		}
		return rel.String()
	}
	rel, err := vaultPaths.RelStrict(path)
	if err != nil {
		return ""
	}
	return rel.String()
}

// resolveSemanticConnectionNotePath canonicalizes a caller-supplied graph
// endpoint without deciding whether its extension is Markdown, code, or a
// descriptor-only note format. Ownership comes from the graph/index result.
func resolveSemanticConnectionNotePath(vaultPath, raw string) (string, error) {
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil {
		return "", err
	}
	if vaultPaths.Root() != "" {
		rel, err := vaultPaths.RelStrict(raw)
		if err != nil {
			return "", err
		}
		return rel.String(), nil
	}
	rel, err := paths.CleanRelPath(raw)
	if err != nil {
		return "", err
	}
	return rel.String(), nil
}

// supportsMCPProjectedNoteFileSurface reports whether the configured runtime
// has an executable, source-readable provider for this note path. Ownership is
// intentionally separate: descriptor-only notes stay note-owned but cannot
// enter this content surface until their provider supports it.
func supportsMCPProjectedNoteFileSurface(config Config, path string) bool {
	runtime, err := config.NoteMetadata.FormatRuntime()
	if err != nil {
		return false
	}
	provider, ok := runtime.ProviderForPath(paths.RelPath(path))
	if !ok {
		return false
	}
	descriptor := provider.Descriptor()
	return runtime.CanProject(descriptor.ID) && descriptor.Capabilities.Has(noteformat.CapabilitySourceReading)
}

func resolveNoteReader(config Config) obsidian.NoteReader {
	if config.NoteReader != nil {
		return config.NoteReader
	}
	if config.Cache != nil {
		return cache.NewNoteAdapter(config.Cache, &obsidian.Note{})
	}
	return &obsidian.Note{}
}

func respondJSON(payload any, marshalErr string) (*mcp.CallToolResult, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", marshalErr, err)), nil
	}
	return mcp.NewToolResultText(string(encoded)), nil
}

// managedIndexUnavailableResult is the stable error envelope for agent
// one-shot plans that own a single existing read-only index. Long-lived MCP
// callers keep their historical fallback-open behavior and never take this
// path.
func managedIndexUnavailableResult(config Config, operation string) *mcp.CallToolResult {
	freshness := actions.IndexedContextFreshness{
		State:       actions.IndexedContextMissing,
		WarningCode: "indexed-context-missing",
		Remediation: "rzm index",
	}
	if config.IndexedContextUnavailable != nil {
		freshness = *config.IndexedContextUnavailable
	}
	encoded, err := json.Marshal(map[string]string{
		"code":        freshness.WarningCode,
		"message":     operation + " requires the managed indexed read model",
		"remediation": freshness.Remediation,
	})
	if err != nil {
		return mcp.NewToolResultError("managed indexed read model is unavailable")
	}
	return mcp.NewToolResultError(string(encoded))
}

func managedIndexUnavailable(config Config) bool {
	return config.IntelStorePolicy == IntelStoreManagedReadOnly && config.GetIntelStore() == nil
}

// metadataStoreFallbackPolicy keeps planned one-shot reads from creating an
// action-owned store when their managed reader is unavailable. The default
// preserves fallback opens for long-lived MCP callers.
func metadataStoreFallbackPolicy(config Config) actions.MetadataStoreFallbackPolicy {
	if config.IntelStorePolicy == IntelStoreFallbackAllowed {
		return actions.MetadataStoreFallbackOpen
	}
	return actions.MetadataStoreFallbackLive
}

func respondContextText(sessionID string, tracker *sessionTracker, text string) (*mcp.CallToolResult, error) {
	return respondContextTextResult(sessionID, tracker, actions.VaultContextTextResult{Text: text}, false)
}

func respondContextTextResult(sessionID string, tracker *sessionTracker, result actions.VaultContextTextResult, indexed bool) (*mcp.CallToolResult, error) {
	payload := ContextTextResponse{
		SessionID: sessionID,
		Text:      result.Text,
	}
	if indexed {
		payload.IndexedEnrichment = &IndexedEnrichmentResponse{
			Status: result.IndexedStatus, Reads: result.IndexedReads, Results: result.IndexedResults, Warnings: result.Warnings,
		}
	}
	if tracker != nil {
		payload.DedupeHits = tracker.DedupeHits()
	}
	return respondJSON(payload, "marshal failed")
}

func codeRefsByFile(config Config) map[string][]coderefs.CodeRef {
	if config.Cache == nil {
		return map[string][]coderefs.CodeRef{}
	}
	if refs := config.Cache.CodeRefsByFile(); len(refs) > 0 {
		return refs
	}
	return map[string][]coderefs.CodeRef{}
}

func codeRefsByNote(config Config) map[string][]coderefs.CodeRef {
	if config.Cache == nil {
		return map[string][]coderefs.CodeRef{}
	}
	if refs := config.Cache.CodeRefsByNote(); len(refs) > 0 {
		return refs
	}
	return map[string][]coderefs.CodeRef{}
}

// parseStringArray ensures a JSON array of strings, returning the slice or an error message.
func parseStringArray(raw interface{}, field string) ([]string, string) {
	items, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Sprintf("%s parameter is required and must be an array", field)
	}

	out := make([]string, len(items))
	for i, v := range items {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Sprintf("all %s items must be strings", field)
		}
		out[i] = s
	}
	return out, ""
}

func parseMatchPatterns(raw interface{}) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("match must be an array of strings")
	}
	out := make([]string, 0, len(items))
	for _, v := range items {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("match must be an array of strings")
		}
		out = append(out, s)
	}
	return out, nil
}

func parseAnchorKinds(raw interface{}) ([]codeanchor.AnchorKind, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("anchorKinds must be an array of strings")
	}
	out := make([]codeanchor.AnchorKind, 0, len(items))
	for _, v := range items {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("anchorKinds must be an array of strings")
		}
		kind := strings.TrimSpace(strings.ToLower(s))
		if kind == "" {
			continue
		}
		switch kind {
		case "function", "func", "symbol":
			out = append(out, codeanchor.AnchorFunc)
		case "annotation", "decorator":
			out = append(out, codeanchor.AnchorAnnotation)
		case "baseclass", "base_class", "base":
			out = append(out, codeanchor.AnchorBaseClass)
		case "path", "dir", "directory":
			out = append(out, codeanchor.AnchorPath)
		case "glob":
			out = append(out, codeanchor.AnchorGlob)
		default:
			return nil, fmt.Errorf("invalid anchorKinds value %q (use function, annotation, baseClass, path, glob)", s)
		}
	}
	return out, nil
}

func extractStringArray(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return []string{v}
	default:
		return nil
	}
}

func normalizeReportPaths(vaultPath string, raw []string) []string {
	seen := map[string]struct{}{}
	var out []string
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if err == nil && vaultPaths.Root() != "" {
			ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, p)
			if err != nil || ref.Rel == "" {
				continue
			}
			candidate := ref.Rel.String()
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			out = append(out, candidate)
			continue
		}
		rel, err := paths.CleanRelPath(p)
		if err != nil || rel == "" {
			continue
		}
		candidate := string(paths.NormalizeCode(rel.String()))
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func relCodePath(vaultPaths paths.VaultPaths, p string) string {
	if p == "" {
		return ""
	}
	if vaultPaths.Root() != "" {
		if ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, p); err == nil && ref.Rel != "" {
			return ref.Rel.String()
		}
	}
	if rel, err := paths.CleanRelPath(p); err == nil && rel.String() != "" {
		return string(paths.NormalizeCode(rel.String()))
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func buildCodeLinkPayloads(refs []coderefs.CodeRef, includeSnippet bool) []CodeLink {
	if len(refs) == 0 {
		return nil
	}
	out := make([]CodeLink, 0, len(refs))
	for _, ref := range refs {
		payload := CodeLink{
			File:   ref.SourceFile,
			Line:   ref.Line,
			Source: string(ref.Kind),
		}
		if includeSnippet {
			payload.Snippet = ref.Snippet
		}
		out = append(out, payload)
	}
	return out
}
