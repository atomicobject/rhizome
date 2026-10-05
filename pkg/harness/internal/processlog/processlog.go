package processlog

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"sync/atomic"
)

var fallbackID atomic.Uint32

func CorrelationID() string {
	var value [4]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("%08x", fallbackID.Add(1))
}

func Spawn(vendor, path, cwd, correlation string, pid int) {
	log.Printf("harness %s spawn path=%q pid=%d cwd=%q corr=%q", vendor, path, pid, cwd, correlation)
}

func Bind(vendor, correlation string, pid int, sessionID string) {
	log.Printf("harness %s bind corr=%q pid=%d session_id=%q", vendor, correlation, pid, sessionID)
}

func Exit(vendor, correlation string, pid int, state *os.ProcessState, err error) {
	if state == nil {
		log.Printf("harness %s exit corr=%q pid=%d exit_error=%q", vendor, correlation, pid, errorText(err))
		return
	}
	if code := state.ExitCode(); code >= 0 {
		log.Printf("harness %s exit corr=%q pid=%d exit_code=%d", vendor, correlation, pid, code)
		return
	}
	log.Printf("harness %s exit corr=%q pid=%d signal=%q", vendor, correlation, pid, state.String())
}

func DecodeFailure(vendor, correlation, sessionID string, err error) {
	log.Printf("harness %s protocol decode failure corr=%q session_id=%q: %v", vendor, correlation, sessionID, err)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
