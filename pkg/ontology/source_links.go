package ontology

import "github.com/atomicobject/rhizome/pkg/noteformat"

func (p *PreparedDocument) prepareSourceLinks() {
	p.sourceLinks = p.root.Projection.Facts.Links
	if p.markdown != nil && p.root.Projection.ProviderVersion == "" {
		for _, link := range markdownProjectionStructuredLinks(p.rootProjection) {
			resolution := noteformat.LinkResolutionNoteReference
			if link.Kind == "markdown" {
				resolution = noteformat.LinkResolutionRelativePath
			}
			p.sourceLinks = append(p.sourceLinks, noteformat.UnresolvedAuthoredLinkFact{Resolution: resolution, Syntax: noteformat.AuthoredLinkSyntax(link.Kind), ResolverInput: link.ResolverInput(), Target: link.Target, Path: link.Path, Fragment: link.Fragment, Display: link.Display, Embed: link.Embed, RawRange: noteformat.SourceRange{StartByte: link.RawSpan.Start, EndByte: link.RawSpan.End}})
		}
	}
	if p.markdown != nil || p.structure == nil {
		return
	}
	selected := map[noteformat.OwnerKey]bool{p.structure.Root: true}
	for _, record := range p.records {
		if record.valid {
			selected[record.owner.Key] = true
		}
	}
	nearest := map[noteformat.OwnerKey]noteformat.OwnerKey{}
	var ownerFor func(noteformat.OwnerKey) noteformat.OwnerKey
	ownerFor = func(key noteformat.OwnerKey) noteformat.OwnerKey {
		if key == "" {
			return p.structure.Root
		}
		if owner, ok := nearest[key]; ok {
			return owner
		}
		if selected[key] {
			nearest[key] = key
			return key
		}
		owner := p.owners[key]
		if owner == nil {
			return ""
		}
		nearest[key] = ownerFor(owner.Parent)
		return nearest[key]
	}
	p.linksByOwner = map[noteformat.OwnerKey][]noteformat.UnresolvedAuthoredLinkFact{}
	for _, link := range p.sourceLinks {
		key := ownerFor(link.Owner)
		p.linksByOwner[key] = append(p.linksByOwner[key], link)
	}
}

// SourceLinks retains the legacy Markdown subtree scope and maps native
// occurrences to their nearest binding-selected owner, excluding siblings.
func (projection *NodeProjection) SourceLinks() []noteformat.UnresolvedAuthoredLinkFact {
	if projection == nil || projection.Prepared == nil {
		return nil
	}
	p := projection.Prepared
	links := p.sourceLinks
	if projection.Snapshot == nil {
		links = p.linksByOwner[projection.OwnerKey]
	} else if projection.Ref.Kind != NodeKindNote {
		span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]
		if span == nil {
			return nil
		}
		var scoped []noteformat.UnresolvedAuthoredLinkFact
		for _, link := range links {
			if link.RawRange.StartByte >= span.Range.Start && link.RawRange.EndByte <= span.Range.End {
				scoped = append(scoped, link)
			}
		}
		links = scoped
	}
	out := append([]noteformat.UnresolvedAuthoredLinkFact(nil), links...)
	for i := range out {
		if out[i].URI != nil {
			uri := *out[i].URI
			out[i].URI = &uri
		}
	}
	return out
}
