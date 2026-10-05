package codeanchor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCallEdgeSuffixKeysForSeed_PrefersQualifiedSuffixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		lang Lang
		fqn  string
		want []string
	}{
		{
			name: "python skips shallow tail",
			lang: LangPy,
			fqn:  "root.pkg.callee.new_func",
			want: []string{"pkg.callee.new_func"},
		},
		{
			name: "csharp caps deep expansion",
			lang: LangCs,
			fqn:  "Company.App.Feature.Constants.EventCategoryTypes.OTJ",
			want: []string{
				"App.Feature.Constants.EventCategoryTypes.OTJ",
				"Feature.Constants.EventCategoryTypes.OTJ",
				"Constants.EventCategoryTypes.OTJ",
			},
		},
		{
			name: "go keeps qualified package tails",
			lang: LangGo,
			fqn:  "github.com/acme/repo/pkg/sub.Run",
			want: []string{
				"acme/repo/pkg/sub.Run",
				"repo/pkg/sub.Run",
				"pkg/sub.Run",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, callEdgeSuffixKeysForSeed(tt.lang, tt.fqn))
		})
	}
}

func TestSuffixEligibleSymbolRef_RequiresQualifiedPackage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		lang     Lang
		pkg      string
		namePart string
		want     bool
	}{
		{
			name:     "python needs two package segments",
			lang:     LangPy,
			pkg:      "models",
			namePart: "User",
			want:     false,
		},
		{
			name:     "python allows dotted package",
			lang:     LangPy,
			pkg:      "pkg.models",
			namePart: "User",
			want:     true,
		},
		{
			name:     "go allows two slash segments",
			lang:     LangGo,
			pkg:      "repo/pkg",
			namePart: "Run",
			want:     true,
		},
		{
			name:     "go rejects single package segment",
			lang:     LangGo,
			pkg:      "pkg",
			namePart: "Run",
			want:     false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, suffixEligibleSymbolRef(tt.lang, tt.pkg, tt.namePart))
		})
	}
}
