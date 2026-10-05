package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
)

// NodeLinkTool implements the node_link MCP tool.
func NodeLinkTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		ensure, err := parseNodeLinkEnsure(args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if ensure == ontology.EnsureLinkTargetApply && !config.ReadWrite {
			encoded, _ := json.Marshal(map[string]string{
				"code":    "apply_requires_read_write",
				"message": "node_link ensure=apply requires MCP read-write mode",
			})
			return mcp.NewToolResultError(string(encoded)), nil
		}
		refs, err := nodeLinkRefsFromArgs(args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(refs) == 0 {
			return mcp.NewToolResultError("provide refs or targets"), nil
		}
		if err := validateNodeLinkRefFormats(refs, config); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		schema, err := ontology.LoadSchema(config.VaultPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("node_link: %v", err)), nil
		}
		reader := resolveNoteReader(config)
		if ensure == ontology.EnsureLinkTargetApply {
			reader = &obsidian.Note{}
			embedded := make([]ontology.NodeRef, 0, len(refs))
			for _, ref := range refs {
				if ref.Kind == ontology.NodeKindEmbedded {
					embedded = append(embedded, ref)
				}
			}
			if len(embedded) > 0 {
				if config.ApplyLinkTargets == nil {
					return mcp.NewToolResultError("node_link apply requires an explicit live writer"), nil
				}
				_, err := config.ApplyLinkTargets(ctx, schema.Hash, ontology.LinkTargetRequest{Refs: embedded, Ensure: ensure, Purpose: "node_link"})
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("node_link: %v", err)), nil
				}
			}
		}
		readEnsure := ensure
		if readEnsure == ontology.EnsureLinkTargetApply {
			readEnsure = ontology.EnsureLinkTargetNever
		}
		result, err := (&ontology.NodeLinkService{
			VaultDef:   config.VaultDef,
			NoteReader: reader,
			Schema:     schema,
		}).Locators(ctx, ontology.LinkTargetRequest{
			Refs:    refs,
			Ensure:  readEnsure,
			Purpose: "node_link",
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("node_link: %v", err)), nil
		}
		return respondJSON(map[string]any{
			"ensure":   string(ensure),
			"locators": result,
		}, "marshal node_link result")
	}
}

func parseNodeLinkEnsure(args map[string]any) (ontology.EnsureLinkTargetMode, error) {
	raw, _ := args["ensure"].(string)
	switch strings.TrimSpace(raw) {
	case "", string(ontology.EnsureLinkTargetNever):
		return ontology.EnsureLinkTargetNever, nil
	case string(ontology.EnsureLinkTargetPlan):
		return ontology.EnsureLinkTargetPlan, nil
	case string(ontology.EnsureLinkTargetApply):
		return ontology.EnsureLinkTargetApply, nil
	default:
		return ontology.EnsureLinkTargetNever, fmt.Errorf("invalid ensure value %q (expected never, plan, or apply)", raw)
	}
}

func nodeLinkRefsFromArgs(args map[string]any) ([]ontology.NodeRef, error) {
	refs := make([]ontology.NodeRef, 0)
	if rawRefs, ok := args["refs"]; ok {
		parsed, err := parseNodeLinkRefValues(rawRefs)
		if err != nil {
			return nil, err
		}
		refs = append(refs, parsed...)
	}
	if rawTargets, ok := args["targets"]; ok {
		targets, err := stringArrayArgValue(rawTargets, "targets")
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			ref, err := nodeRefFromLinkTarget(target)
			if err != nil {
				return nil, err
			}
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

func parseNodeLinkRefValues(raw any) ([]ontology.NodeRef, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("refs must be an array of NodeRef objects or JSON strings")
	}
	refs := make([]ontology.NodeRef, 0, len(items))
	for _, item := range items {
		var ref ontology.NodeRef
		switch v := item.(type) {
		case map[string]any:
			data, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(data, &ref); err != nil {
				return nil, err
			}
		case string:
			input := strings.TrimSpace(v)
			if strings.HasPrefix(input, "{") {
				if err := json.Unmarshal([]byte(input), &ref); err != nil {
					return nil, err
				}
			} else {
				parsed, err := nodeRefFromLinkTarget(input)
				if err != nil {
					return nil, err
				}
				ref = parsed
			}
		default:
			return nil, fmt.Errorf("refs entries must be NodeRef objects or strings")
		}
		if ref.IsZero() {
			return nil, fmt.Errorf("refs entries require notePath")
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func stringArrayArgValue(raw any, name string) ([]string, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", name)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s entries must be strings", name)
		}
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out, nil
}

func nodeRefFromLinkTarget(input string) (ontology.NodeRef, error) {
	target := unwrapNodeLinkTarget(input)
	if target == "" {
		return ontology.NodeRef{}, fmt.Errorf("empty link target")
	}
	notePath, fragment := splitNodeLinkTarget(target)
	if strings.TrimSpace(notePath) == "" {
		return ontology.NodeRef{}, fmt.Errorf("target %q is missing a note path", input)
	}
	normalized, err := normalizeMarkdownNodeLinkTargetCompat(notePath)
	if err != nil {
		return ontology.NodeRef{}, fmt.Errorf("target %q has an invalid note path: %w", input, err)
	}
	ref := ontology.NodeRef{NotePath: normalized, Kind: ontology.NodeKindNote}
	if fragment != "" {
		ref.Fragment = strings.TrimPrefix(fragment, "#")
		ref.Kind = ontology.NodeKindEmbedded
	}
	return ref, nil
}

// normalizeMarkdownNodeLinkTargetCompat preserves the legacy target grammar
// for node_link strings. Generic NodeRef inputs keep their authored paths.
func normalizeMarkdownNodeLinkTargetCompat(path string) (string, error) {
	normalized, err := paths.CleanNotePath(path)
	if err != nil {
		return "", err
	}
	if filepath.Ext(normalized.String()) == "" {
		normalized = paths.NormalizeNote(normalized.String())
	}
	return normalized.String(), nil
}

func validateNodeLinkRefFormats(refs []ontology.NodeRef, config Config) error {
	runtime, err := config.NoteMetadata.FormatRuntime()
	if err != nil {
		// Older direct callers did not compose note metadata. Preserve their
		// Markdown-compatible behavior; production MCP composition supplies it.
		return nil
	}
	for _, ref := range refs {
		path, err := paths.CleanNotePath(ref.NotePath)
		if err != nil {
			return fmt.Errorf("node_link: invalid note path %q", ref.NotePath)
		}
		provider, ok := runtime.ProviderForPath(paths.RelPath(path))
		if !ok || !runtime.CanProject(provider.Descriptor().ID) {
			return fmt.Errorf("node_link: note format for %q does not support link locators", ref.NotePath)
		}
	}
	return nil
}

func unwrapNodeLinkTarget(input string) string {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "[[") && strings.HasSuffix(input, "]]") {
		input = strings.TrimSuffix(strings.TrimPrefix(input, "[["), "]]")
		if idx := strings.Index(input, "|"); idx >= 0 {
			input = input[:idx]
		}
	}
	return strings.TrimSpace(input)
}

func splitNodeLinkTarget(target string) (string, string) {
	if idx := strings.LastIndex(target, "#"); idx > 0 {
		return strings.TrimSpace(target[:idx]), strings.TrimSpace(target[idx+1:])
	}
	return strings.TrimSpace(target), ""
}
