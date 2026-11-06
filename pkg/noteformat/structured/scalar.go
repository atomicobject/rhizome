package structured

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// These lexical rules follow PyYAML 6.0.3 resolver.py, not yaml.v3 tags.
var (
	integerText   = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	yamlInt       = regexp.MustCompile(`^[-+]?(?:0b[0-1_]+|0[0-7_]+|(?:0|[1-9][0-9_]*)|0x[0-9a-fA-F_]+|[1-9][0-9_]*(?::[0-5]?[0-9])+)$`)
	yamlFloat     = regexp.MustCompile(`^((?:[-+]?[0-9][0-9_]*\.[0-9_]*(?:[eE][-+][0-9]+)?|\.[0-9][0-9_]*(?:[eE][-+][0-9]+)?|[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+\.[0-9_]*)|[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`)
	yamlDate      = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	yamlTimestamp = regexp.MustCompile(`^([0-9]{4})-([0-9]{1,2})-([0-9]{1,2})(?:[Tt]|[ \t]+)([0-9]{1,2}):([0-9]{2}):([0-9]{2})(\.[0-9]*)?(?:[ \t]*(Z|([-+])([0-9]{1,2})(?::([0-9]{2}))?))?$`)
)

func scalar(n *yaml.Node) (Kind, string, error) {
	s := n.Value
	if n.Style != 0 {
		return String, s, nil
	}
	switch s {
	case "", "~", "null", "Null", "NULL":
		return Null, "", nil
	case "yes", "Yes", "YES", "true", "True", "TRUE", "on", "On", "ON":
		return Bool, "true", nil
	case "no", "No", "NO", "false", "False", "FALSE", "off", "Off", "OFF":
		return Bool, "false", nil
	}
	if yamlInt.MatchString(s) {
		if strings.Contains(s, ":") {
			return "", "", fmt.Errorf("sexagesimal integer unsupported")
		}
		clean := strings.ReplaceAll(s, "_", "")
		sign := ""
		if clean[0] == '-' || clean[0] == '+' {
			sign = clean[:1]
			clean = clean[1:]
		}
		base := 10
		if strings.HasPrefix(clean, "0b") {
			base = 2
			clean = clean[2:]
		} else if strings.HasPrefix(clean, "0x") {
			base = 16
			clean = clean[2:]
		} else if len(clean) > 1 && clean[0] == '0' {
			base = 8
		}
		value, ok := new(big.Int).SetString(sign+clean, base)
		if !ok {
			return "", "", fmt.Errorf("invalid integer")
		}
		return Number, value.String(), nil
	}
	if yamlFloat.MatchString(s) {
		if strings.Contains(s, ":") || strings.ContainsAny(s, "iInN") {
			return "", "", fmt.Errorf("nonfinite or sexagesimal float unsupported")
		}
		clean := strings.ReplaceAll(s, "_", "")
		if clean[0] == '+' {
			clean = clean[1:]
		}
		if strings.HasPrefix(clean, ".") {
			clean = "0" + clean
		} else if strings.HasPrefix(clean, "-.") {
			clean = "-0" + clean[1:]
		}
		parts := strings.FieldsFunc(clean, func(r rune) bool { return r == 'e' || r == 'E' })
		if strings.HasSuffix(parts[0], ".") {
			parts[0] += "0"
		}
		clean = parts[0]
		if len(parts) == 2 {
			clean += "e" + parts[1]
		}
		// Reject spellings whose SafeLoader float would change the decimal value.
		f, err := strconv.ParseFloat(clean, 64)
		if err != nil || math.IsInf(f, 0) {
			return "", "", fmt.Errorf("float outside supported domain")
		}
		original, ok := new(big.Rat).SetString(clean)
		if !ok {
			return "", "", fmt.Errorf("invalid decimal")
		}
		canonical := strconv.FormatFloat(f, 'g', -1, 64)
		roundtrip, ok := new(big.Rat).SetString(canonical)
		if !ok || original.Cmp(roundtrip) != 0 {
			return "", "", fmt.Errorf("lossy float unsupported")
		}
		if !strings.ContainsAny(canonical, ".eE") {
			canonical += ".0"
		}
		return Number, canonical, nil
	}
	if yamlDate.MatchString(s) {
		if _, err := time.Parse("2006-01-02", s); err != nil || strings.HasPrefix(s, "0000") {
			return "", "", fmt.Errorf("invalid date")
		}
		return Date, s, nil
	}
	if m := yamlTimestamp.FindStringSubmatch(s); m != nil {
		return timestamp(m)
	}
	return String, s, nil
}

func timestamp(m []string) (Kind, string, error) {
	pad := func(s string) string {
		if len(s) == 1 {
			return "0" + s
		}
		return s
	}
	fraction := m[7]
	// SafeLoader truncates sub-microsecond precision. Reject instead of losing it.
	if len(fraction) > 7 {
		return "", "", fmt.Errorf("timestamp exceeds microsecond precision")
	}
	if fraction == "." {
		fraction = ""
	}
	zone := m[8]
	if zone == "" {
		return "", "", fmt.Errorf("timezone-free timestamp unsupported")
	}
	if zone != "Z" {
		minutes := m[11]
		if minutes == "" {
			minutes = "00"
		}
		// Python normalizes unusual offsets; this profile only accepts civil offsets.
		if pad(m[10]) >= "24" || len(m[10]) > 2 || minutes > "59" {
			return "", "", fmt.Errorf("timestamp offset unsupported")
		}
		zone = m[9] + pad(m[10]) + ":" + minutes
	}
	text := m[1] + "-" + pad(m[2]) + "-" + pad(m[3]) + "T" + pad(m[4]) + ":" + m[5] + ":" + m[6] + fraction + zone
	if _, err := time.Parse(time.RFC3339Nano, text); err != nil || m[1] == "0000" {
		return "", "", fmt.Errorf("invalid timestamp")
	}
	return Timestamp, text, nil
}
