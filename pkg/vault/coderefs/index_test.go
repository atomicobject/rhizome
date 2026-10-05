package coderefs

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndex_ReplaceFile(t *testing.T) {
	idx := NewIndex()

	refs := []CodeRef{
		{SourceFile: "src/main.go", Target: "Notes/MyNote.md", Kind: RefKindWikilink, Line: 10},
		{SourceFile: "src/main.go", Target: "Notes/Other.md", Kind: RefKindMention, Line: 20},
	}

	// Add refs
	idx.ReplaceFile("src/main.go", refs)

	// Verify byFile
	gotByFile := idx.RefsByFile("src/main.go")
	require.Len(t, gotByFile, 2)

	// Verify byNote
	gotByNote := idx.RefsByNote("Notes/MyNote.md")
	require.Len(t, gotByNote, 1)
	assert.Equal(t, "src/main.go", gotByNote[0].SourceFile)

	gotByNote2 := idx.RefsByNote("Notes/Other.md")
	require.Len(t, gotByNote2, 1)
}

func TestIndex_ReplaceFile_Update(t *testing.T) {
	idx := NewIndex()

	// Initial refs
	refs1 := []CodeRef{
		{SourceFile: "src/main.go", Target: "Notes/Old.md", Kind: RefKindWikilink, Line: 10},
	}
	idx.ReplaceFile("src/main.go", refs1)

	// Verify old ref exists
	assert.Len(t, idx.RefsByNote("Notes/Old.md"), 1)

	// Replace with new refs
	refs2 := []CodeRef{
		{SourceFile: "src/main.go", Target: "Notes/New.md", Kind: RefKindWikilink, Line: 15},
	}
	idx.ReplaceFile("src/main.go", refs2)

	// Old ref should be gone
	assert.Nil(t, idx.RefsByNote("Notes/Old.md"))

	// New ref should exist
	gotNew := idx.RefsByNote("Notes/New.md")
	require.Len(t, gotNew, 1)
	assert.Equal(t, 15, gotNew[0].Line)
}

func TestIndex_ReplaceFile_Empty(t *testing.T) {
	idx := NewIndex()

	refs := []CodeRef{
		{SourceFile: "src/main.go", Target: "Notes/MyNote.md", Kind: RefKindWikilink, Line: 10},
	}
	idx.ReplaceFile("src/main.go", refs)

	// Replace with empty refs (should remove the file)
	idx.ReplaceFile("src/main.go", nil)

	assert.Nil(t, idx.RefsByFile("src/main.go"))
	assert.Nil(t, idx.RefsByNote("Notes/MyNote.md"))
}

func TestIndex_RemoveFile(t *testing.T) {
	idx := NewIndex()

	refs := []CodeRef{
		{SourceFile: "src/main.go", Target: "Notes/MyNote.md", Kind: RefKindWikilink, Line: 10},
	}
	idx.ReplaceFile("src/main.go", refs)

	// Remove the file
	idx.RemoveFile("src/main.go")

	assert.Nil(t, idx.RefsByFile("src/main.go"))
	assert.Nil(t, idx.RefsByNote("Notes/MyNote.md"))
}

func TestIndex_RefsByNote_ReturnsNilForMissing(t *testing.T) {
	idx := NewIndex()
	assert.Nil(t, idx.RefsByNote("nonexistent.md"))
}

func TestIndex_RefsByFile_ReturnsNilForMissing(t *testing.T) {
	idx := NewIndex()
	assert.Nil(t, idx.RefsByFile("nonexistent.go"))
}

func TestIndex_RefsByNote_ReturnsCopy(t *testing.T) {
	idx := NewIndex()

	refs := []CodeRef{
		{SourceFile: "src/main.go", Target: "Notes/MyNote.md", Kind: RefKindWikilink, Line: 10},
	}
	idx.ReplaceFile("src/main.go", refs)

	// Get refs and modify the returned slice
	got := idx.RefsByNote("Notes/MyNote.md")
	got[0].Line = 999

	// Original should be unchanged
	original := idx.RefsByNote("Notes/MyNote.md")
	assert.Equal(t, 10, original[0].Line)
}

