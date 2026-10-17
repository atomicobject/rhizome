package update

import (
	"regexp"
	"strings"
)

var comparableVersion = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

// CompareVersions orders release versions. The bool is false for unparsable
// versions; the integer is negative, zero, or positive when a is before, equal
// to, or after b. A leading v is optional for app bundle short versions.
func CompareVersions(a, b string) (int, bool) {
	av, bv := comparableVersion.FindStringSubmatch(a), comparableVersion.FindStringSubmatch(b)
	if av == nil || bv == nil {
		return 0, false
	}
	for _, v := range [][]string{av, bv} {
		for _, id := range strings.Split(v[4], ".") {
			if numericIdentifier(id) && len(id) > 1 && id[0] == '0' {
				return 0, false
			}
		}
	}
	for i := 1; i <= 3; i++ {
		if c := compareNumeric(av[i], bv[i]); c != 0 {
			return c, true
		}
	}
	if av[4] == bv[4] {
		return 0, true
	}
	if av[4] == "" {
		return 1, true
	}
	if bv[4] == "" {
		return -1, true
	}
	ap, bp := strings.Split(av[4], "."), strings.Split(bv[4], ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		an, bn := numericIdentifier(ap[i]), numericIdentifier(bp[i])
		if an && bn {
			if c := compareNumeric(ap[i], bp[i]); c != 0 {
				return c, true
			}
		} else if an {
			return -1, true
		} else if bn {
			return 1, true
		} else if c := strings.Compare(ap[i], bp[i]); c != 0 {
			return c, true
		}
	}
	if len(ap) < len(bp) {
		return -1, true
	}
	return 1, true
}

func numericIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func compareNumeric(a, b string) int {
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return strings.Compare(a, b)
}
