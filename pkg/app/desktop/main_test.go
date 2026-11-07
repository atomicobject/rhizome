package desktop

import (
	"os"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
)

func TestMain(m *testing.M) {
	// Selection must not depend on whether this machine has Rhizome Dev.
	_ = os.Setenv(repoexec.DevAppEnv, "")
	os.Exit(m.Run())
}
