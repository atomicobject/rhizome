package agentcode

const defaultCallTimeoutMS = 30_000

// InvocationMetadata describes the generated client protocol once for a
// describe response. It deliberately stays out of SurfaceResponse so startup
// discovery remains compact.
type InvocationMetadata struct {
	CallSignature   string              `json:"callSignature"`
	CallOptions     CallOptionsMetadata `json:"callOptions"`
	CallsSerialized bool                `json:"callsSerialized"`
	Concurrency     string              `json:"concurrency"`
}

// CallOptionsMetadata documents options passed to generated JavaScript client
// methods. They are transport controls, rather than operation input fields.
type CallOptionsMetadata struct {
	TimeoutMSDefault int    `json:"timeoutMsDefault"`
	TimeoutMS        string `json:"timeoutMs"`
	Signal           string `json:"signal"`
	SessionID        string `json:"sessionId"`
}

// OutcomeMetadata distinguishes normal operation outcomes from client,
// protocol, and transport errors that reject the JavaScript promise.
type OutcomeMetadata struct {
	ResolvedFields []OutcomeFieldMetadata `json:"resolvedFields"`
	Evidence       string                 `json:"evidence"`
	Throws         string                 `json:"throws"`
}

type OutcomeFieldMetadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func codeInvocationMetadata() InvocationMetadata {
	return InvocationMetadata{
		CallSignature:   "rzm.method(input, options?)",
		CallsSerialized: false,
		Concurrency:     "Client defaults to 8 concurrent calls (createClient concurrency: 1..32); host runs at most 32. Provider/ignore calls are independent, audited existing-index reads share access, and live vault refreshes/mutations take exclusive access. Await dependent operations. Unacknowledged cancellation holds its slot until a reply or close.",
		CallOptions: CallOptionsMetadata{
			TimeoutMSDefault: defaultCallTimeoutMS,
			TimeoutMS:        "Optional 1..300000 ms per-call deadline. Queue wait counts toward it; it is independent of the whole code-execution timeout and is sent in the call envelope, never inside operation input JSON.",
			Signal:           "Optional AbortSignal. It cancels locally while queued and requests cancellation after dispatch; it stays in the JavaScript client and is not part of operation input.",
			SessionID:        "Optional non-empty session override for this call.",
		},
	}
}

func codeOutcomeMetadata() OutcomeMetadata {
	return OutcomeMetadata{
		ResolvedFields: []OutcomeFieldMetadata{
			{Name: "ok", Description: "True when the operation completed with exitCode 0."},
			{Name: "exitCode", Description: "Operation exit status; a nonzero code is a resolved failed outcome."},
			{Name: "payload", Description: "Operation result described by that operation's outputSchema; it may be null on failure."},
			{Name: "stdout", Description: "Operation text output; empty when the result is already carried by payload, which is never repeated here."},
			{Name: "stderr", Description: "Operation error text when the operation emits it."},
			{Name: "diagnostic", Description: "Structured diagnostic evidence when available."},
		},
		Evidence: "Payload-specific warnings, truncation, continuation, and status fields remain operation-specific evidence; inspect them before treating a payload as complete.",
		Throws:   "The Promise rejects with CodeModeError {code, message, details} for client input/options validation, cancellation, deadline, protocol, transport, or server failures. When present, details.mayHaveExecuted reports execution uncertainty; true means inspect state before retrying, and its absence is not proof nothing ran. Those errors do not return CallOutcome.",
	}
}
