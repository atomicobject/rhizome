//go:build cgo

package codeanchor

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sitter "github.com/tree-sitter/go-tree-sitter"
	sitterpython "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

func TestParseTreeWithTimeout_ParsesSimple(t *testing.T) {
	parser := sitter.NewParser()
	defer parser.Close()
	require.NoError(t, parser.SetLanguage(sitter.NewLanguage(sitterpython.Language())))

	tree, timedOut, err := parseTreeWithTimeout(parser, []byte("x = 1\n"), 2*time.Second)
	require.NoError(t, err)
	require.False(t, timedOut)
	require.NotNil(t, tree)
	tree.Close()
}

func TestParseTreeWithTimeout_LargeInput(t *testing.T) {
	parser := sitter.NewParser()
	defer parser.Close()
	require.NoError(t, parser.SetLanguage(sitter.NewLanguage(sitterpython.Language())))

	content := []byte(strings.Repeat("x = 1\n", 20000))
	tree, timedOut, err := parseTreeWithTimeout(parser, content, 2*time.Second)
	require.NoError(t, err)
	require.False(t, timedOut)
	require.NotNil(t, tree)
	tree.Close()
}
