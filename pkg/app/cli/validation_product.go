package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/validate"
)

type ValidationProductRequest struct {
	Selectors                                      []string
	Surface                                        validate.ExecutionSurface
	VaultName, ApplyCommand                        string
	MaxIssues                                      int
	SkipAnchors, SkipEmbeds, IncludeImages         bool
	ScopeNote, ScopeTarget, ScopeRef               string
	Repair, Apply, AllowHistorical, NonInteractive bool
	Confirm                                        func(string) (bool, error)
	// ApplySelection lists reviewed action IDs or stable issue keys; when
	// non-empty, apply runs exactly those actions (validate.Options.ApplySelection).
	ApplySelection []string
}

type ValidationProductRunner interface {
	Run(context.Context, ValidationProductRequest) (ValidationResult, error)
}

func RunValidationProduct(ctx context.Context, runner ValidationProductRunner, request ValidationProductRequest) (ValidationResult, error) {
	if runner == nil {
		return ValidationResult{}, fmt.Errorf("validation product runner is not configured")
	}
	return runner.Run(ctx, request)
}

// ResolveValidationApplySelection merges --action entries with the entries of
// a --from-plan file. Either one turns apply into an exact reviewed selection,
// so both require apply.
func ResolveValidationApplySelection(apply bool, actionIDs []string, planPath string) ([]string, error) {
	if len(actionIDs) == 0 && planPath == "" {
		return nil, nil
	}
	if !apply {
		return nil, fmt.Errorf("--action and --from-plan require --apply")
	}
	selection := append([]string(nil), actionIDs...)
	if planPath != "" {
		// The plan is a user file, so a relative path is relative to the
		// working directory, not the vault.
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve --from-plan: %w", err)
		}
		data, err := os.ReadFile(paths.AbsFromInput(cwd, planPath).String())
		if err != nil {
			return nil, fmt.Errorf("read --from-plan: %w", err)
		}
		entries, err := ParseValidationApplySelection(data)
		if err != nil {
			return nil, fmt.Errorf("--from-plan %s: %w", planPath, err)
		}
		selection = append(selection, entries...)
	}
	return selection, nil
}

// ParseValidationApplySelection reads a reviewed selection file: either a JSON
// array of action IDs/issue keys, or plain text with one entry per line where
// blank lines and lines starting with # are ignored.
func ParseValidationApplySelection(data []byte) ([]string, error) {
	text := strings.TrimSpace(string(data))
	var entries []string
	if strings.HasPrefix(text, "[") {
		var raw []string
		if err := json.Unmarshal([]byte(text), &raw); err != nil {
			return nil, fmt.Errorf("parse selection JSON array: %w", err)
		}
		for _, entry := range raw {
			if entry = strings.TrimSpace(entry); entry != "" {
				entries = append(entries, entry)
			}
		}
	} else {
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				entries = append(entries, line)
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("selection lists no action IDs or issue keys")
	}
	return entries, nil
}

func BuildValidationProductApplyCommand(request ValidationProductRequest) string {
	parts := []string{"rzm"}
	if request.Surface == validate.SurfaceAgent {
		parts = append(parts, "agent")
	}
	parts = append(parts, "validate", "fix")
	if len(request.Selectors) == 1 && strings.TrimSpace(request.Selectors[0]) != "" {
		parts = append(parts, shellQuoteValidationProductArg(request.Selectors[0]))
	}
	parts = append(parts, "--apply")
	appendValue := func(flag, value string) {
		if strings.TrimSpace(value) != "" {
			parts = append(parts, flag, shellQuoteValidationProductArg(value))
		}
	}
	appendValue("--vault", request.VaultName)
	if request.SkipAnchors {
		parts = append(parts, "--skip-anchors")
	}
	if request.SkipEmbeds {
		parts = append(parts, "--skip-embeds")
	}
	if request.IncludeImages {
		parts = append(parts, "--include-images")
	}
	appendValue("--scope-note", request.ScopeNote)
	appendValue("--scope-target", request.ScopeTarget)
	appendValue("--scope-ref", request.ScopeRef)
	if request.AllowHistorical {
		parts = append(parts, "--allow-historical")
	}
	return strings.Join(parts, " ")
}

func shellQuoteValidationProductArg(arg string) string {
	if arg == "" {
		return "''"
	}
	for _, r := range arg {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == '/' || r == ':') {
			return "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
		}
	}
	return arg
}
