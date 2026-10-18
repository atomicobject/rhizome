// Package sourcetxn owns durable, witness-checked vault file publication.
package sourcetxn

import (
	"os"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// Context binds source operations to the caller's actual vault writer lease.
type writerContext struct {
	VaultPath          string
	Lease              *indexlock.Lease
	onlyDirectory      string
	includeQuarantined bool
	role               WriterRole
}

func (c writerContext) canonicalCheck(check string) (string, bool) { return check, check != "" }

type CompletionArtifact struct {
	Path    string
	Content []byte `json:"-"`
}

const repairSpecialMode = os.ModeSetuid | os.ModeSetgid | os.ModeSticky

const repairCompletionDirectory = ".rhizome/edit-receipts"
