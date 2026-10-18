package update

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	for _, tt := range []struct {
		a, b  string
		order int
		ok    bool
	}{
		{"v1.2.3", "v1.2.3", 0, true}, {"1.2.3", "v1.2.3", 0, true},
		{"v2.0.0", "v1.99.99", 1, true}, {"v1.10.0", "v1.9.9", 1, true},
		{"v1.2.3-rc.1", "v1.2.3", -1, true}, {"v1.2.3-alpha", "v1.2.3-beta", -1, true},
		{"v1.2.3-rc.2", "v1.2.3-rc.10", -1, true}, {"v1.2.3-1", "v1.2.3-a", -1, true},
		{"v1.2.3-a", "v1.2.3-a.1", -1, true}, {"v1.2.3-01", "v1.2.3", 0, false},
		{"dev", "v1.2.3", 0, false}, {"v1.2", "v1.2.3", 0, false}, {"v01.2.3", "v1.2.3", 0, false},
		{"v1.2.3+build", "v1.2.3", 0, false}, {"v999999999999999999999.0.0", "v2.0.0", 1, true},
	} {
		t.Run(tt.a+"/"+tt.b, func(t *testing.T) {
			got, ok := CompareVersions(tt.a, tt.b)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.order, got)
			reversed, reverseOK := CompareVersions(tt.b, tt.a)
			require.Equal(t, tt.ok, reverseOK)
			require.Equal(t, -tt.order, reversed)
		})
	}
}
