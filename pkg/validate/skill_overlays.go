package validate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/skilloverlay"
)

func RunSkillOverlays(runCtx RunContext) CheckResult {
	if runCtx.SkillOverlays == nil {
		return CheckResult{Name: CheckSkillOverlays, Error: "skill overlay validator is not configured"}
	}
	manifest, err := runCtx.SkillOverlays()
	if err != nil {
		return CheckResult{Name: CheckSkillOverlays, Error: err.Error()}
	}
	return skillOverlayCheckResult(manifest)
}

func skillOverlayCheckResult(manifest skilloverlay.Manifest) CheckResult {
	result := CheckResult{Name: CheckSkillOverlays, OK: true}
	result.IssueCount = len(manifest.Issues)
	applied := 0
	for _, entry := range manifest.Entries {
		if strings.TrimSpace(entry.OverlayID) != "" {
			applied++
		}
	}
	result.Summary = fmt.Sprintf("%d overlay fragment(s) checked", applied)
	if len(manifest.Issues) == 0 {
		return result
	}
	result.OK = false
	manifest.Issues = append([]skilloverlay.Issue(nil), manifest.Issues...)
	sort.SliceStable(manifest.Issues, func(i, j int) bool {
		left := manifest.Issues[i]
		right := manifest.Issues[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		if left.Skill != right.Skill {
			return left.Skill < right.Skill
		}
		return left.Message < right.Message
	})
	result.Issues = make([]Issue, 0, len(manifest.Issues))
	for _, issue := range manifest.Issues {
		result.Issues = append(result.Issues, Issue{
			Code:    issue.Code,
			Path:    issue.Path,
			Field:   issue.Slot,
			Source:  issue.Overlay,
			Target:  issue.Skill,
			Message: issue.Message,
		})
	}
	return result
}
