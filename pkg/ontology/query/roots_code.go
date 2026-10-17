package query

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/vektah/gqlparser/v2/ast"
)

func (e *executor) rootCode(ctx context.Context, ref ontology.NodeRef) (*codeRecord, error) {
	codePath, _, err := paths.ResolveCodeInputWithVaultPaths(e.loaders.vaultPaths, ref.NotePath)
	if err != nil {
		return nil, err
	}
	abs, err := e.loaders.vaultPaths.AbsCode(codePath)
	if err != nil {
		return nil, err
	}
	exists := false
	if info, err := os.Stat(abs.String()); err == nil {
		exists = !info.IsDir()
	} else {
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	anchors, err := e.deps.Store.IntelAnchorsByPath(ctx, codePath.String())
	if err != nil {
		return nil, err
	}
	if !exists && len(anchors) == 0 {
		return nil, nil
	}
	record := &codeRecord{Path: codePath.String(), Title: filepath.Base(codePath.String()), Symbols: anchors}
	if len(anchors) > 0 {
		record.Lang = string(anchors[0].Lang)
	}
	if ref.NodeID != "" || ref.Fragment != "" {
		wanted := firstNonEmpty(strings.TrimPrefix(ref.Fragment, "symbol:"), ref.NodeID)
		for i := range anchors {
			anchor := anchors[i]
			if anchor.AnchorID == wanted || anchor.FQN == wanted || anchor.Symbol == wanted {
				record.Anchor = &anchor
				record.Title = firstNonEmpty(anchor.Symbol, anchor.FQN, anchor.AnchorID)
				record.Lang = string(anchor.Lang)
				break
			}
		}
		if ref.Kind == "CODE_SYMBOL" && record.Anchor == nil {
			return nil, nil
		}
	}
	return record, nil
}

func (e *executor) resolveCodeSelectionSet(ctx context.Context, node *codeRecord, set ast.SelectionSet, path []string) map[string]any {
	return e.resolveSelections(set, path, "unsupported selection on Node", func(condition string) bool {
		return codeMatchesTypeCondition(node, condition)
	}, func(field *ast.Field, fieldPath []string) (any, bool) {
		return e.resolveCodeField(ctx, node, field, fieldPath)
	})
}

func (e *executor) resolveCodeField(ctx context.Context, node *codeRecord, field *ast.Field, path []string) (any, bool) {
	if node == nil {
		return nil, true
	}
	ref := codeNodeRef(node)
	switch field.Name {
	case "ref":
		return nodeRefGraphQLValue(ref), true
	case "nodeId":
		if node.Anchor != nil {
			return "code-symbol:" + node.Anchor.AnchorID, true
		}
		return "code-file:" + node.Path, true
	case "nodeKind":
		if node.Anchor != nil {
			return "CODE_SYMBOL", true
		}
		return "CODE_FILE", true
	case "path":
		return node.Path, true
	case "title":
		return node.Title, true
	case "resolvedType":
		if node.Anchor != nil {
			return "CodeSymbol", true
		}
		return "CodeFile", true
	case "locator":
		return locatorGraphQLValue(ontology.NodeLocator{
			Ref:           ref,
			Kind:          ref.Kind,
			SourceLocator: queryLocatorKey(ref),
			Status:        ontology.NodeLocatorLinkable,
			LinkTarget: &ontology.NodeLinkTarget{
				Ref:          ref,
				Markdown:     node.Path,
				DisplayLabel: node.Title,
				Exists:       true,
				RequiresFix:  false,
			},
		}), true
	case "neighborhood":
		return e.emptyNeighborhoodSelectionSet(field.SelectionSet, path), true
	case "localGraph":
		return e.emptyLocalGraphSelectionSet(field.SelectionSet, path), true
	case "workspace":
		return nil, true
	case "language":
		if node.Anchor != nil {
			return string(node.Anchor.Lang), true
		}
		return node.Lang, true
	case "symbol":
		if node.Anchor == nil {
			return nil, true
		}
		return node.Anchor.Symbol, true
	case "fqn":
		if node.Anchor == nil {
			return nil, true
		}
		return node.Anchor.FQN, true
	case "signature":
		if node.Anchor == nil {
			return nil, true
		}
		return node.Anchor.Signature, true
	case "docComment":
		if node.Anchor == nil {
			return nil, true
		}
		return node.Anchor.DocComment, true
	case "symbols":
		first := 50
		if args := e.fieldArgs(field); args != nil {
			first = intValue(args["first"], 50)
		}
		symbols := node.Symbols
		if first > 0 && len(symbols) > first {
			symbols = symbols[:first]
		}
		out := make([]any, 0, len(symbols))
		for i := range symbols {
			anchor := symbols[i]
			out = append(out, e.resolveCodeSelectionSet(ctx, &codeRecord{
				Path:   node.Path,
				Title:  firstNonEmpty(anchor.Symbol, anchor.FQN, anchor.AnchorID),
				Lang:   string(anchor.Lang),
				Anchor: &anchor,
			}, field.SelectionSet, path))
		}
		return out, true
	default:
		e.addError(path, fmt.Sprintf("field %q does not exist on code node", field.Name))
		return nil, false
	}
}

func (e *executor) resolveRefSelectionSet(ctx context.Context, input string, set ast.SelectionSet, path []string) map[string]any {
	resolution := e.resolveRef(ctx, input)
	out := make(map[string]any)
	for _, selection := range set {
		field, ok := selection.(*ast.Field)
		if !ok {
			continue
		}
		key := responseKey(field)
		switch field.Name {
		case "input":
			out[key] = resolution.Input
		case "found":
			out[key] = resolution.Found
		case "ref":
			if resolution.Ref == nil {
				out[key] = nil
			} else {
				out[key] = nodeRefGraphQLValue(*resolution.Ref)
			}
		case "candidates":
			candidates := make([]any, 0, len(resolution.Candidates))
			for _, candidate := range resolution.Candidates {
				candidates = append(candidates, nodeRefGraphQLValue(candidate))
			}
			out[key] = candidates
		case "kind":
			if resolution.Ref == nil {
				out[key] = nil
			} else {
				out[key] = graphQLNodeKind(resolution.Ref.Kind)
			}
		case "title":
			out[key] = resolution.Title
		case "path":
			out[key] = resolution.Path
		case "resolvedType":
			out[key] = resolution.ResolvedType
		case "reason":
			out[key] = resolution.Reason
		default:
			e.addError(append(path, key), fmt.Sprintf("field %q does not exist on NodeRefResolution", field.Name))
		}
	}
	return out
}

type refResolution struct {
	Input        string
	Found        bool
	Ref          *ontology.NodeRef
	Candidates   []ontology.NodeRef
	Title        string
	Path         string
	ResolvedType string
	Reason       string
}

func (e *executor) resolveRef(ctx context.Context, input string) refResolution {
	input = strings.TrimSpace(input)
	res := refResolution{Input: input}
	if input == "" {
		res.Reason = "empty_input"
		return res
	}
	if ref, candidates, ok, err := e.resolveNodeRefTargetCandidates(ctx, input); err != nil {
		res.Reason = err.Error()
		return res
	} else if len(candidates) > 1 {
		res.Candidates = candidates
		res.Reason = "ambiguous"
		return res
	} else if ok {
		node, err := e.rootNodeFromRef(ctx, ref)
		if err != nil || node == nil {
			res.Reason = "unresolved"
			return res
		}
		e.populateRefResolutionFromNode(ctx, &res, node)
		return res
	}
	node, err := e.rootNode(ctx, input)
	if err != nil || node == nil {
		res.Reason = "unresolved"
		return res
	}
	e.populateRefResolutionFromNode(ctx, &res, node)
	return res
}

func (e *executor) populateRefResolutionFromNode(ctx context.Context, res *refResolution, node *resolvedNode) {
	if res == nil || node == nil {
		return
	}
	switch {
	case node.Note != nil:
		outRef := ontology.NodeRef{NotePath: node.Note.Path, Kind: ontology.NodeKindNote, TypeName: node.Note.TypeName}
		res.Ref = &outRef
		res.Found = true
		res.Title = node.Note.Title
		res.Path = node.Note.Path
		res.ResolvedType = node.Note.TypeName
	case node.Section != nil:
		if node.Section.Node == nil {
			res.Reason = "unresolved"
			return
		}
		outRef, err := e.publicSectionRef(ctx, node.Section)
		if err != nil {
			res.Reason = err.Error()
			return
		}
		res.Ref = &outRef
		res.Found = true
		res.Title = node.Section.Node.Title
		res.Path = outRef.String()
		res.ResolvedType = node.Section.TypeName
	case node.Code != nil:
		outRef := codeNodeRef(node.Code)
		res.Ref = &outRef
		res.Found = true
		res.Title = node.Code.Title
		res.Path = node.Code.Path
		if node.Code.Anchor != nil {
			res.ResolvedType = "CodeSymbol"
		} else {
			res.ResolvedType = "CodeFile"
		}
	}
}
