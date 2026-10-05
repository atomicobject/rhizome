package views

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

// monthBuckets groups rows by the calendar month of a Date or DateTime field,
// keyed YYYY-MM and labeled like "Sep 2026", newest first. Rows without a
// parseable date share a final "No <field>" bucket.
func monthBuckets(rows []TableRow, field string, capability FieldCapability) []rowBucket {
	index := map[string]int{}
	var out []rowBucket
	for _, row := range rows {
		key := ""
		if raw, ok := capabilityFieldValue(row, field, capability); ok {
			key = monthKey(raw)
		}
		at, ok := index[key]
		if !ok {
			at = len(out)
			index[key] = at
			out = append(out, rowBucket{value: key, label: monthLabel(key, firstNonEmpty(capability.Label, labelForField(field)))})
		}
		out[at].rows = append(out[at].rows, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].value == "") != (out[j].value == "") {
			return out[j].value == ""
		}
		return out[i].value > out[j].value
	})
	return out
}

// monthKey is the YYYY-MM of a date value in its authored offset, or "".
func monthKey(value any) string {
	if parsed, ok := rowDate(value); ok {
		return parsed.Format("2006-01")
	}
	return ""
}

func monthLabel(key string, fieldLabel string) string {
	parsed, err := time.Parse("2006-01", key)
	if err != nil {
		return "No " + strings.ToLower(strings.TrimSpace(fieldLabel))
	}
	return parsed.Format("Jan 2006")
}

// checkGroupBucket rejects a bucket other than month, and a month bucket on
// anything but one schema Date or DateTime field. Fields without schema type
// information group their unparseable values as undated.
func checkGroupBucket(group *viewconfig.GroupSpec, capabilities []FieldCapability) error {
	if group == nil || group.Bucket == "" {
		return nil
	}
	if group.Bucket != viewconfig.GroupBucketMonth {
		return fmt.Errorf("%w: unsupported group bucket %q", ErrInvalidRequest, group.Bucket)
	}
	fields := normalizedGroupFields(group)
	if len(fields) != 1 {
		return fmt.Errorf("%w: a month bucket groups exactly one field", ErrInvalidRequest)
	}
	capability, ok := resolveCapability(capabilities, fields[0])
	if ok && capability.schemaOrderKnown && (normalizedValueKind(capability.ValueKind) != "date" || capability.List) {
		return fmt.Errorf("%w: month buckets need a Date or DateTime field; %q is not one", ErrInvalidRequest, fields[0])
	}
	return nil
}
