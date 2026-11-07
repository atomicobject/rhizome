package repoexec

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Selection must not depend on whether this machine has Rhizome Dev.
	_ = os.Setenv(DevAppEnv, "")
	os.Exit(m.Run())
}
