package codeanchor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/stretchr/testify/require"
)

type testNoteSource struct {
	path    string
	content string
	hash    string
	mtime   int64
}

func newTestNoteSource(path, content string, mtime int64) testNoteSource {
	return testNoteSource{
		path:    path,
		content: content,
		hash:    fmt.Sprintf("%x", sha256.Sum256([]byte(content))),
		mtime:   mtime,
	}
}

func (s testNoteSource) NotePathString() string            { return s.path }
func (s testNoteSource) NoteFormatID() noteformat.FormatID { return noteformat.FormatID("markdown") }
func (s testNoteSource) NoteContentString() string         { return s.content }
func (s testNoteSource) NoteContentHashString() string     { return s.hash }
func (s testNoteSource) NoteMtimeUnix() int64              { return s.mtime }

func ingestTestNoteFile(ctx context.Context, svc *Service, path, content string) (Note, error) {
	return svc.IngestNoteSource(ctx, newTestNoteSource(path, content, 0))
}

func TestExtractAnchorDeclarationsConsumesNoteSource(t *testing.T) {
	svc := NewServiceWithOptions(nil, nil, WithoutWarmCache())
	content := `---
title: "Node identity"
code-anchors:
  go:
    - label: node-id
      symbol: github.com/atomicobject/rhizome/pkg/ontology.OntologyNodeID
---
Body
`

	declarations, err := svc.ExtractAnchorDeclarations(context.Background(), newTestNoteSource("docs/node-identity.md", content, 123))

	require.NoError(t, err)
	require.Equal(t, "docs/node-identity.md", declarations.Note.Path)
	require.Equal(t, "Node identity", declarations.Note.Title)
	require.Equal(t, []string{"node-id"}, declarations.KeepLabels)
	require.Len(t, declarations.Note.DefinedAnchors, 1)
	require.Equal(t, AnchorFunc, declarations.Note.DefinedAnchors[0].Kind)
	require.Equal(t, "node-id", declarations.Note.DefinedAnchors[0].Label)
}
