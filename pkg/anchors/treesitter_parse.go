//go:build cgo

package codeanchor

import (
	"errors"
	"time"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

func parseTreeWithTimeout(parser *sitter.Parser, content []byte, timeout time.Duration) (*sitter.Tree, bool, error) {
	if timeout <= 0 {
		tree := parser.Parse(content, nil)
		if tree == nil {
			return nil, false, errors.New("tree-sitter parse failed")
		}
		return tree, false, nil
	}

	start := time.Now()
	options := &sitter.ParseOptions{
		ProgressCallback: func(sitter.ParseState) bool {
			return time.Since(start) > timeout
		},
	}
	tree := parser.ParseWithOptions(func(i int, _ sitter.Point) []byte {
		if i >= len(content) {
			return nil
		}
		const chunkSize = 64 * 1024
		end := i + chunkSize
		if end > len(content) {
			end = len(content)
		}
		return content[i:end]
	}, nil, options)
	if tree == nil {
		if time.Since(start) >= timeout {
			return nil, true, nil
		}
		return nil, false, errors.New("tree-sitter parse failed")
	}
	return tree, false, nil
}
