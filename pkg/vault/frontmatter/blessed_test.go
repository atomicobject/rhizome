package frontmatter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterBlessed_CaseInsensitiveKeysAndLowercaseOutput(t *testing.T) {
	in := map[string]interface{}{
		"Summary":     "Hello",
		"TAGS":        []string{"a", "b"},
		"Description": "World",
		"Unrelated":   "nope",
	}

	out := FilterBlessed(in)
	assert.Equal(t, map[string]interface{}{
		"summary":     "Hello",
		"tags":        []string{"a", "b"},
		"description": "World",
	}, out)
}
