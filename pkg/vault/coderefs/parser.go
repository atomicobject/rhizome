package coderefs

// Docs:
// - [Coderefs (Hub)](docs/hubs/Coderefs (Hub).md)
// - [Coderefs - scanning + indexing](docs/reference/guides/Coderefs - scanning + indexing.md)

import (
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/codefile"
)

type commentFamily string

const (
	commentFamilySlashLineBlock commentFamily = "slash_line_block"
	commentFamilyBlockOnly      commentFamily = "block_only"
	commentFamilyHash           commentFamily = "hash"
	commentFamilyHTML           commentFamily = "html"
)

type languageSpec struct {
	Language  string
	Family    commentFamily
	Extractor func(string) []TextBlock
}

// TextBlock represents a chunk of comment text extracted from source code.
type TextBlock struct {
	Text   string
	Line   int // 1-based start line
	Offset int // Byte offset in source
}

var commentFamilyExtractors = map[commentFamily]func(string) []TextBlock{
	commentFamilySlashLineBlock: extractSlashLineBlockComments,
	commentFamilyBlockOnly:      extractBlockOnlyComments,
	commentFamilyHash:           extractScriptStyleComments,
	commentFamilyHTML:           extractHTMLComments,
}

var languageSpecsByExt = buildLanguageSpecsByExt()

func buildLanguageSpecsByExt() map[string]languageSpec {
	out := map[string]languageSpec{
		".go":     newLanguageSpec("go", commentFamilySlashLineBlock),
		".java":   newLanguageSpec("java", commentFamilySlashLineBlock),
		".c":      newLanguageSpec("c", commentFamilySlashLineBlock),
		".h":      newLanguageSpec("c", commentFamilySlashLineBlock),
		".cpp":    newLanguageSpec("cpp", commentFamilySlashLineBlock),
		".hpp":    newLanguageSpec("cpp", commentFamilySlashLineBlock),
		".cc":     newLanguageSpec("cpp", commentFamilySlashLineBlock),
		".cs":     newLanguageSpec("csharp", commentFamilySlashLineBlock),
		".rs":     newLanguageSpec("rust", commentFamilySlashLineBlock),
		".py":     {Language: "python", Family: commentFamilyHash, Extractor: extractPythonComments},
		".rb":     newLanguageSpec("ruby", commentFamilyHash),
		".sh":     newLanguageSpec("shell", commentFamilyHash),
		".bash":   newLanguageSpec("shell", commentFamilyHash),
		".zsh":    newLanguageSpec("shell", commentFamilyHash),
		".html":   newLanguageSpec("html", commentFamilyHTML),
		".htm":    newLanguageSpec("html", commentFamilyHTML),
		".xhtml":  newLanguageSpec("html", commentFamilyHTML),
		".astro":  newLanguageSpec("astro", commentFamilyHTML),
		".vue":    newLanguageSpec("vue", commentFamilyHTML),
		".svelte": newLanguageSpec("svelte", commentFamilyHTML),
		".css":    newLanguageSpec("css", commentFamilyBlockOnly),
		".pcss":   newLanguageSpec("css", commentFamilyBlockOnly),
		".scss":   newLanguageSpec("scss", commentFamilySlashLineBlock),
		".sass":   newLanguageSpec("sass", commentFamilySlashLineBlock),
		".less":   newLanguageSpec("less", commentFamilySlashLineBlock),
		".styl":   newLanguageSpec("styl", commentFamilySlashLineBlock),
	}
	for _, ext := range codefile.TypeScriptExtensions() {
		out[ext] = newLanguageSpec("typescript", commentFamilySlashLineBlock)
	}
	for _, ext := range codefile.JavaScriptExtensions() {
		out[ext] = newLanguageSpec("javascript", commentFamilySlashLineBlock)
	}
	return out
}

var languageSpecsByName = buildLanguageSpecsByName(languageSpecsByExt)

