package codeanchor

import "testing"

func TestReverseString(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"", ""},
		{"a", "a"},
		{"ab", "ba"},
		{"abc", "cba"},
		{"myapp.service.MyClass", "ssalCyM.ecivres.ppaym"},
		{"pkg.Foo", "ooF.gkp"},
		{"backend.src.myapp.models.User", "resU.sledom.ppaym.crs.dnekcab"},
		// Unicode safety
		{"日本語", "語本日"},
		{"a日b", "b日a"},
	}
	for _, tt := range tests {
		got := ReverseString(tt.input)
		if got != tt.want {
			t.Errorf("ReverseString(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
