package operations

import (
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func evaluateExpressionMatches(allNotes []string, note obsidian.NoteReader, expr *InputExpression) []string {
	if expr == nil {
		return nil
	}

	if len(allNotes) == 0 {
		return nil
	}

	results := make(chan string, len(allNotes))
	numWorkers := determineWorkerCount(len(allNotes))
	if numWorkers == 0 {
		return nil
	}
	batchSize := (len(allNotes) + numWorkers - 1) / numWorkers

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		start := i * batchSize
		end := start + batchSize
		if end > len(allNotes) {
			end = len(allNotes)
		}
		if start >= len(allNotes) {
			continue
		}

		wg.Add(1)
		go func(files []string) {
			defer wg.Done()
			facts := NoteFactsFromReader(note)
			for _, path := range files {
				ctx := &noteContext{
					path:         path,
					allowContent: true,
					facts:        facts,
				}
				if evaluateExpression(expr, ctx) {
					results <- path
				}
			}
		}(allNotes[start:end])
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var matches []string
	for path := range results {
		matches = append(matches, path)
	}
	return matches
}

func evaluateExpression(expr *InputExpression, ctx *noteContext) bool {
	if expr == nil {
		return false
	}
	switch expr.Type {
	case exprLeaf:
		return ctx.matchesInput(expr.Input)
	case exprAnd:
		return evaluateExpression(expr.Left, ctx) && evaluateExpression(expr.Right, ctx)
	case exprOr:
		return evaluateExpression(expr.Left, ctx) || evaluateExpression(expr.Right, ctx)
	case exprNot:
		return !evaluateExpression(expr.Left, ctx)
	default:
		return false
	}
}

// MatchesExpressionForPath evaluates an input expression against a single path.
// When allowContent is false, tag/property matches always return false.
func MatchesExpressionForPath(expr *InputExpression, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, path string, allowContent bool) bool {
	if expr == nil {
		return false
	}
	ctx := &noteContext{
		path:         path,
		allowContent: allowContent,
		facts:        NoteFactsFromReader(note),
	}
	return evaluateExpression(expr, ctx)
}

type noteContext struct {
	path         string
	allowContent bool
	facts        NoteFacts
}

func (c *noteContext) matchesInput(input *ListInput) bool {
	if input == nil {
		return false
	}
	switch input.Type {
	case InputTypeFile:
		return matchFilePath(c.path, input.Value)
	case InputTypeFind:
		return obsidian.FuzzyMatch(input.Value, c.path)
	case InputTypeTag:
		if !c.allowContent {
			return false
		}
		fact, ok := c.facts.LookupFact(c.path)
		if !ok {
			return false
		}
		for _, tag := range fact.Tags {
			if tagMatchesParent(tag, input.Value) {
				return true
			}
		}
		return false
	case InputTypeProperty:
		if !c.allowContent {
			return false
		}
		return c.propertyHasValue(input.Property, input.Value)
	default:
		return false
	}
}

func matchFilePath(notePath, input string) bool {
	normalizedInputPath := string(paths.Normalize(input))

	if normalizedInputPath == "*" {
		return true
	}

	normalizedNotePath := string(paths.Normalize(notePath))
	dirPrefix := normalizedInputPath + "/"
	return normalizedNotePath == normalizedInputPath || strings.HasPrefix(normalizedNotePath, dirPrefix)
}

func (c *noteContext) propertyHasValue(property string, target string) bool {
	if strings.TrimSpace(property) == "" || strings.TrimSpace(target) == "" {
		return false
	}

	fact, ok := c.facts.LookupFact(c.path)
	if !ok {
		return false
	}
	return propertyFactsHaveValue(fact.Frontmatter, fact.InlineProps, property, target)
}

// propertyFactsHaveValue checks provider-projected metadata for a property value.
func propertyFactsHaveValue(frontmatter map[string]any, inline map[string][]string, property string, target string) bool {
	if strings.TrimSpace(property) == "" || strings.TrimSpace(target) == "" {
		return false
	}

	targetNorm := notemeta.NormalizePropertyValue(target)
	propKey := strings.ToLower(strings.TrimSpace(property))

	checkValues := func(vals []string) bool {
		for _, v := range vals {
			if notemeta.NormalizePropertyValue(v) == targetNorm {
				return true
			}
		}
		return false
	}

	for k, v := range frontmatter {
		if strings.ToLower(strings.TrimSpace(k)) != propKey {
			continue
		}
		info := obsidian.AnalyzePropertyValue(v)
		if checkValues(info.Values) {
			return true
		}
	}

	for k, vals := range inline {
		if strings.ToLower(strings.TrimSpace(k)) != propKey {
			continue
		}
		if checkValues(vals) {
			return true
		}
	}

	return false
}
