package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/ontology/idalloc"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
)

type CodeModeCurrentUserRuntime interface {
	Show(context.Context) (identity.Resolution, error)
	Set(context.Context, string) (identity.Resolution, error)
	Validate(context.Context) (identity.Resolution, error)
}

type CodeModeCurrentUserService struct {
	Runtime   CodeModeCurrentUserRuntime
	ReadWrite bool
}

func (s CodeModeCurrentUserService) Call(ctx context.Context, input map[string]any) CodeModeOutcome {
	var result identity.Resolution
	var err error
	switch codeModeAction(input) {
	case "show":
		result, err = s.Runtime.Show(ctx)
	case "set":
		if !s.ReadWrite {
			return codeModeFailure(fmt.Errorf(`{"code":"write_requires_read_write","message":"current_user set requires a read-write code-mode connection"}`), 1)
		}
		ref := codeModeString(input, "personTitleOrRef")
		if ref == "" {
			return codeModeFailure(fmt.Errorf("ref is required for current_user set"), 1)
		}
		result, err = s.Runtime.Set(ctx, ref)
	case "validate":
		result, err = s.Runtime.Validate(ctx)
	default:
		return codeModeFailure(fmt.Errorf("current_user action must be show, set, or validate"), 1)
	}
	if err != nil {
		return codeModeFailure(err, 1)
	}
	return codeModeSuccess(result)
}

type CodeModeNextIDAllocator interface {
	Allocate(context.Context, string, int, []string) (*idalloc.Result, error)
}

type CodeModeNextIDService struct{ Allocator CodeModeNextIDAllocator }

func (s CodeModeNextIDService) Call(ctx context.Context, input map[string]any) CodeModeOutcome {
	typeName := codeModeString(input, "type")
	if typeName == "" {
		return codeModeFailure(fmt.Errorf("type is required"), 1)
	}
	count := codeModeIntDefault(input, "count", 1)
	if count > idalloc.MaxBatchCount {
		count = idalloc.MaxBatchCount
	}
	result, err := s.Allocator.Allocate(ctx, typeName, count, codeModeStrings(input, "paths"))
	if err == nil {
		return codeModeSuccess(result)
	}
	code := idalloc.ErrorCode(err)
	if code == "" || code == "internal_error" {
		return codeModeFailure(err, 1)
	}
	diagnostic := map[string]any{"error": err.Error(), "code": code}
	if result != nil {
		diagnostic["type"], diagnostic["strategy"] = result.Type, result.Strategy
		if len(result.Paths) > 0 {
			diagnostic["paths"] = result.Paths
		}
	}
	return codeModeDiagnosticFailure(diagnostic, 1)
}

type CodeModeHeadingRenamer interface {
	Rename(context.Context, RenameHeadingParams) (RenameHeadingResult, error)
}

type CodeModeRenameHeadingService struct{ Renamer CodeModeHeadingRenamer }

func (s CodeModeRenameHeadingService) Call(ctx context.Context, input map[string]any) CodeModeOutcome {
	if codeModeBool(input, "apply") {
		return codeModeFailure(fmt.Errorf(`{"code":"apply_requires_read_write","message":"note_rename_heading remains plan-only on the agent surface"}`), 1)
	}
	path, oldHeading, newHeading := codeModeString(input, "path"), codeModeString(input, "oldHeading"), codeModeString(input, "newHeading")
	if path == "" || oldHeading == "" || newHeading == "" {
		return codeModeFailure(fmt.Errorf("path, oldHeading, and newHeading are required"), 1)
	}
	result, err := s.Renamer.Rename(ctx, RenameHeadingParams{
		Path: path, OldHeading: oldHeading, NewHeading: newHeading,
		UpgradeToBlockID: HeadingRenameUpgradeMode(codeModeStringDefault(input, "upgradeToBlockId", string(HeadingRenameUpgradeAuto))),
		Fallback:         HeadingRenameFallbackMode(codeModeStringDefault(input, "fallback", string(HeadingRenameFallbackBlockID))),
	})
	if err != nil {
		return codeModeFailure(err, 1)
	}
	return codeModeSuccess(result)
}

type CodeModeNoteMover interface {
	Move(context.Context, MoveParams) (MoveSummary, error)
}

type CodeModeNoteMoveService struct {
	Mover      CodeModeNoteMover
	ReadWrite  bool
	RenderText func(MoveSummary) string
}

func (s CodeModeNoteMoveService) Call(ctx context.Context, input map[string]any) CodeModeOutcome {
	if !s.ReadWrite {
		return codeModeFailure(fmt.Errorf(`{"code":"write_requires_read_write","message":"note_move requires a read-write code-mode connection"}`), 1)
	}
	if codeModeBool(input, "open") {
		return codeModeFailure(fmt.Errorf(`{"code":"machine_unsafe_option","message":"note_move open is unavailable in code mode"}`), 1)
	}
	sources := codeModeStrings(input, "sources")
	if source := codeModeString(input, "source"); source != "" {
		sources = append([]string{source}, sources...)
	}
	if len(sources) == 0 {
		return codeModeFailure(fmt.Errorf("note_move requires source or sources"), 1)
	}
	target, folder := codeModeString(input, "target"), codeModeString(input, "toFolder")
	moves := make([]MoveRequest, 0, len(sources))
	if folder != "" {
		for _, source := range sources {
			moves = append(moves, MoveRequest{Source: source, Target: filepath.Join(folder, filepath.Base(source))})
		}
	} else {
		if len(sources) != 1 || target == "" {
			return codeModeFailure(fmt.Errorf("note_move requires one source and target, or sources with toFolder"), 1)
		}
		moves = append(moves, MoveRequest{Source: sources[0], Target: target})
	}
	summary, err := s.Mover.Move(ctx, MoveParams{Moves: moves, Overwrite: codeModeBool(input, "overwrite"), UpdateBacklinks: codeModeBoolDefault(input, "updateBacklinks", true)})
	outcome := codeModeSuccess(summary)
	if err != nil {
		outcome = codeModeFailure(err, 1)
		outcome.Payload = summary
	}
	diagnostic, _ := outcome.Diagnostic.(map[string]any)
	if diagnostic == nil {
		diagnostic = make(map[string]any)
	}
	diagnostic["mutation"] = noteNamespaceMutationStatus(summary.Mutation)
	outcome.Diagnostic = diagnostic
	if s.RenderText != nil {
		outcome.Stdout = s.RenderText(summary)
	}
	return outcome
}

func codeModeStringDefault(input map[string]any, key, fallback string) string {
	if value := codeModeString(input, key); value != "" {
		return value
	}
	return fallback
}

func codeModeBoolDefault(input map[string]any, key string, fallback bool) bool {
	value, ok := input[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

func codeModeDiagnosticFailure(diagnostic any, exitCode int) CodeModeOutcome {
	data, _ := json.Marshal(diagnostic)
	return CodeModeOutcome{ExitCode: exitCode, Stderr: string(data) + "\n", Diagnostic: diagnostic}
}
