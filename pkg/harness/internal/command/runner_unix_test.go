//go:build unix

package command

import (
	"context"
	"testing"
	"time"
)

func TestRunReturnsWhileAGrandchildHoldsTheOutputPipe(t *testing.T) {
	start := time.Now()
	// The pipe wait is cut short, which exec may report as an error; only the
	// time matters here.
	_, _ = OSRunner{}.Run(context.Background(), Spec{Path: "/bin/sh", Args: []string{"-c", "sleep 60 & echo listed"}})
	if elapsed := time.Since(start); elapsed > waitDelay+10*time.Second {
		t.Fatalf("Run waited %s for a grandchild's pipe", elapsed)
	}
}
