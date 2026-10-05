package codeanchor

import (
	"bytes"
	"log"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogParseErrorIfPoor_SuppressesUsefulSummaries(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	logParseErrorIfPoor("csharp", "foo.cs", FileSummary{
		FilePath: "foo.cs",
		Lang:     LangCs,
		Symbols:  []Symbol{{Lang: LangCs, Kind: SymClass, File: "foo.cs", Name: "Foo"}},
	}, "")
	require.Empty(t, buf.String())
}

func TestLogParseErrorIfPoor_LogsEmptySummaries(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	logParseErrorIfPoor("csharp", "foo.cs", FileSummary{FilePath: "foo.cs", Lang: LangCs}, "recovery unavailable")
	require.Contains(t, buf.String(), "codeanchor: csharp parse errors for foo.cs")
	require.Contains(t, buf.String(), "recovery unavailable")
}
