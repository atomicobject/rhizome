package cmd

import (
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/validate"
)

func renderNamespaceMutationText(result validate.NamespaceMutationResult) string {
	var out strings.Builder
	for _, recovered := range result.Recovered {
		if recovered.TransactionID == "" {
			fmt.Fprintf(&out, "Recovered earlier move: %s.\n", recovered.Decision)
		} else {
			fmt.Fprintf(&out, "Recovered move transaction %s: %s.\n", recovered.TransactionID, recovered.Decision)
		}
		if recovered.RecoveryPending {
			out.WriteString("Recovery is pending; preserve the journal and artifacts, and resolve the reported recovery conflict before retrying.\n")
		}
	}
	current := result.Current
	switch current.Decision {
	case validate.NamespaceCommitted:
		if current.RecoveryPending {
			fmt.Fprintf(&out, "Move transaction %s committed; recovery is pending. Do not repeat this move. Preserve the journal and artifacts and resolve the reported recovery conflict.\n", current.TransactionID)
		}
	case validate.NamespaceRestored:
		fmt.Fprintf(&out, "Move transaction %s restored the original files.\n", current.TransactionID)
		if current.RecoveryPending {
			out.WriteString("Recovery is pending; preserve the journal and artifacts, and resolve the reported recovery conflict before retrying.\n")
		}
	case validate.NamespaceUnresolved:
		fmt.Fprintf(&out, "Move transaction %s is unresolved. Do not repeat this move until recovery establishes its outcome. Preserve the files, journal and artifacts and resolve the reported recovery conflict.\n", current.TransactionID)
	case validate.NamespaceNotStarted:
		if len(result.Recovered) > 0 {
			out.WriteString("The current move did not start; review the recovered result and retry the request.\n")
		}
	}
	if current.Decision != validate.NamespaceNotStarted && current.RecoveryPending && current.ReceiptPath != "" {
		fmt.Fprintf(&out, "Receipt path: %s\n", current.ReceiptPath)
	}
	return out.String()
}
