package agentapi

import (
	"encoding/json"
	"fmt"
)

// codeInputFieldMetadata adds agent-facing meaning to the small schema subset
// emitted by code_contracts.go. It is descriptive only: ValidateCodeInput
// continues to own the executable input rules.
type codeInputFieldMetadata struct {
	Description string
	Default     any
	HasDefault  bool
}

var commonCodeInputFieldMetadata = map[string]codeInputFieldMetadata{
	"sessionId": {
		Description: "Optional session identifier. The initialized client or per-call CallOptions sessionId can supply or replace it.",
	},
	"budgetChars": {
		Description: "Optional response budget for operations that support a content budget; omission leaves the operation's runtime budget in effect.",
	},
}

func applyCodeContractMetadata(operation string, contract *CodeOperationContract, fields map[string]codeInputFieldMetadata, interpretation string) {
	var schema map[string]any
	if err := json.Unmarshal(contract.InputSchema, &schema); err != nil {
		panic(fmt.Sprintf("decode code contract input schema for %s: %v", operation, err))
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		panic(fmt.Sprintf("code contract input schema for %s has no properties", operation))
	}
	metadata := make(map[string]codeInputFieldMetadata, len(commonCodeInputFieldMetadata)+len(fields))
	for name, value := range commonCodeInputFieldMetadata {
		metadata[name] = value
	}
	for name, value := range fields {
		metadata[name] = value
	}
	for name, value := range metadata {
		property, ok := properties[name].(map[string]any)
		if !ok {
			panic(fmt.Sprintf("code contract metadata for %s references missing input %s", operation, name))
		}
		property["description"] = value.Description
		if value.HasDefault {
			property["default"] = value.Default
		}
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("encode code contract input schema for %s: %v", operation, err))
	}
	contract.InputSchema = payload
	if interpretation != "" {
		contract.Interpretation = append(contract.Interpretation, interpretation)
	}
}

func completeCodeDescriptor(d ToolDescriptor, contract *CodeOperationContract, leaves []string, fields map[string]codeInputFieldMetadata, interpretation string) ToolDescriptor {
	applyCodeContractMetadata(d.Name, contract, fields, interpretation)
	d.CodeContract = contract
	d.CodeCLILeaves = leaves
	d.Surfaces |= SurfaceCodeMode
	return d
}
