package operations

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/atomicobject/rhizome/pkg/validate"
)

// CIFormat selects one read-only CI renderer. GitHub workflow commands and
// machine JSON intentionally never share one output stream.
type CIFormat string

const (
	CIFormatGitHub CIFormat = "github"
	CIFormatJSON   CIFormat = "json"
)

// CIErrorResult is the stable machine-readable failure envelope used when CI
// cannot produce a ValidationResult.
type CIErrorResult struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error"`
	ExitCode int    `json:"exitCode"`
}

// ParseCIFormat accepts exactly one renderer name.
func ParseCIFormat(value string) (CIFormat, error) {
	format := CIFormat(value)
	switch format {
	case CIFormatGitHub, CIFormatJSON:
		return format, nil
	default:
		return "", invalidCIFormatError(value)
	}
}

// RenderCI renders an already-computed validation result without executing
// checks, preparing projections, or mutating vault state.
func RenderCI(writer io.Writer, format CIFormat, result ValidationResult) error {
	switch format {
	case CIFormatJSON:
		return renderCIJSON(writer, result)
	case CIFormatGitHub:
		return renderCIGitHub(writer, result)
	default:
		return invalidCIFormatError(string(format))
	}
}

// RenderCIError keeps pre-result execution/configuration failures on the
// selected renderer's stream. Callers return ValidationExitFailure afterward.
func RenderCIError(writer io.Writer, format CIFormat, renderErr error) error {
	if _, err := ParseCIFormat(string(format)); err != nil {
		return err
	}
	if renderErr == nil {
		return fmt.Errorf("cannot render a nil CI error")
	}

	switch format {
	case CIFormatJSON:
		encoder := json.NewEncoder(writer)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(CIErrorResult{
			OK:       false,
			Error:    renderErr.Error(),
			ExitCode: ValidationExitFailure,
		})
	case CIFormatGitHub:
		if err := writeGitHubCommand(writer, "error", []githubProperty{{name: "title", value: "Rhizome validation"}}, renderErr.Error()); err != nil {
			return err
		}
		_, err := fmt.Fprintf(writer, "Rhizome validation: execution/configuration failure; exit=%d\n", ValidationExitFailure)
		return err
	default:
		return invalidCIFormatError(string(format))
	}
}

// RenderValidationResultHuman renders the shared result for an interactive
// terminal. It reports every selected outcome, including blocked and absent
// features, without interpreting or executing follow-up commands.
func RenderValidationResultHuman(writer io.Writer, result ValidationResult) error {
	if _, err := fmt.Fprintf(writer, "Validation selector: %s\n", result.Selector); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Effective checks: %s\n", strings.Join(result.EffectiveChecks, ", ")); err != nil {
		return err
	}

	checks := make(map[string]validate.CheckResult, len(result.Checks))
	for _, check := range result.Checks {
		checks[check.Name] = check
	}
	for _, outcome := range result.Outcomes {
		if _, err := fmt.Fprintf(writer, "[%s] %s", outcome.Outcome, outcome.Check); err != nil {
			return err
		}
		if outcome.Summary != "" {
			if _, err := fmt.Fprintf(writer, ": %s", oneLine(outcome.Summary)); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(writer, "\n"); err != nil {
			return err
		}

		if check, ok := checks[outcome.Check]; ok {
			if _, err := fmt.Fprintf(writer, "  Check: %s\n", humanCheckSummary(check)); err != nil {
				return err
			}
			if strings.TrimSpace(check.Error) != "" {
				if _, err := fmt.Fprintf(writer, "  Error: %s\n", oneLine(check.Error)); err != nil {
					return err
				}
			}
			for _, issue := range check.Issues {
				if _, err := fmt.Fprintf(writer, "  - %s\n", humanIssueSummary(issue)); err != nil {
					return err
				}
			}
		}
		if outcome.RemediationCommand != "" {
			if _, err := fmt.Fprintf(writer, "  Remediate: %s\n", outcome.RemediationCommand); err != nil {
				return err
			}
		}
		if outcome.PreparationCommand != "" {
			if _, err := fmt.Fprintf(writer, "  Prepare: %s\n", outcome.PreparationCommand); err != nil {
				return err
			}
		}
	}
	if result.FixPlan != nil {
		if _, err := fmt.Fprintf(
			writer,
			"Repair plan: total=%d; safe=%d; confirmation=%d; agent-required=%d\n",
			result.FixPlan.TotalCount,
			result.FixPlan.SafeCount,
			result.FixPlan.ConfirmationCount,
			result.FixPlan.AgentCount,
		); err != nil {
			return err
		}
		for _, action := range result.FixPlan.Actions {
			if strings.TrimSpace(action.Summary) == "" {
				continue
			}
			if _, err := fmt.Fprintf(writer, "  Action [%s]: %s\n", action.Safety, oneLine(action.Summary)); err != nil {
				return err
			}
		}
	}
	if result.FixExecution != nil {
		if _, err := fmt.Fprintf(
			writer,
			"Repair apply: applied=%d; skipped=%d; failed=%d; remaining=%d\n",
			len(result.FixExecution.Applied),
			len(result.FixExecution.Skipped),
			len(result.FixExecution.Failed),
			result.FixExecution.RemainingFindings,
		); err != nil {
			return err
		}
		if strings.TrimSpace(result.FixExecution.ReplanCommand) != "" {
			if _, err := fmt.Fprintf(writer, "  Replan: %s\n", result.FixExecution.ReplanCommand); err != nil {
				return err
			}
		}
	}
	if strings.TrimSpace(result.ExecutionError) != "" {
		if _, err := fmt.Fprintf(writer, "Repair execution error: %s\n", oneLine(result.ExecutionError)); err != nil {
			return err
		}
	}
	followUps := renderedRepairFollowUps(result)
	if len(followUps) > 0 {
		if _, err := fmt.Fprintf(writer, "Repair follow-ups: %s\n", pluralCount(len(followUps), "review item")); err != nil {
			return err
		}
		for _, followUp := range followUps {
			if _, err := fmt.Fprintf(
				writer,
				"  Follow-up [%s] %s [%s]: %s\n",
				strings.Join(followUp.phases, "/"),
				followUp.evidence.SourcePath,
				followUp.evidence.Diagnostic.Kind,
				repairFollowUpEvidenceSummary(followUp.evidence),
			); err != nil {
				return err
			}
		}
	}
	if result.IdentifierReconciliation != nil {
		if _, err := fmt.Fprintf(writer, "Identifier reconciliation: %s\n", identifierReconciliationSummary(result)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "  Timings: %s\n", identifierReconciliationTimings(result)); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintf(
		writer,
		"Validation summary: %s; %s; exit=%d\n",
		pluralCount(result.IssueCount, "issue"),
		pluralCount(result.ErrorCount, "error"),
		result.ExitCode(),
	)
	return err
}

func renderCIJSON(writer io.Writer, result ValidationResult) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(result)
}

