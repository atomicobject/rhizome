package cache

import (
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// canProjectNotePath checks executable provider support before cache reads a
// file. A nil runtime is the narrow Markdown compatibility mode only.
func (s *Service) canProjectNotePath(path paths.NotePath) bool {
	if s.noteRuntime == nil {
		return MarkdownCompatibilityAdmission(path)
	}
	provider, ok := s.noteRuntime.ProviderForPath(paths.RelPath(path))
	return ok && s.noteRuntime.CanProject(provider.Descriptor().ID)
}

// projectEntry adapts one selected source into the cache's legacy entry shape.
// Runtime-backed paths consume provider facts only. The Markdown parser is
// confined to markdownCompatibilityEntry for callers that explicitly retain
// the legacy no-runtime contract.
func (s *Service) projectEntry(path paths.NotePath, content []byte, modTime time.Time) (*Entry, bool, error) {
	if s.noteRuntime == nil {
		return markdownCompatibilityEntry(path, content, modTime), true, nil
	}
	provider, ok := s.noteRuntime.ProviderForPath(paths.RelPath(path))
	if !ok || !s.noteRuntime.CanProject(provider.Descriptor().ID) {
		return nil, false, nil
	}
	source, err := noteformat.NewAuthoredSource(path, provider.Descriptor(), content, modTime.Unix())
	if err != nil {
		return nil, false, err
	}
	projection, err := s.noteRuntime.Project(source)
	if err != nil {
		return nil, false, err
	}
	if projection.Status != noteformat.ProjectionStatusCurrent {
		return nil, false, nil
	}
	return entryFromProjectionFacts(source, projection, modTime), true, nil
}

func entryFromProjectionFacts(source noteformat.AuthoredSource, projection noteformat.Projection, modTime time.Time) *Entry {
	entry := &Entry{
		Path:        source.Path().String(),
		ModTime:     modTime,
		Size:        source.Size(),
		Content:     string(source.Bytes()),
		ContentTime: modTime,
		Frontmatter: make(map[string]interface{}),
		InlineProps: make(map[string][]string),
	}
	for _, fact := range projection.Facts.RootMetadata {
		if key := strings.TrimSpace(fact.Key); key != "" {
			entry.Frontmatter[key] = fact.Value.Export()
		}
	}
	for _, fact := range projection.Facts.InlineProperties {
		if key := strings.TrimSpace(fact.Key); key != "" {
			entry.InlineProps[key] = append(entry.InlineProps[key], fact.Value)
		}
	}
	for _, fact := range projection.Facts.Tags {
		entry.Tags = append(entry.Tags, fact.Value)
	}
	entry.Tags = normalizeTags(entry.Tags)
	if _, hasAliases := entry.Frontmatter["aliases"]; !hasAliases {
		aliases := make([]string, 0, len(projection.Facts.Aliases))
		for _, fact := range projection.Facts.Aliases {
			if alias := strings.TrimSpace(fact.Value); alias != "" {
				aliases = append(aliases, alias)
			}
		}
		if len(aliases) > 0 {
			entry.Frontmatter["aliases"] = aliases
		}
	}
	return entry
}

// markdownCompatibilityEntry preserves the legacy cache-only Markdown parser
// for callers that intentionally do not inject a provider runtime.
func markdownCompatibilityEntry(path paths.NotePath, content []byte, modTime time.Time) *Entry {
	entry := &Entry{
		Path:    path.String(),
		ModTime: modTime,
		Size:    int64(len(content)),
		Content: string(content),
	}
	if contentTime, ok := obsidian.ResolveContentTime(entry.Path, entry.Content); ok {
		entry.ContentTime = contentTime
	} else {
		entry.ContentTime = modTime
	}
	frontmatter, _ := obsidian.ExtractFrontmatter(entry.Content)
	if frontmatter != nil {
		entry.Frontmatter = frontmatter
		if tags, ok := frontmatter["tags"].([]string); ok {
			entry.Tags = append(entry.Tags, normalizeTags(tags)...)
		}
	}
	entry.Tags = append(entry.Tags, normalizeTags(stripHashtagPrefix(obsidian.ExtractHashtags(entry.Content)))...)
	entry.InlineProps = obsidian.ExtractInlineProperties(entry.Content)
	return entry
}
