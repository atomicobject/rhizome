package validate

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/sourcetxn"
)

// Check selection remains validate's responsibility; source recovery owns only
// file witnesses and durable evidence.
func validateSourceJournalChecks(manifest sourcetxn.RepairEvidence) error {
	for _, check := range manifest.Checks {
		canonical, ok := CanonicalCheck(check)
		if !ok || canonical != check {
			return fmt.Errorf("journal originating check is not canonical: %s", check)
		}
	}
	return nil
}
