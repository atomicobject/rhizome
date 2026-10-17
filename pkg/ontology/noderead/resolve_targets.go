package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"path/filepath"
	"strings"
)

type parsedResolveTarget struct {
	Input         string
	SourceLocator string
	NotePath      string
	Fragment      string
	Ref           ontology.NodeRef
}

type inputRef struct {
	Input string
	Ref   ontology.NodeRef
}

func (s *Scope) parseResolveTargets(ctx context.Context, req ResolveRequest) ([]parsedResolveTarget, []NodeResolveDiagnostic, error) {
	targets := make([]parsedResolveTarget, 0, len(req.Targets))
	diagnostics := make([]NodeResolveDiagnostic, 0)
	var cache *obsidian.NotePathCache
	var cacheErr error
	notePathCache := func() *obsidian.NotePathCache {
		if cache == nil {
			cache, cacheErr = s.notePathCache(ctx)
		}
		return cache
	}
	for _, rawTarget := range req.Targets {
		input := strings.TrimSpace(rawTarget.Input)
		if input == "" {
			continue
		}
		if ref, ok := parseNodeRefJSON(input); ok {
			targets = append(targets, parsedResolveTarget{Input: input, Ref: ref})
			continue
		}
		target := unwrapResolveTarget(input)
		// Authored wikilinks still need inventory resolution for omitted
		// extensions and relative spellings before any canonical-path reads.
		isWikilink := strings.HasPrefix(input, "[[") && strings.HasSuffix(input, "]]")
		if !isLikelyMarkdownLink(input) && !isWikilink {
			if parsed, ok := explicitResolveTarget(input, target); ok {
				targets = append(targets, parsed)
				continue
			}
		}
		relativeWiki := isWikilink && (strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../"))
		if relativeWiki {
			notePath, fragment := splitLocatorTarget(target)
			fromPath, _ := splitLocatorTarget(req.FromPath)
			notePath = filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(fromPath), notePath)))
			target = notePath
			if fragment != "" {
				target += "#" + fragment
			}
		}
		var resolved obsidian.ResolvedNoteTarget
		var ok bool
		var candidates []obsidian.ResolvedNoteTarget
		isMarkdown := isLikelyMarkdownLink(input)
		if cache := notePathCache(); cache != nil && isMarkdown {
			candidates = cache.ResolveMdLinkCandidates(target, req.FromPath)
		} else if cache != nil {
			candidates = cache.ResolveNoteCandidates(target)
		}
		if isMarkdown && len(candidates) == 0 {
			diagnostics = append(diagnostics, NodeResolveDiagnostic{Input: input, Code: "target_unresolved", Message: "Markdown link target did not resolve to a note"})
			continue
		}
		if relativeWiki {
			notePath, _ := splitLocatorTarget(target)
			exact := candidates[:0]
			for _, candidate := range candidates {
				if candidate.Path == notePath || strings.TrimSuffix(candidate.Path, filepath.Ext(candidate.Path)) == notePath {
					exact = append(exact, candidate)
				}
			}
			candidates = exact
			if len(candidates) == 0 || req.FromPath == "" {
				diagnostics = append(diagnostics, NodeResolveDiagnostic{Input: input, Code: "target_unresolved", Message: "relative link target did not resolve from its source note"})
				continue
			}
		}
		if !ok && len(candidates) == 1 {
			resolved, ok = candidates[0], true
		}
		if len(candidates) > 1 {
			canonical, err := s.canonicalCandidateRefs(ctx, candidates)
			if err != nil {
				return nil, nil, err
			}
			diagnostics = append(diagnostics, NodeResolveDiagnostic{
				Input:      input,
				Code:       "target_ambiguous",
				Message:    "link target resolves to multiple notes",
				Candidates: canonical,
			})
			continue
		}
		identifierTarget, identifierFragment := splitLocatorTarget(target)
		identifierCandidates, err := s.preferredIdentifierCandidates(ctx, identifierTarget)
		if err != nil {
			return nil, nil, err
		}
		resolvedClaimsIdentifier := false
		if ok {
			for _, candidate := range identifierCandidates {
				if candidate.Kind == ontology.NodeKindNote && candidate.NotePath == resolved.Path {
					resolvedClaimsIdentifier = true
					break
				}
			}
		}
		if ok && !resolvedClaimsIdentifier {
			resolvedClaimsIdentifier, err = s.noteClaimsPreferredIdentifier(ctx, resolved.Path, identifierTarget)
			if err != nil {
				return nil, nil, err
			}
			if resolvedClaimsIdentifier {
				own, err := s.canonicalCandidateRefs(ctx, []obsidian.ResolvedNoteTarget{{Path: resolved.Path}})
				if err != nil {
					return nil, nil, err
				}
				identifierCandidates = append(identifierCandidates, own...)
			}
		}
		if !ok || resolvedClaimsIdentifier {
			if len(identifierCandidates) > 1 {
				diagnostics = append(diagnostics, NodeResolveDiagnostic{
					Input:      input,
					Code:       "target_ambiguous",
					Message:    "preferred identifier resolves to multiple notes",
					Candidates: identifierCandidates,
				})
				continue
			}
			if len(identifierCandidates) == 1 {
				candidate := identifierCandidates[0]
				if identifierFragment == "" {
					targets = append(targets, parsedResolveTarget{
						Input:         input,
						SourceLocator: ontology.NodeSourceLocator(candidate),
						NotePath:      candidate.NotePath,
						Ref:           candidate,
					})
					continue
				}
				resolved = obsidian.ResolvedNoteTarget{Path: candidate.NotePath, Fragment: identifierFragment}
				ok = true
			}
		}
		if !ok {
			notePath, fragment := splitLocatorTarget(target)
			if notePath == "" {
				diagnostics = append(diagnostics, NodeResolveDiagnostic{Input: input, Code: "target_unresolved", Message: "link target did not resolve to a note"})
				continue
			}
			resolved = obsidian.ResolvedNoteTarget{Path: notePath, Fragment: fragment}
		}
		ref := ontology.NodeRef{NotePath: resolved.Path, Kind: ontology.NodeKindNote}
		sourceLocator := resolved.Path
		if strings.TrimSpace(resolved.Fragment) != "" {
			fragment := strings.TrimSpace(resolved.Fragment)
			sourceLocator = resolved.Path + "#" + fragment
			ref = ontology.NodeRef{NotePath: resolved.Path, Fragment: fragment, Kind: ontology.NodeKindEmbedded}
			if structural := strings.TrimPrefix(fragment, "struct:"); structural != fragment {
				ref.Fragment = ""
				ref.Structural = strings.TrimSpace(structural)
				ref.Kind = ontology.NodeKindSection
			}
		}
		targets = append(targets, parsedResolveTarget{
			Input:         input,
			SourceLocator: sourceLocator,
			NotePath:      resolved.Path,
			Fragment:      strings.TrimSpace(resolved.Fragment),
			Ref:           ref,
		})
	}
	if cacheErr != nil {
		return nil, nil, cacheErr
	}
	return targets, diagnostics, ctx.Err()
}