func TestIndex_RefsByFile_ReturnsCopy(t *testing.T) {
	idx := NewIndex()

	refs := []CodeRef{
		{SourceFile: "src/main.go", Target: "Notes/MyNote.md", Kind: RefKindWikilink, Line: 10},
	}
	idx.ReplaceFile("src/main.go", refs)

	// Get refs and modify
	got := idx.RefsByFile("src/main.go")
	got[0].Line = 999

	// Original should be unchanged
	original := idx.RefsByFile("src/main.go")
	assert.Equal(t, 10, original[0].Line)
}

func TestIndex_AllRefsByNote(t *testing.T) {
	idx := NewIndex()

	idx.ReplaceFile("src/a.go", []CodeRef{
		{SourceFile: "src/a.go", Target: "Notes/One.md", Kind: RefKindWikilink, Line: 1},
	})
	idx.ReplaceFile("src/b.go", []CodeRef{
		{SourceFile: "src/b.go", Target: "Notes/One.md", Kind: RefKindMention, Line: 2},
		{SourceFile: "src/b.go", Target: "Notes/Two.md", Kind: RefKindWikilink, Line: 3},
	})

	all := idx.AllRefsByNote()

	require.Len(t, all, 2)
	assert.Len(t, all["Notes/One.md"], 2)
	assert.Len(t, all["Notes/Two.md"], 1)
}

func TestIndex_AllRefsByFile(t *testing.T) {
	idx := NewIndex()

	idx.ReplaceFile("src/a.go", []CodeRef{
		{SourceFile: "src/a.go", Target: "Notes/One.md", Kind: RefKindWikilink, Line: 1},
	})
	idx.ReplaceFile("src/b.go", []CodeRef{
		{SourceFile: "src/b.go", Target: "Notes/Two.md", Kind: RefKindWikilink, Line: 2},
	})

	all := idx.AllRefsByFile()

	require.Len(t, all, 2)
	assert.Len(t, all["src/a.go"], 1)
	assert.Len(t, all["src/b.go"], 1)
}

func TestIndex_AllRefsByNote_ReturnsCopy(t *testing.T) {
	idx := NewIndex()

	idx.ReplaceFile("src/a.go", []CodeRef{
		{SourceFile: "src/a.go", Target: "Notes/One.md", Kind: RefKindWikilink, Line: 1},
	})

	all := idx.AllRefsByNote()
	all["Notes/One.md"][0].Line = 999

	// Original should be unchanged
	original := idx.RefsByNote("Notes/One.md")
	assert.Equal(t, 1, original[0].Line)
}

func TestIndex_MultipleRefsToSameNote(t *testing.T) {
	idx := NewIndex()

	// Multiple files referencing the same note
	idx.ReplaceFile("src/a.go", []CodeRef{
		{SourceFile: "src/a.go", Target: "Notes/Shared.md", Kind: RefKindWikilink, Line: 1},
	})
	idx.ReplaceFile("src/b.go", []CodeRef{
		{SourceFile: "src/b.go", Target: "Notes/Shared.md", Kind: RefKindMention, Line: 2},
	})
	idx.ReplaceFile("src/c.go", []CodeRef{
		{SourceFile: "src/c.go", Target: "Notes/Shared.md", Kind: RefKindWikilink, Line: 3},
	})

	refs := idx.RefsByNote("Notes/Shared.md")
	assert.Len(t, refs, 3)

	// Remove one file
	idx.RemoveFile("src/b.go")

	refs = idx.RefsByNote("Notes/Shared.md")
	assert.Len(t, refs, 2)
}

func TestIndex_ConcurrentAccess(t *testing.T) {
	idx := NewIndex()

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent writes
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			refs := []CodeRef{
				{SourceFile: "src/main.go", Target: "Notes/Note.md", Kind: RefKindWikilink, Line: n},
			}
			idx.ReplaceFile("src/main.go", refs)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = idx.RefsByNote("Notes/Note.md")
			_ = idx.RefsByFile("src/main.go")
			_ = idx.AllRefsByNote()
			_ = idx.AllRefsByFile()
		}()
	}

	wg.Wait()
	// If we get here without a race detector panic, the test passes
}