type renderedRepairFollowUp struct {
	evidence validate.RepairFollowUp
	phases   []string
}

func renderedRepairFollowUps(result ValidationResult) []renderedRepairFollowUp {
	var output []renderedRepairFollowUp
	indexes := make(map[string]int)
	appendPhase := func(phase string, followUps []validate.RepairFollowUp) {
		for _, followUp := range followUps {
			encoded, _ := json.Marshal(followUp)
			key := string(encoded)
			if index, ok := indexes[key]; ok {
				output[index].phases = append(output[index].phases, phase)
				continue
			}
			indexes[key] = len(output)
			output = append(output, renderedRepairFollowUp{
				evidence: followUp,
				phases:   []string{phase},
			})
		}
	}
	if result.FixPlan != nil {
		appendPhase("plan", result.FixPlan.FollowUps)
	}
	if result.FixExecution != nil {
		appendPhase("apply", result.FixExecution.FollowUps)
	}
	return output
}

func repairFollowUpEvidenceSummary(followUp validate.RepairFollowUp) string {
	message := strings.TrimSpace(followUp.Diagnostic.Kind)
	if followUp.Diagnostic.Field != nil {
		message = strings.TrimSpace(followUp.Diagnostic.Field.Message)
	}
	if followUp.Diagnostic.Link != nil {
		message = strings.TrimSpace(followUp.Diagnostic.Link.Message)
	}
	if message == "" {
		message = "identifier repair evidence requires review"
	}
	diagnostic, _ := json.Marshal(followUp.Diagnostic)
	return fmt.Sprintf(
		"%s; source-hash=%s; membership=%s; diagnostic=%s",
		oneLine(message),
		followUp.SourceHash,
		strings.Join(followUp.MembershipKeys, ","),
		diagnostic,
	)
}

func identifierReconciliationSummary(result ValidationResult) string {
	reconciliation := result.IdentifierReconciliation
	if reconciliation == nil {
		return ""
	}
	collisions := 0
	fingerprint := ""
	if reconciliation.Plan != nil {
		collisions = len(reconciliation.Plan.Collisions)
		fingerprint = reconciliation.Plan.Fingerprint
	}
	return fmt.Sprintf(
		"collisions=%d; fingerprint=%s; history-complete=%t; fallback-collisions=%d",
		collisions,
		fingerprint,
		reconciliation.Diagnostics.HistoryComplete,
		reconciliation.Diagnostics.FallbackCollisions,
	)
}

func identifierReconciliationTimings(result ValidationResult) string {
	if result.IdentifierReconciliation == nil {
		return ""
	}
	timings := result.IdentifierReconciliation.Diagnostics.Timings
	return fmt.Sprintf(
		"inventory=%s; git-provenance=%s; reference-discovery=%s; transaction-construction=%s; apply=%s; post-validation=%s",
		timings.Inventory.String(),
		timings.GitProvenance.String(),
		timings.ReferenceDiscovery.String(),
		timings.TransactionConstruction.String(),
		timings.Apply.String(),
		timings.PostValidation.String(),
	)
}

func humanCheckSummary(check validate.CheckResult) string {
	summary := oneLine(check.Summary)
	if summary == "" {
		if check.IssueCount == 0 {
			return "clean"
		}
		return pluralCount(check.IssueCount, "issue")
	}
	if check.IssueCount == 0 {
		return summary
	}
	return fmt.Sprintf("%s (%s)", summary, pluralCount(check.IssueCount, "issue"))
}

func humanIssueSummary(issue validate.Issue) string {
	var output strings.Builder
	path := issue.Path
	if path == "" {
		path = issue.Source
	}
	if path != "" {
		output.WriteString(path)
		if issue.Line > 0 {
			fmt.Fprintf(&output, ":%d", issue.Line)
		}
		output.WriteString(" ")
	}
	if issue.Code != "" {
		fmt.Fprintf(&output, "[%s] ", issue.Code)
	}
	output.WriteString(oneLine(issueMessage(issue)))
	return output.String()
}

func issueMessage(issue validate.Issue) string {
	message := strings.TrimSpace(issue.Message)
	if message == "" && issue.Target != "" {
		message = "validation issue targeting " + issue.Target
	}
	if message == "" && issue.Code != "" {
		message = issue.Code
	}
	if message == "" {
		message = "validation issue"
	}
	return message
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func pluralCount(count int, singular string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %ss", count, singular)
}

func invalidCIFormatError(value string) error {
	return fmt.Errorf("invalid CI format %q; expected exactly one of github or json", value)
}
