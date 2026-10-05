package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/cli"
)

type filesCursor struct {
	Inputs             []string `json:"i,omitempty"`
	Offset             int      `json:"o"`
	MaxDepth           int      `json:"d,omitempty"`
	SkipAnchors        bool     `json:"sa,omitempty"`
	SkipEmbeds         bool     `json:"se,omitempty"`
	IncludeContent     bool     `json:"c,omitempty"`
	CompressContent    bool     `json:"cc,omitempty"`
	Intent             string   `json:"in,omitempty"`
	IncludeFrontmatter bool     `json:"f,omitempty"`
	IncludeBacklinks   bool     `json:"b,omitempty"`
	AbsolutePaths      bool     `json:"a,omitempty"`
	Dedupe             *bool    `json:"dd,omitempty"`
	SuppressTags       []string `json:"st,omitempty"`
	NoSuppress         bool     `json:"ns,omitempty"`
	BudgetChars        int      `json:"bc,omitempty"`
	Limit              int      `json:"l,omitempty"`
}

// FilesRequestPlanFacts are the normalized files request details that select
// one-shot runtime capabilities. They intentionally omit response settings.
type FilesRequestPlanFacts struct {
	MaxDepth       int
	HasPathInputs  bool
	OnlyFileInputs bool
	FileInputs     []string
}

// NormalizeFilesRequestPlan decodes files continuation state before runtime
// composition. The shared files handler remains the authority for all cursor
// fields; planning needs only traversal depth and code-path eligibility.
func NormalizeFilesRequestPlan(args map[string]any) (FilesRequestPlanFacts, error) {
	var (
		inputs   []string
		maxDepth int
	)
	rawToken, _ := args["continuationToken"].(string)
	if strings.TrimSpace(rawToken) != "" {
		cursor, err := decodeFilesCursor(rawToken)
		if err != nil {
			return FilesRequestPlanFacts{}, err
		}
		inputs = cursor.Inputs
		maxDepth = cursor.MaxDepth
	} else {
		var err error
		inputs, err = filesRequestInputs(args)
		if err != nil {
			return FilesRequestPlanFacts{}, err
		}
		maxDepth = filesRequestMaxDepth(args)
	}

	parsedInputs, expr, err := actions.ParseInputsWithExpression(inputs)
	if err != nil {
		return FilesRequestPlanFacts{}, err
	}
	info := actions.AnalyzeExpression(expr)
	return FilesRequestPlanFacts{
		MaxDepth:       maxDepth,
		HasPathInputs:  filesPathInputsCanEnumerateCode(parsedInputs, info),
		OnlyFileInputs: len(parsedInputs) > 0 && len(info.FileInputs) == len(parsedInputs),
		FileInputs:     append([]string(nil), info.FileInputs...),
	}, nil
}

func filesPathInputsCanEnumerateCode(inputs []actions.ListInput, info actions.ExpressionInfo) bool {
	if !filesHasPathInputs(inputs) {
		return false
	}
	if !info.HasOr && !info.HasNot && (info.HasTag || info.HasProperty) {
		// This matches FilesTool: an AND-only tag/property expression cannot
		// select code paths, so it must not open the CodeIndex for planning.
		return false
	}
	return true
}

func filesRequestInputs(args map[string]any) ([]string, error) {
	rawInputs, ok := args["inputs"]
	if !ok {
		return nil, fmt.Errorf("inputs parameter is required and must be an array")
	}
	switch values := rawInputs.(type) {
	case []string:
		return append([]string(nil), values...), nil
	case []interface{}:
		inputs := make([]string, len(values))
		for i, value := range values {
			input, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("all inputs must be strings")
			}
			inputs[i] = input
		}
		return inputs, nil
	default:
		return nil, fmt.Errorf("inputs parameter is required and must be an array")
	}
}

func filesRequestMaxDepth(args map[string]any) int {
	switch value := args["maxDepth"].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

func encodeFilesCursor(cur filesCursor) (string, error) {
	cur.Offset = max(0, cur.Offset)
	cur.MaxDepth = max(0, cur.MaxDepth)
	cur.Limit = max(0, cur.Limit)
	cur.BudgetChars = max(0, cur.BudgetChars)

	outInputs := make([]string, 0, len(cur.Inputs))
	for _, in := range cur.Inputs {
		in = strings.TrimSpace(in)
		if in == "" {
			continue
		}
		outInputs = append(outInputs, in)
	}
	cur.Inputs = outInputs

	if len(cur.Inputs) == 0 {
		return "", fmt.Errorf("cursor requires inputs")
	}
	b, err := json.Marshal(cur)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func decodeFilesCursor(raw string) (filesCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return filesCursor{}, fmt.Errorf("empty cursor")
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return filesCursor{}, fmt.Errorf("decode cursor: %w", err)
	}
	var cur filesCursor
	if err := json.Unmarshal(b, &cur); err != nil {
		return filesCursor{}, fmt.Errorf("unmarshal cursor: %w", err)
	}
	cur.Offset = max(0, cur.Offset)
	cur.MaxDepth = max(0, cur.MaxDepth)
	cur.Limit = max(0, cur.Limit)
	cur.BudgetChars = max(0, cur.BudgetChars)
	if cur.Dedupe == nil {
		defaultDedupe := true
		cur.Dedupe = &defaultDedupe
	}
	if len(cur.Inputs) == 0 {
		return filesCursor{}, fmt.Errorf("invalid cursor: missing inputs")
	}
	return cur, nil
}
