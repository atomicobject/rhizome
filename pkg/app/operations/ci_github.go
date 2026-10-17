package operations

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/validate"
)

func renderCIGitHub(writer io.Writer, result ValidationResult) error {
	for _, check := range result.Checks {
		for _, issue := range check.Issues {
			if err := writeGitHubIssue(writer, check.Name, issue); err != nil {
				return err
			}
		}
		if strings.TrimSpace(check.Error) != "" {
			if err := writeGitHubCommand(writer, "error", []githubProperty{{name: "title", value: check.Name}}, check.Error); err != nil {
				return err
			}
		}
	}
	for _, outcome := range result.Outcomes {
		if outcome.Outcome != validate.CheckOutcomeBlocked {
			continue
		}
		message := oneLine(outcome.Summary)
		if message == "" {
			message = "validation prerequisite is blocked"
		}
		if outcome.PreparationCommand != "" {
			message += "; prepare: " + outcome.PreparationCommand
		}
		if err := writeGitHubCommand(
			writer,
			"error",
			[]githubProperty{{name: "title", value: outcome.Check + ":prerequisite"}},
			message,
		); err != nil {
			return err
		}
	}
	for _, followUp := range renderedRepairFollowUps(result) {
		if err := writeGitHubCommand(
			writer,
			"warning",
			[]githubProperty{
				{name: "file", value: followUp.evidence.SourcePath},
				{name: "title", value: "repair-follow-up:" + followUp.evidence.Diagnostic.Kind},
			},
			"phase="+strings.Join(followUp.phases, "/")+"; "+repairFollowUpEvidenceSummary(followUp.evidence),
		); err != nil {
			return err
		}
	}

	outcomeLabels := make([]string, 0, len(result.Outcomes))
	for _, outcome := range result.Outcomes {
		outcomeLabels = append(outcomeLabels, outcome.Check+":"+string(outcome.Outcome))
	}
	if _, err := fmt.Fprintf(
		writer,
		"Rhizome validation: selector=%s; checks=%s; outcomes=%s; issues=%d; errors=%d; exit=%d\n",
		result.Selector,
		strings.Join(result.EffectiveChecks, ","),
		strings.Join(outcomeLabels, ","),
		result.IssueCount,
		result.ErrorCount,
		result.ExitCode(),
	); err != nil {
		return err
	}
	if result.IdentifierReconciliation != nil {
		if _, err := fmt.Fprintf(
			writer,
			"Identifier reconciliation: %s; timings=%s\n",
			identifierReconciliationSummary(result),
			strings.ReplaceAll(identifierReconciliationTimings(result), "=", ":"),
		); err != nil {
			return err
		}
	}

	for _, outcome := range result.Outcomes {
		if outcome.RemediationCommand != "" {
			if _, err := fmt.Fprintf(writer, "Remediate %s: %s\n", outcome.Check, outcome.RemediationCommand); err != nil {
				return err
			}
		}
		if outcome.PreparationCommand != "" {
			if _, err := fmt.Fprintf(writer, "Prepare %s: %s\n", outcome.Check, outcome.PreparationCommand); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeGitHubIssue(writer io.Writer, check string, issue validate.Issue) error {
	path := issue.Path
	if path == "" {
		path = issue.Source
	}
	properties := make([]githubProperty, 0, 3)
	if path != "" {
		properties = append(properties, githubProperty{name: "file", value: path})
	}
	if issue.Line > 0 {
		properties = append(properties, githubProperty{name: "line", value: strconv.Itoa(issue.Line)})
	}
	title := check
	if issue.Code != "" {
		title += ":" + issue.Code
	}
	properties = append(properties, githubProperty{name: "title", value: title})

	return writeGitHubCommand(writer, "error", properties, issueMessage(issue))
}

type githubProperty struct {
	name  string
	value string
}

func writeGitHubCommand(writer io.Writer, command string, properties []githubProperty, message string) error {
	if _, err := fmt.Fprintf(writer, "::%s", command); err != nil {
		return err
	}
	if len(properties) > 0 {
		if _, err := io.WriteString(writer, " "); err != nil {
			return err
		}
		for index, property := range properties {
			if index > 0 {
				if _, err := io.WriteString(writer, ","); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(writer, "%s=%s", property.name, escapeGitHubProperty(property.value)); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(writer, "::%s\n", escapeGitHubData(message))
	return err
}

func escapeGitHubData(value string) string {
	value = strings.ReplaceAll(value, "%", "%25")
	value = strings.ReplaceAll(value, "\r", "%0D")
	return strings.ReplaceAll(value, "\n", "%0A")
}

func escapeGitHubProperty(value string) string {
	value = escapeGitHubData(value)
	value = strings.ReplaceAll(value, ":", "%3A")
	return strings.ReplaceAll(value, ",", "%2C")
}