func newLanguageSpec(language string, family commentFamily) languageSpec {
	return languageSpec{
		Language: language,
		Family:   family,
	}
}

func buildLanguageSpecsByName(byExt map[string]languageSpec) map[string]languageSpec {
	out := make(map[string]languageSpec, len(byExt))
	for _, spec := range byExt {
		if spec.Language == "" {
			continue
		}
		if _, ok := out[spec.Language]; ok {
			continue
		}
		out[spec.Language] = spec
	}
	return out
}

func (s languageSpec) extractor() func(string) []TextBlock {
	if s.Extractor != nil {
		return s.Extractor
	}
	return commentFamilyExtractors[s.Family]
}

func languageSpecForPath(path string) (languageSpec, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	spec, ok := languageSpecsByExt[ext]
	return spec, ok
}

// DetectLanguage returns a simplified language identifier based on file extension.
func DetectLanguage(path string) string {
	if spec, ok := languageSpecForPath(path); ok {
		return spec.Language
	}
	return ""
}

// ExtractCommentBlocks extracts comment text from source code based on the language.
//
// This parser is deliberately lightweight: it is for coderef discovery and
// rewrite targeting, not syntax validation. Keep it conservative about strings
// and code spans so retrieval links come from comments/docstrings rather than
// incidental string literals.
func ExtractCommentBlocks(lang, source string) []TextBlock {
	if spec, ok := languageSpecsByName[strings.ToLower(lang)]; ok {
		if extractor := spec.extractor(); extractor != nil {
			return extractor(source)
		}
	}
	// Fallback to slash+block comments for unknown languages.
	return extractSlashLineBlockComments(source)
}

func extractSlashLineBlockComments(source string) []TextBlock {
	return extractSlashStyleComments(source, true)
}

func extractBlockOnlyComments(source string) []TextBlock {
	return extractSlashStyleComments(source, false)
}

