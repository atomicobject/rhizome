package obsidian_test

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
)

func TestRemoveMdSuffix(t *testing.T) {
	tests := []struct {
		testName string
		input    string
		want     string
	}{
		{testName: "Without existing .md", input: "myNote", want: "myNote"},
		{testName: "With existing .md", input: "myNote.md", want: "myNote"},
	}

	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			// Act
			got := obsidian.RemoveMdSuffix(test.input)
			// Assert
			assert.Equal(t, test.want, got)
		})
	}
}
