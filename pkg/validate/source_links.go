package validate

import (
	"context"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	vaulthealth "github.com/atomicobject/rhizome/pkg/vault/health"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type validationSourceLink struct {
	Source string
	Fact   noteformat.UnresolvedAuthoredLinkFact
	Target string
	Exists bool
}

type validationSourceLinks struct {
	sources map[string]notemeta.NoteSourceSnapshot
	paths   []string
	files   func() *obsidian.VaultFileIndex
	cache   *obsidian.NotePathCache
	links   []validationSourceLink
}

func validationLinksFor(ctx context.Context, run RunContext) (*validationSourceLinks, error) {
	snapshot := run.sourceSnapshot
	if snapshot == nil {
		var err error
		snapshot, err = captureValidationRunSnapshot(ctx, run)
		if err != nil {
			return nil, err
		}
	}
	snapshot.linksOnce.Do(func() { snapshot.linkInventory = buildValidationSourceLinks(run, snapshot) })
	return snapshot.linkInventory, nil
}

func buildValidationSourceLinks(run RunContext, snapshot *validationRunSnapshot) *validationSourceLinks {
	aliases := make(map[string][]string, len(snapshot.sources))
	for _, source := range snapshot.sources {
		aliases[source.Path.String()] = source.Aliases
	}
	inventory := &validationSourceLinks{sources: snapshot.byPath, paths: snapshot.paths, cache: obsidian.BuildNotePathCacheWithAliases(snapshot.paths, aliases), files: obsidian.LazyVaultFileIndex(run.VaultDef, snapshot.paths)}
	for _, sourcePath := range snapshot.paths {
		source := snapshot.byPath[sourcePath]
		for _, fact := range source.Projection.Facts.Links {
			if validationLinkExternal(source, fact) || !validationLinkSyntaxEnabled(run.VaultDef, fact) {
				continue
			}
			target, exists := inventory.resolve(source, fact)
			inventory.links = append(inventory.links, validationSourceLink{Source: source.Path.String(), Fact: fact, Target: target, Exists: exists})
		}
	}
	return inventory
}

func (inventory *validationSourceLinks) resolve(source notemeta.NoteSourceSnapshot, fact noteformat.UnresolvedAuthoredLinkFact) (string, bool) {
	target, exists := notemeta.ResolveProjectedLink(inventory.cache, source.Path.String(), source.DocumentBase, fact)
	if fact.Path == "" && fact.Fragment != "" && fact.Resolution == noteformat.LinkResolutionNoteReference {
		return source.Path.String(), true
	}
	if exists {
		return target, true
	}
	// Obsidian can open attachments and deliberately ignored notes without an indexed edge.
	if fact.Resolution == noteformat.LinkResolutionNoteReference {
		return inventory.files().Resolve(fact.Path, source.Path.String())
	}
	base := source.Path.String()
	if fact.Resolution == noteformat.LinkResolutionURI && source.DocumentBase != nil && source.DocumentBase.URI.Path != "" {
		base = validationRelativePath(base, source.DocumentBase.URI.Path)
		if strings.HasSuffix(source.DocumentBase.URI.Path, "/") {
			base += "/"
		}
	}
	candidate := fact.Path
	if fact.Resolution == noteformat.LinkResolutionRelativePath {
		candidate = markdownsyntax.DecodeMarkdownPath(candidate)
	}
	if candidate == "" {
		candidate = strings.TrimSuffix(base, "/")
	} else {
		candidate = validationRelativePath(base, candidate)
	}
	if candidate == "" || candidate == "." || candidate == ".." || strings.HasPrefix(candidate, "../") || strings.HasSuffix(fact.Path, "/") {
		return "", false
	}
	return inventory.files().Resolve("/"+strings.TrimPrefix(candidate, "/"), source.Path.String())
}

func validationRelativePath(base, target string) string {
	if strings.HasPrefix(target, "/") {
		return path.Clean(strings.TrimPrefix(target, "/"))
	}
	if strings.HasSuffix(base, "/") {
		return path.Join(base, target)
	}
	return path.Join(path.Dir(base), target)
}

func validationLinkSyntaxEnabled(vault obsidian.VaultDefinition, fact noteformat.UnresolvedAuthoredLinkFact) bool {
	switch fact.Resolution {
	case noteformat.LinkResolutionNoteReference:
		return vault.SupportsWikilinks()
	case noteformat.LinkResolutionRelativePath:
		return vault.SupportsMarkdownLinks()
	default:
		return true
	}
}

func validationLinkExternal(source notemeta.NoteSourceSnapshot, fact noteformat.UnresolvedAuthoredLinkFact) bool {
	if fact.URI != nil && (fact.URI.Scheme != "" || fact.URI.Authority != "" || fact.URI.ProtocolRelative) {
		return true
	}
	if fact.Resolution == noteformat.LinkResolutionURI && source.DocumentBase != nil {
		base := source.DocumentBase.URI
		return base.Scheme != "" || base.Authority != "" || base.ProtocolRelative
	}
	return false
}

func sourceBrokenLinks(ctx context.Context, run RunContext, opts ScanOptions) ([]vaulthealth.BrokenLink, error) {
	inventory, err := validationLinksFor(ctx, run)
	if err != nil {
		return nil, err
	}
	formats, err := run.NoteMetadata.FormatRuntime()
	if err != nil {
		return nil, err
	}
	var broken []vaulthealth.BrokenLink
	var readErrors vaulthealth.NoteReadErrors
	extraTargets := map[string]notemeta.NoteSourceSnapshot{}
	for _, link := range inventory.links {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fact := link.Fact
		if opts.SkipEmbeds && fact.Embed || opts.SkipAnchors && fact.Fragment != "" || !opts.IncludeImages && validationImageTarget(fact.Path) {
			continue
		}
		// Provider-owned notes participate; source-relative attachments do not.
		if fact.Resolution == noteformat.LinkResolutionRelativePath && path.Ext(fact.Path) != "" {
			_, catalogued := inventory.sources[link.Target]
			_, claimed := formats.ProviderForPath(paths.Normalize(fact.Path))
			if !catalogued && !claimed {
				continue
			}
		}
		reason := vaulthealth.BrokenLinkReasonNoteMissing
		if link.Exists {
			if fact.Fragment == "" {
				continue
			}
			target, known := inventory.sources[link.Target]
			if !known {
				target, known = extraTargets[link.Target]
			}
			if !known {
				// Explicit ignored note reads keep Obsidian's file-opening semantics.
				reader := run.NoteReader
				if captured, ok := reader.(*validationSnapshotNoteReader); ok {
					reader = captured.base
				}
				content, readErr := reader.GetContents(run.VaultDef, link.Target)
				if readErr != nil {
					readErrors = append(readErrors, &vaulthealth.NoteReadError{Source: link.Source, Target: link.Target, Err: readErr})
					continue
				}
				projected, projectErr := run.NoteMetadata.ProjectNoteSource(link.Target, content, 0)
				if projectErr != nil {
					readErrors = append(readErrors, &vaulthealth.NoteReadError{Source: link.Source, Target: link.Target, Err: projectErr})
					continue
				}
				target = projected
				extraTargets[link.Target] = target
			}
			if sourceHasFragment(target.FragmentTargets, fact.Fragment, fact.Resolution != noteformat.LinkResolutionNoteReference) {
				continue
			}
			reason = vaulthealth.BrokenLinkReasonHeadingMissing
			if strings.HasPrefix(fact.Fragment, "^") {
				reason = vaulthealth.BrokenLinkReasonBlockMissing
			}
		}
		source := inventory.sources[link.Source]
		raw := ""
		if fact.RawRange.StartByte >= 0 && fact.RawRange.EndByte >= fact.RawRange.StartByte && fact.RawRange.EndByte <= len(source.Content) {
			raw = source.Content[fact.RawRange.StartByte:fact.RawRange.EndByte]
		}
		kind := markdownsyntax.BacklinkTypeBasic
		if fact.Embed {
			kind = markdownsyntax.BacklinkTypeEmbed
		} else if fact.Subtype == "alias" {
			kind = markdownsyntax.BacklinkTypeAlias
		}
		line := 1 + strings.Count(source.Content[:max(0, min(len(source.Content), fact.RawRange.StartByte))], "\n")
		broken = append(broken, vaulthealth.BrokenLink{Source: link.Source, Target: fact.Path, LinkType: kind, Alias: fact.Display, Fragment: fact.Fragment, Reason: reason, Line: line, Raw: raw, Markdown: fact.Resolution != noteformat.LinkResolutionNoteReference})
	}
	if len(readErrors) > 0 {
		return broken, readErrors
	}
	return broken, nil
}

func sourceHasFragment(targets []noteformat.FragmentTargetFact, fragment string, uri bool) bool {
	if uri {
		if decoded, err := url.PathUnescape(fragment); err == nil {
			fragment = decoded
		}
		counts := map[string]int{}
		for _, target := range targets {
			if target.Kind == noteformat.FragmentTargetElementID || target.Kind == noteformat.FragmentTargetLegacyName {
				if strings.EqualFold(target.Text, fragment) {
					return true
				}
			}
			if target.Kind == noteformat.FragmentTargetHeading {
				slug := markdownsyntax.MarkdownHeadingSlug(target.Text)
				ordinal := counts[slug]
				counts[slug]++
				if ordinal > 0 {
					slug += "-" + strconv.Itoa(ordinal)
				}
				if slug == fragment {
					return true
				}
			}
		}
	}
	if strings.HasPrefix(fragment, "^") {
		for _, target := range targets {
			if target.Kind == noteformat.FragmentTargetBlock && strings.TrimPrefix(target.Text, "^") == strings.TrimPrefix(fragment, "^") {
				return true
			}
		}
		return false
	}
	for _, part := range strings.Split(fragment, "#") {
		if part == "" {
			continue
		}
		found := false
		for _, target := range targets {
			if target.Kind == noteformat.FragmentTargetHeading || target.Kind == noteformat.FragmentTargetElementID || target.Kind == noteformat.FragmentTargetLegacyName {
				if markdownsyntax.ObsidianHeadingKey(target.Text) == markdownsyntax.ObsidianHeadingKey(part) {
					found = true
					break
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func validationImageTarget(target string) bool {
	switch strings.ToLower(path.Ext(target)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".avif", ".ico":
		return true
	}
	return false
}

func validationBlockFragmentAuthored(source notemeta.NoteSourceSnapshot, fact noteformat.UnresolvedAuthoredLinkFact) bool {
	if !fact.FragmentRange.Present {
		return false
	}
	return markdownBlockFragmentAuthored(source, fact)
}
