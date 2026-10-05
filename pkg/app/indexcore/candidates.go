package indexcore

import (
	"sort"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	sqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/indexingpipe"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
)

func ingestCandidates(snapshot noteownership.Snapshot, plan noteownership.TransitionPlan, affected, fatal map[string]struct{}) (code, notes []indexingpipe.FileCandidate, sources map[paths.NotePath]noteformat.AuthoredSource) {
	sources = map[paths.NotePath]noteformat.AuthoredSource{}
	for _, c := range snapshot.Candidates() {
		if !c.Present {
			continue
		}
		_, force := affected[c.Path.String()]
		candidate := indexingpipe.FileCandidate{AbsPath: c.AbsPath.String(), RelPath: c.Path.String(), ModTime: c.ModTime, ForceRead: force}
		if c.Owner == notediscovery.Code {
			candidate.Kind = indexingpipe.FileKindCode
			candidate.Lang = c.Language
			code = append(code, candidate)
		}
		if c.Owner == notediscovery.Note && c.Provider == noteownership.MarkdownFormatID {
			if _, failed := fatal[c.Path.String()]; failed {
				delete(sources, paths.NotePath(c.Path))
				continue
			}
			candidate.Kind = indexingpipe.FileKindNote
			if source, ok := plan.Source(paths.NotePath(c.Path)); ok {
				sources[paths.NotePath(c.Path)] = source
				candidate.ModTime = source.Mtime()
			}
			notes = append(notes, candidate)
		}
	}
	return
}
func retiredPaths(snapshot noteownership.Snapshot) (notes, code []string) {
	for _, c := range snapshot.Candidates() {
		if c.PreviousOwner == notediscovery.Note && c.Owner != notediscovery.Note {
			notes = append(notes, c.Path.String())
		}
		if c.PreviousOwner == notediscovery.Code && c.Owner != notediscovery.Code {
			code = append(code, c.Path.String())
		}
	}
	return
}
func fatalPaths(plan noteownership.TransitionPlan, preparation notemeta.PublishedMetadataPreparation) map[string]struct{} {
	result := map[string]struct{}{}
	for _, p := range preparation.FatalPaths() {
		result[p.String()] = struct{}{}
	}
	for _, t := range plan.Transitions() {
		if t.Note != nil && t.Note.Status == sqlite.NoteProjectionStatusFatal {
			result[t.Path] = struct{}{}
		}
	}
	return result
}
func noteStrings(paths []paths.NotePath) []string {
	result := make([]string, len(paths))
	for i, p := range paths {
		result[i] = p.String()
	}
	return result
}
func dirtyScopes(paths []string, full bool) []anchors.DerivedScope {
	scopes := []anchors.DerivedScope{{Kind: anchors.DerivedGraph}}
	for _, kind := range []anchors.DerivedKind{anchors.DerivedNotes, anchors.DerivedOntology, anchors.DerivedCode} {
		if full {
			scopes = append(scopes, anchors.DerivedScope{Kind: kind})
			continue
		}
		for _, path := range paths {
			scopes = append(scopes, anchors.DerivedScope{Kind: kind, Path: path})
		}
	}
	return scopes
}
func sortDerived(work []anchors.DerivedWork) {
	sort.Slice(work, func(i, j int) bool {
		if work[i].Kind != work[j].Kind {
			return work[i].Kind < work[j].Kind
		}
		return work[i].Path < work[j].Path
	})
}
