package validate

import (
	"fmt"
	"sort"
	"strings"
)

var migrationBucketCodes = map[string]struct{}{
	"missing_required_field":                 {},
	"wrong_target_type":                      {},
	"link_target_missing":                    {},
	"query_compile_error":                    {},
	"identifier_strategy_migration_required": {},
}

// BuildMigrationBuckets groups repeated validation issues by migration shape.
// It intentionally ignores max-issues truncation; callers should pass the full
// issue slice before trimming CheckResult.Issues.
func BuildMigrationBuckets(issues []Issue) []MigrationBucket {
	type accumulator struct {
		bucket  MigrationBucket
		paths   map[string]struct{}
		targets map[string]struct{}
	}
	groups := make(map[string]*accumulator)
	for _, issue := range issues {
		if _, ok := migrationBucketCodes[issue.Code]; !ok {
			continue
		}
		key := migrationBucketKey(issue)
		acc := groups[key]
		if acc == nil {
			acc = &accumulator{
				bucket: MigrationBucket{
					Code:   issue.Code,
					Type:   issue.Type,
					Field:  issue.Field,
					Safety: migrationBucketSafety(issue.Code),
				},
				paths:   map[string]struct{}{},
				targets: map[string]struct{}{},
			}
			groups[key] = acc
		}
		acc.bucket.IssueCount++
		if issue.Path != "" {
			acc.paths[issue.Path] = struct{}{}
		}
		if issue.Target != "" {
			acc.targets[issue.Target] = struct{}{}
		}
	}
	out := make([]MigrationBucket, 0, len(groups))
	for _, acc := range groups {
		acc.bucket.Paths = sortedKeys(acc.paths)
		acc.bucket.Targets = sortedKeys(acc.targets)
		acc.bucket.Summary = migrationBucketSummary(acc.bucket)
		out = append(out, acc.bucket)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IssueCount != out[j].IssueCount {
			return out[i].IssueCount > out[j].IssueCount
		}
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Field < out[j].Field
	})
	return out
}

func migrationBucketKey(issue Issue) string {
	switch issue.Code {
	case "query_compile_error":
		return issue.Code
	default:
		return strings.Join([]string{issue.Code, issue.Type, issue.Field}, "\x00")
	}
}

func migrationBucketSafety(code string) FixSafety {
	switch code {
	case "identifier_strategy_migration_required":
		return FixSafetyConfirm
	case "missing_required_field", "wrong_target_type", "link_target_missing", "query_compile_error":
		return FixSafetyAgent
	default:
		return FixSafetyAgent
	}
}

func migrationBucketSummary(bucket MigrationBucket) string {
	field := bucket.Field
	if bucket.Type != "" && field != "" {
		field = bucket.Type + "." + field
	}
	switch bucket.Code {
	case "missing_required_field":
		return fmt.Sprintf("Add or intentionally relax required field %s", firstNonEmptyString(field, "unknown field"))
	case "wrong_target_type":
		return fmt.Sprintf("Review links on %s that resolve to the wrong ontology type", firstNonEmptyString(field, "unknown field"))
	case "link_target_missing":
		return fmt.Sprintf("Repair unresolved links on %s", firstNonEmptyString(field, "unknown field"))
	case "query_compile_error":
		return "Update saved query recipes that no longer compile against the active schema"
	case "identifier_strategy_migration_required":
		return fmt.Sprintf("Review and apply whole-pool identifier strategy migration for %s", firstNonEmptyString(field, "identifier field"))
	default:
		return bucket.Code
	}
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
