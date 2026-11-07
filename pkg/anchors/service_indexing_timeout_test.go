package codeanchor

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

type blockedPHPIndexer struct{ release <-chan struct{} }

func (blockedPHPIndexer) Lang() Lang { return LangPhp }
func (idx blockedPHPIndexer) IndexFile([]byte, paths.CodePathRef) (FileSummary, error) {
	<-idx.release
	return FileSummary{}, nil
}

func TestIndexFileTimeoutPreservesSelectedIndexerLanguage(t *testing.T) {
	for _, path := range []string{"src/timeout.php", "src/timeout.go"} {
		t.Run(path, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				release := make(chan struct{})
				defer close(release)
				summary, err := (&Service{}).indexFileWithTimeout(context.Background(), blockedPHPIndexer{release: release}, nil, paths.CodePathRef{Rel: paths.CodePath(path)})
				require.NoError(t, err)
				require.Equal(t, ParseTimeout, summary.ParseStatus)
				require.Equal(t, LangPhp, summary.Lang)
				require.Equal(t, path, summary.FilePath)
			})
		})
	}
}
