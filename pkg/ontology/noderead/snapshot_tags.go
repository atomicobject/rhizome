package noderead

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	"sort"
	"strings"
)

func snapshotTags(snapshot *ontology.DocumentSnapshot) []string {
	if snapshot == nil {
		return nil
	}
	facts := snapshot.SelectorFacts().Tags
	tags := make([]string, 0, len(facts))
	seen := map[string]bool{}
	for _, fact := range facts {
		tag := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(fact, "#")))
		if tag != "" && !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	sort.Strings(tags)
	return tags
}
