package runtime

import (
	"context"
	"encoding/hex"
	"net/http"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
)

const DiagnosticTraceHeader = "X-Rhizome-Trace-ID"
const DiagnosticParentHeader = "X-Rhizome-Parent-Operation-ID"

func validDiagnosticID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// SetDiagnosticIdentity propagates only opaque operation identities. It never
// forwards credentials, client input, or diagnostic file paths.
func SetDiagnosticIdentity(request *http.Request, ctx context.Context) {
	op := diagnostics.OperationFromContext(ctx)
	if validDiagnosticID(op.TraceID) {
		request.Header.Set(DiagnosticTraceHeader, op.TraceID)
	}
	if validDiagnosticID(op.ID) {
		request.Header.Set(DiagnosticParentHeader, op.ID)
	}
}

// RequestDiagnosticIdentity is used only after control-token authorization.
func RequestDiagnosticIdentity(request *http.Request) (trace, parent string) {
	trace = request.Header.Get(DiagnosticTraceHeader)
	parent = request.Header.Get(DiagnosticParentHeader)
	if !validDiagnosticID(trace) {
		trace = ""
	}
	if !validDiagnosticID(parent) {
		parent = ""
	}
	return
}
