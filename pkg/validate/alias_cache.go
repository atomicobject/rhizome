package validate

import "github.com/atomicobject/rhizome/pkg/vault/obsidian"

func aliasMapFromNotes(allNotes []string, getContent func(string) string) map[string][]string {
	aliasesByPath := make(map[string][]string)
	for _, notePath := range allNotes {
		if getContent == nil {
			continue
		}
		fm, err := obsidian.ExtractFrontmatter(getContent(notePath))
		if err != nil {
			continue
		}
		if aliases := obsidian.AliasListFromFrontmatter(fm); len(aliases) > 0 {
			aliasesByPath[notePath] = aliases
		}
	}
	if len(aliasesByPath) == 0 {
		return nil
	}
	return aliasesByPath
}
