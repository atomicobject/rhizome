package views

import (
	"cmp"
	"math"
	"strings"
	"time"
)

// Unranked groups classify each bucket once, preserving compareSortValues ordering.
type groupSortValue struct {
	kind   int // empty, number, date, text
	text   string
	number float64
	date   time.Time
}

func newGroupSortValue(value any) groupSortValue {
	key := groupSortValue{text: valueString(value)}
	if key.text == "" {
		return key
	}
	if number, ok := numberValue(value); ok && !math.IsNaN(number) {
		key.kind, key.number = 1, number
	} else if date, ok := timeValue(value); ok {
		key.kind, key.date = 2, date
	} else {
		key.kind = 3
	}
	return key
}

func (left groupSortValue) compare(right groupSortValue) int {
	if order := cmp.Compare(left.kind, right.kind); order != 0 {
		return order
	}
	switch left.kind {
	case 1:
		return compareFloat(left.number, right.number)
	case 2:
		return left.date.Compare(right.date)
	default:
		return strings.Compare(left.text, right.text)
	}
}

// Ranked enums and single buckets need no parsing.
func (bucket *rowBucket) sortOrder() groupSortValue {
	if !bucket.sortKeyReady {
		bucket.sortKey = newGroupSortValue(bucket.sortValue)
		bucket.sortKeyReady = true
	}
	return bucket.sortKey
}
