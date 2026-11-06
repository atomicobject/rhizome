package session

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

func TestSessionGrantRecordFitsCredentialManager(t *testing.T) {
	// Gateway tokens contain a 22-character session id and 43-character secret.
	id := strings.Repeat("s", 22)
	record := Record{
		V: RecordVersion, Origin: "https://team.example.invalid", Generation: math.MaxUint64,
		SessionID: id, Email: strings.Repeat("u", 64) + "@example.invalid",
		AccessToken:      wire.AccessTokenPrefix + id + "." + strings.Repeat("a", 43),
		RefreshToken:     wire.RefreshTokenPrefix + id + "." + strings.Repeat("r", 43),
		AccessExpiresAt:  time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC),
		SessionExpiresAt: time.Date(2026, 12, 5, 12, 0, 0, 0, time.UTC),
		Limits:           wire.Limits{VoyageConcurrency: math.MaxInt32, TypeSafeConcurrency: math.MaxInt32},
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 2560 {
		t.Fatalf("session record is %d bytes; Windows Credential Manager supports 2560", len(data))
	}
}
