package knowledge

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandle_String(t *testing.T) {
	require.Equal(t, "", Handle{}.String())
	require.Equal(t, "note:note-1", (Handle{Kind: KindNote, ID: "note-1"}).String())
	require.Equal(t, "notechunk:note-1#0", NoteChunkHandle("note-1", 0).String())
	require.Equal(t, "codechunk:anchor-1#symbol#2", CodeChunkHandle("anchor-1", "symbol", 2).String())
}

func TestParseHandle_Canonical(t *testing.T) {
	h, err := ParseHandle("notechunk:note-1#3")
	require.NoError(t, err)
	require.Equal(t, KindNoteChunk, h.Kind)
	require.Equal(t, "note-1", h.ID)
	require.Equal(t, []string{"3"}, h.Fragments)
	require.Equal(t, "notechunk:note-1#3", h.String())
}

func TestParseHandle_RejectsLegacyChunkPrefixes(t *testing.T) {
	_, err := ParseHandle("note_chunk:note-1:0")
	require.Error(t, err)

	_, err = ParseHandle("code_chunk:anchor-1:symbol:2")
	require.Error(t, err)
}

func TestParseHandle_File(t *testing.T) {
	h, err := ParseHandle("file:pkg/app/mcp/tools.go")
	require.NoError(t, err)
	require.Equal(t, KindFile, h.Kind)
	require.Equal(t, "pkg/app/mcp/tools.go", h.ID)
}

func TestParseHandle_FileRejectsAbsolute(t *testing.T) {
	_, err := ParseHandle("file:/abs/path.go")
	require.Error(t, err)

	_, err = ParseHandle("file:C:/abs/path.go")
	require.Error(t, err)
}

func TestFileHandleRejectsAbsolute(t *testing.T) {
	require.Panics(t, func() {
		FileHandle("/abs/path.go")
	})

	require.Panics(t, func() {
		FileHandle("C:/abs/path.go")
	})
}
