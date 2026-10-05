package paths

import (
	"runtime"
	"testing"
)

func TestCaseKey(t *testing.T) {
	tests := []struct {
		path, folded string
	}{
		{"Notes/README.md", "notes/readme.md"},
		{"notes/readme.md", "notes/readme.md"},
		{"UPPER/CASE.TXT", "upper/case.txt"},
		{"mixed/Case/Path.go", "mixed/case/path.go"},
		{"notes/README.md", "notes/readme.md"},
		{"NOTES/readme.md", "notes/readme.md"},
		{"Notes/readme.MD", "notes/readme.md"},
		{"", ""},
	}

	for _, tt := range tests {
		want := tt.folded
		if runtime.GOOS == "linux" {
			want = tt.path
		}
		if got := CaseKey(tt.path); got != want {
			t.Errorf("CaseKey(%q) = %q, want %q", tt.path, got, want)
		}
	}
}

func TestCaseEqual(t *testing.T) {
	tests := []struct {
		a, b      string
		wantEqual bool
	}{
		{"Notes/README.md", "notes/readme.md", true},
		{"Notes/README.md", "Notes/README.md", true},
		{"a.txt", "b.txt", false},
		{"", "", true},
	}

	for _, tt := range tests {
		result := CaseEqual(tt.a, tt.b)

		if runtime.GOOS == "linux" {
			// On Linux, only exact matches are equal
			want := tt.a == tt.b
			if result != want {
				t.Errorf("CaseEqual(%q, %q) = %v on linux, want %v", tt.a, tt.b, result, want)
			}
		} else {
			// On Windows/macOS, case-insensitive comparison
			if result != tt.wantEqual {
				t.Errorf("CaseEqual(%q, %q) = %v, want %v", tt.a, tt.b, result, tt.wantEqual)
			}
		}
	}
}