func extractSlashStyleComments(source string, allowLineComments bool) []TextBlock {
	var blocks []TextBlock
	n := len(source)
	i := 0
	line := 1

	for i < n {
		// Check for newline to increment line counter
		if source[i] == '\n' {
			line++
			i++
			continue
		}

		// Check for single-line string "..."
		if source[i] == '"' {
			i++
			for i < n {
				if source[i] == '\n' {
					line++ // Strings generally shouldn't span lines unescaped, but for safety
				}
				if source[i] == '\\' {
					i += 2 // Skip escaped char
					continue
				}
				if source[i] == '"' {
					i++
					break
				}
				i++
			}
			continue
		}

		// Check for single-line char '...' (simple approximation)
		if source[i] == '\'' {
			i++
			for i < n {
				if source[i] == '\\' {
					i += 2
					continue
				}
				if source[i] == '\'' {
					i++
					break
				}
				i++
			}
			continue
		}

		// Check for line comment //
		if allowLineComments && i+1 < n && source[i] == '/' && source[i+1] == '/' {
			start := i
			i += 2
			// Consume until newline
			for i < n && source[i] != '\n' {
				i++
			}
			// Extract content (trim slashes)
			content := source[start:i]
			blocks = append(blocks, TextBlock{
				Text:   content,
				Line:   line,
				Offset: start,
			})
			continue
		}

		// Check for block comment /* ... */
		if i+1 < n && source[i] == '/' && source[i+1] == '*' {
			start := i
			startLine := line
			i += 2
			for i+1 < n {
				if source[i] == '\n' {
					line++
				}
				if source[i] == '*' && source[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			content := source[start:i]
			blocks = append(blocks, TextBlock{
				Text:   content,
				Line:   startLine,
				Offset: start,
			})
			continue
		}

		i++
	}

	return blocks
}

func extractHTMLComments(source string) []TextBlock {
	var blocks []TextBlock
	n := len(source)
	i := 0
	line := 1

	for i < n {
		if source[i] == '\n' {
			line++
			i++
			continue
		}

		if source[i] == '"' || source[i] == '\'' || source[i] == '`' {
			quote := source[i]
			i++
			for i < n {
				if source[i] == '\n' {
					line++
				}
				if source[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if source[i] == quote {
					i++
					break
				}
				i++
			}
			continue
		}

		if strings.HasPrefix(source[i:], "<!--") {
			start := i
			startLine := line
			i += len("<!--")
			for i < n {
				if source[i] == '\n' {
					line++
				}
				if strings.HasPrefix(source[i:], "-->") {
					i += len("-->")
					break
				}
				i++
			}
			blocks = append(blocks, TextBlock{
				Text:   source[start:i],
				Line:   startLine,
				Offset: start,
			})
			continue
		}

		i++
	}

	return blocks
}

// extractPythonComments handles # comments and triple-quoted docstrings (""" and ”')
func extractPythonComments(source string) []TextBlock {
	var blocks []TextBlock
	n := len(source)
	i := 0
	line := 1

	for i < n {
		if source[i] == '\n' {
			line++
			i++
			continue
		}

		// Check for triple-quoted strings (docstrings): """ or '''
		if i+2 < n {
			// Triple double quotes
			if source[i] == '"' && source[i+1] == '"' && source[i+2] == '"' {
				start := i
				startLine := line
				i += 3
				// Find closing """
				for i+2 < n {
					if source[i] == '\n' {
						line++
					}
					if source[i] == '"' && source[i+1] == '"' && source[i+2] == '"' {
						i += 3
						break
					}
					i++
				}
				content := source[start:i]
				blocks = append(blocks, TextBlock{
					Text:   content,
					Line:   startLine,
					Offset: start,
				})
				continue
			}

			// Triple single quotes
			if source[i] == '\'' && source[i+1] == '\'' && source[i+2] == '\'' {
				start := i
				startLine := line
				i += 3
				// Find closing '''
				for i+2 < n {
					if source[i] == '\n' {
						line++
					}
					if source[i] == '\'' && source[i+1] == '\'' && source[i+2] == '\'' {
						i += 3
						break
					}
					i++
				}
				content := source[start:i]
				blocks = append(blocks, TextBlock{
					Text:   content,
					Line:   startLine,
					Offset: start,
				})
				continue
			}
		}

		// Skip regular strings (non-docstring)
		if source[i] == '"' || source[i] == '\'' {
			quote := source[i]
			i++
			for i < n {
				if source[i] == '\n' {
					line++
					break // Unescaped newline ends string in Python
				}
				if source[i] == '\\' {
					i += 2
					continue
				}
				if source[i] == quote {
					i++
					break
				}
				i++
			}
			continue
		}

		// Check for comment #
		if source[i] == '#' {
			start := i
			i++
			for i < n && source[i] != '\n' {
				i++
			}
			content := source[start:i]
			blocks = append(blocks, TextBlock{
				Text:   content,
				Line:   line,
				Offset: start,
			})
			continue
		}

		i++
	}

	return blocks
}

// extractScriptStyleComments handles # comments
// Note: This is a simplified parser and might misidentify # inside complex constructs
func extractScriptStyleComments(source string) []TextBlock {
	var blocks []TextBlock
	n := len(source)
	i := 0
	line := 1

	for i < n {
		if source[i] == '\n' {
			line++
			i++
			continue
		}

		// Check for string "..." or '...'
		if source[i] == '"' || source[i] == '\'' {
			quote := source[i]
			i++
			for i < n {
				if source[i] == '\n' {
					line++
				}
				if source[i] == '\\' {
					i += 2
					continue
				}
				if source[i] == quote {
					i++
					break
				}
				i++
			}
			continue
		}

		// Check for comment #
		if source[i] == '#' {
			start := i
			i++
			for i < n && source[i] != '\n' {
				i++
			}
			content := source[start:i]
			blocks = append(blocks, TextBlock{
				Text:   content,
				Line:   line,
				Offset: start,
			})
			continue
		}

		i++
	}

	return blocks
}
