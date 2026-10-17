package runtimeproviders

import (
	"fmt"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	schemadoc "github.com/atomicobject/rhizome/pkg/ontology/schemadoc"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"strings"
)

func CompanionResolver(note obsidian.NoteReader) schemadoc.CompanionDocResolver {
	if note == nil {
		note = &obsidian.Note{}
	}
	return func(path string) (schemadoc.CompanionDocMeta, bool) {
		meta := schemadoc.CompanionDocMeta{}
		if title, ok := note.Title(path); ok {
			meta.Title = strings.TrimSpace(title)
		}
		fact, ok := actions.NoteFactsFromReader(note).LookupFact(path)
		if ok {
			meta.Summary = firstFrontmatterSummary(fact.Frontmatter)
		}
		return meta, ok
	}
}

func firstFrontmatterSummary(frontmatter map[string]interface{}) string {
	if len(frontmatter) == 0 {
		return ""
	}
	for _, key := range []string{"summary", "synopsis", "about", "description", "desc", "doc"} {
		for current, value := range frontmatter {
			if !strings.EqualFold(current, key) {
				continue
			}
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" {
				return text
			}
		}
	}
	return ""
}
