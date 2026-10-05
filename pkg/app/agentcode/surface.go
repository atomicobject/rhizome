// Package agentcode discovers and executes cataloged agent operations through
// a bounded JavaScript client and persistent stdio service.
package agentcode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
)

const (
	// FormatVersion governs the compact startup catalog. Describe has a separate
	// version because its batch metadata is deliberately excluded from startup.
	FormatVersion         = "1"
	DescribeFormatVersion = "2"
	GeneratorVersion      = "4"
)

type OperationIndex struct {
	Name    string   `json:"name"`
	Method  string   `json:"method"`
	Summary string   `json:"summary"`
	Effects []string `json:"effects"`
}

type SurfaceResponse struct {
	FormatVersion string           `json:"formatVersion"`
	VersionHash   string           `json:"versionHash"`
	Operations    []OperationIndex `json:"operations"`
	Examples      []string         `json:"examples"`
}

type OperationDescription struct {
	Name           string          `json:"name"`
	Method         string          `json:"method"`
	AgentCommand   string          `json:"agentCommand"`
	Summary        string          `json:"summary"`
	Effects        []string        `json:"effects"`
	InputSchema    json.RawMessage `json:"inputSchema,omitempty"`
	OutputSchema   json.RawMessage `json:"outputSchema,omitempty"`
	Example        map[string]any  `json:"example"`
	Interpretation []string        `json:"interpretation"`
}

type DescribeResponse struct {
	FormatVersion   string                 `json:"formatVersion"`
	ContractHash    string                 `json:"contractHash"`
	Invocation      InvocationMetadata     `json:"invocation"`
	Outcome         OutcomeMetadata        `json:"outcome"`
	Selected        []string               `json:"selected"`
	Operations      []OperationDescription `json:"operations"`
	SchemaSelection string                 `json:"schemaSelection,omitempty"`
}

// SelectSchemas projects discovery without changing the full selected-contract
// hash used by generated clients. Invocation and outcome guidance remain intact.
func (r DescribeResponse) SelectSchemas(selection string) (DescribeResponse, error) {
	switch selection {
	case "", "both":
		return r, nil
	case "input", "output":
	default:
		return DescribeResponse{}, fmt.Errorf("schema selection must be input, output, or both")
	}
	r.SchemaSelection = selection
	r.Operations = append([]OperationDescription(nil), r.Operations...)
	for i := range r.Operations {
		if selection == "input" {
			r.Operations[i].OutputSchema = nil
		} else {
			r.Operations[i].InputSchema = nil
		}
	}
	return r, nil
}

func Surface() SurfaceResponse {
	response, err := Describe(supportedOperationNames())
	if err != nil {
		panic(err)
	}
	operations := make([]OperationIndex, len(response.Operations))
	for i, operation := range response.Operations {
		operations[i] = OperationIndex{Name: operation.Name, Method: operation.Method, Summary: operation.Summary, Effects: operation.Effects}
	}
	return SurfaceResponse{
		FormatVersion: FormatVersion,
		VersionHash:   response.ContractHash,
		Operations:    operations,
		Examples: []string{
			"rzm agent code describe --operation file_context --operation files",
			`rzm agent code execute --code 'return await rzm.files({inputs: ["README.md"]});'`,
		},
	}
}

func supportedOperationNames() []string {
	descriptors := agentapi.CodeOperationDescriptors()
	names := make([]string, len(descriptors))
	for i, descriptor := range descriptors {
		names[i] = descriptor.Name
	}
	return names
}

func Describe(selected []string) (DescribeResponse, error) {
	names, err := normalizeSelection(selected)
	if err != nil {
		return DescribeResponse{}, err
	}
	operations := make([]OperationDescription, 0, len(names))
	for _, name := range names {
		descriptor, ok := agentapi.CodeOperationDescriptor(name)
		if !ok {
			return DescribeResponse{}, fmt.Errorf("unsupported code operation %q", name)
		}
		contract := descriptor.CodeContract
		operations = append(operations, OperationDescription{
			Name: name, Method: camelCase(name), AgentCommand: descriptor.AgentCommand, Summary: contract.Summary,
			Effects: append([]string(nil), contract.Effects...), InputSchema: cloneRaw(contract.InputSchema),
			OutputSchema: cloneRaw(contract.OutputSchema), Example: cloneMap(contract.Example),
			Interpretation: append([]string(nil), contract.Interpretation...),
		})
	}
	response := DescribeResponse{
		FormatVersion: DescribeFormatVersion,
		Invocation:    codeInvocationMetadata(),
		Outcome:       codeOutcomeMetadata(),
		Selected:      names,
		Operations:    operations,
	}
	response.ContractHash, err = descriptionHash(response)
	return response, err
}

func normalizeSelection(selected []string) ([]string, error) {
	if len(selected) == 0 {
		return nil, fmt.Errorf("at least one --operation is required")
	}
	seen := make(map[string]struct{}, len(selected))
	names := append([]string(nil), selected...)
	aliases := make(map[string]string)
	for _, descriptor := range agentapi.CodeOperationDescriptors() {
		aliases[descriptor.Name] = descriptor.Name
		aliases[camelCase(descriptor.Name)] = descriptor.Name
	}
	for i, input := range names {
		name := strings.TrimSpace(input)
		if name == "" {
			return nil, fmt.Errorf("operation name must not be empty")
		}
		canonical, ok := aliases[name]
		if !ok {
			return nil, fmt.Errorf("unsupported code operation %q; use rzm agent code surface to list operation names and methods", name)
		}
		name = canonical
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("duplicate code operation %q", name)
		}
		seen[name] = struct{}{}
		names[i] = name
	}
	sort.Strings(names)
	return names, nil
}

func descriptionHash(response DescribeResponse) (string, error) {
	response.ContractHash = ""
	payload, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("marshal code operation contracts: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func cloneRaw(value json.RawMessage) json.RawMessage { return append(json.RawMessage(nil), value...) }

func cloneMap(value map[string]any) map[string]any {
	payload, _ := json.Marshal(value)
	var cloned map[string]any
	_ = json.Unmarshal(payload, &cloned)
	return cloned
}
