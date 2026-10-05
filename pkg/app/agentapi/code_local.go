package agentapi

import "context"

// CallOutcome preserves an operation's domain result independently of its transport.
type CallOutcome struct {
	OK         bool   `json:"ok"`
	ExitCode   int    `json:"exitCode"`
	Payload    any    `json:"payload"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Diagnostic any    `json:"diagnostic"`
}

// CodeLocalService supplies operations that cannot use their shared MCP handler unchanged.
type CodeLocalService interface {
	Surface(context.Context, map[string]any) CallOutcome
	Start(context.Context, map[string]any) CallOutcome
	CurrentUser(context.Context, map[string]any) CallOutcome
	NextID(context.Context, map[string]any) CallOutcome
	OntologyQuerySchema(context.Context, map[string]any) CallOutcome
	OntologyQuery(context.Context, map[string]any) CallOutcome
	OntologyReference(context.Context, map[string]any) CallOutcome
	OntologyAuthoringGuide(context.Context, map[string]any) CallOutcome
	OntologyInspect(context.Context, map[string]any) CallOutcome
	QueryRecipe(context.Context, map[string]any) CallOutcome
	View(context.Context, map[string]any) CallOutcome
	Validate(context.Context, map[string]any) CallOutcome
	CodeRationale(context.Context, map[string]any) CallOutcome
	RenameHeading(context.Context, map[string]any) CallOutcome
	NoteMove(context.Context, map[string]any) CallOutcome
}

type CodeLocalHandler func(CodeLocalService, context.Context, map[string]any) CallOutcome

// CallCodeLocal invokes the catalog-selected local override when one exists.
func CallCodeLocal(ctx context.Context, service CodeLocalService, name string, input map[string]any) (CallOutcome, bool) {
	descriptor, ok := descriptorForName(name, SurfaceCodeMode)
	if !ok || descriptor.CodeLocal == nil {
		return CallOutcome{}, false
	}
	return descriptor.CodeLocal(service, ctx, input), true
}
