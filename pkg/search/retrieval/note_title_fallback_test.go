package retrieval

import (
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOntologyCandidateUsesPortableNoteTitleFallback(t *testing.T) {
	for _, path := range []string{"notes/release.v2.html", `notes\release.v2.html`} {
		candidate := buildOntologyCandidate("seed.md", "", path, semdb.OntologyEdgeRow{}, "", 1)
		require.Equal(t, "release.v2", candidate.Title)
		require.Equal(t, path, candidate.Path)
	}
}
