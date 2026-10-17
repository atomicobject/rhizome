package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"strings"
)

// Locators returns cached source locators for refs without mutating note files.
func (s *Scope) Locators(ctx context.Context, refs []ontology.NodeRef) (map[string]ontology.NodeLocator, error) {
	return s.LocatorsWithEnsure(ctx, refs, ontology.EnsureLinkTargetNever)
}

// LocatorsWithEnsure returns source locators and may plan or apply safe link
// target fixes depending on ensure.
func (s *Scope) LocatorsWithEnsure(ctx context.Context, refs []ontology.NodeRef, ensure ontology.EnsureLinkTargetMode) (map[string]ontology.NodeLocator, error) {
	if s == nil || s.service == nil {
		return map[string]ontology.NodeLocator{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ensure == ontology.EnsureLinkTargetApply {
		if _, err := s.applyEmbeddedLinkTargets(ctx, refs); err != nil {
			return nil, err
		}
		ensure = ontology.EnsureLinkTargetNever
	}
	if ensure != ontology.EnsureLinkTargetNever {
		return s.locatorsWithEnsure(ctx, refs, ensure)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	locators, err := s.locatorsLocked(ctx, refs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ontology.NodeLocator, len(locators))
	for _, ref := range refs {
		ref = NormalizeRef(ref)
		locator, ok := locators[RefIdentityKey(ref)]
		if !ok || locator == nil {
			continue
		}
		out[locator.SourceLocator] = cloneNodeLocator(*locator)
		out[nodereadLocatorKey(ref)] = cloneNodeLocator(*locator)
	}
	return out, nil
}

func (s *Scope) locatorsWithEnsure(ctx context.Context, refs []ontology.NodeRef, ensure ontology.EnsureLinkTargetMode) (map[string]ontology.NodeLocator, error) {
	if s == nil || s.service == nil || s.service.Schema == nil || s.service.NoteReader == nil {
		return map[string]ontology.NodeLocator{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	service, err := s.linkServiceLocked(ctx)
	if err != nil {
		return nil, err
	}
	result, err := service.Locators(ctx, ontology.LinkTargetRequest{Refs: refs, Ensure: ensure})
	if canceled := readCancellation(ctx, err); canceled != nil {
		return nil, canceled
	}
	return result, err
}

func (s *Scope) locatorsLocked(ctx context.Context, refs []ontology.NodeRef) (map[string]*ontology.NodeLocator, error) {
	out := make(map[string]*ontology.NodeLocator, len(refs))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.service == nil || s.service.Schema == nil {
		return out, nil
	}
	missing := make([]ontology.NodeRef, 0, len(refs))
	identityKeysByLocatorKey := make(map[string][]string, len(refs))
	for _, ref := range refs {
		ref = NormalizeRef(ref)
		if ref.IsZero() {
			continue
		}
		identityKey := RefIdentityKey(ref)
		if locator, ok := s.locatorByRef[identityKey]; ok {
			locatorCopy := cloneNodeLocator(locator)
			out[identityKey] = &locatorCopy
			continue
		}
		if s.locatorKnown[identityKey] {
			continue
		}
		locatorKey := nodereadLocatorKey(ref)
		identityKeysByLocatorKey[locatorKey] = append(identityKeysByLocatorKey[locatorKey], identityKey)
		missing = append(missing, ref)
	}
	if len(missing) == 0 {
		return out, nil
	}
	if s.service.NoteReader == nil {
		return out, nil
	}
	service, err := s.linkServiceLocked(ctx)
	if err != nil {
		return nil, err
	}
	result, err := service.Locators(ctx, ontology.LinkTargetRequest{Refs: missing, Ensure: ontology.EnsureLinkTargetNever})
	// NodeLinkService can translate a canceled source read into diagnostics.
	// Check the context before publishing those locators as completed results.
	if canceled := readCancellation(ctx, err); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		return nil, err
	}
	for _, ref := range missing {
		s.locatorKnown[RefIdentityKey(ref)] = true
	}
	for locatorKey, locator := range result {
		for _, identityKey := range identityKeysByLocatorKey[locatorKey] {
			s.locatorByRef[identityKey] = locator
			locatorCopy := cloneNodeLocator(locator)
			out[identityKey] = &locatorCopy
		}
	}
	return out, nil
}

func nodereadLocatorKey(ref ontology.NodeRef) string {
	ref = NormalizeRef(ref)
	if ref.Kind == ontology.NodeKindNote {
		return strings.TrimSpace(ref.NotePath)
	}
	return ontology.NodeSourceLocator(ref)
}
