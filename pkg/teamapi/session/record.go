package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

// RecordVersion is the record format this client reads and writes. A change
// an older client must not drop on rewrite bumps it.
const RecordVersion = 1

// Record is the client's copy of one session, stored per OS user and origin.
// Generation identifies the sign-in that produced it: login publication
// increments it, refresh keeps it, so a late callback or a stale rejection
// can tell whether the account changed under it.
type Record struct {
	V                int         `json:"v"`
	Origin           string      `json:"origin"`
	Generation       uint64      `json:"generation"`
	SessionID        string      `json:"session_id"`
	Email            string      `json:"email"`
	AccessToken      string      `json:"access_token"`
	AccessExpiresAt  time.Time   `json:"access_expires_at"`
	RefreshToken     string      `json:"refresh_token"`
	SessionExpiresAt time.Time   `json:"session_expires_at"`
	Limits           wire.Limits `json:"limits"`
}

// errMalformed marks a record this version wrote badly or for another origin.
// Unlike a newer record, a new sign-in may replace it.
var errMalformed = fmt.Errorf("%w: stored session record is malformed", ErrStoreUnavailable)

// decodeRecord refuses a version it does not understand before reading any
// other field, so callers never rewrite a newer client's record.
func decodeRecord(data []byte, origin string) (Record, error) {
	var head struct {
		V int `json:"v"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return Record{}, errMalformed
	}
	if head.V > RecordVersion {
		return Record{}, ErrNewerRecord
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil || head.V != RecordVersion ||
		rec.Origin != origin || rec.AccessToken == "" || rec.RefreshToken == "" {
		return Record{}, errMalformed
	}
	return rec, nil
}

func recordFromGrant(origin string, generation uint64, grant wire.SessionGrant) (Record, error) {
	if grant.AccessToken == "" || grant.RefreshToken == "" || grant.SessionID == "" {
		return Record{}, errors.New("team session: gateway returned an incomplete session grant")
	}
	return Record{
		V: RecordVersion, Origin: origin, Generation: generation,
		SessionID: grant.SessionID, Email: grant.Email,
		AccessToken: grant.AccessToken, AccessExpiresAt: grant.AccessExpiresAt,
		RefreshToken: grant.RefreshToken, SessionExpiresAt: grant.SessionExpiresAt,
		Limits: grant.Limits,
	}, nil
}

// CanonicalOrigin validates a gateway origin and returns its canonical form,
// scheme://host[:port] in lower case without a default port. The origin must
// be an HTTPS root, or plain HTTP on a loopback host for local development.
func CanonicalOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	bad := func(reason string) (string, error) {
		return "", fmt.Errorf("team API origin %q: %s", raw, reason)
	}
	// url.Parse drops an empty query or fragment marker, so check the text.
	if strings.ContainsAny(raw, "?#") {
		return bad("must not have a query or fragment")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return bad(err.Error())
	}
	if u.Opaque != "" || u.Host == "" {
		return bad("must be an absolute URL such as https://team.example.com")
	}
	if u.User != nil {
		return bad("must not contain user information")
	}
	if u.Path != "" && u.Path != "/" {
		return bad("must not have a path")
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	switch {
	case scheme == "https":
	case scheme == "http" && isLoopbackHost(host):
	case scheme == "http":
		return bad("plain HTTP is allowed only for 127.0.0.1, localhost, or [::1]")
	default:
		return bad("scheme must be https")
	}
	return originFor(scheme, host, u.Port()), nil
}

func isLoopbackHost(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func originFor(scheme, host, port string) string {
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		return scheme + "://" + net.JoinHostPort(host, port)
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host
}

// requestOrigin is the canonical origin a request URL addresses.
func requestOrigin(u *url.URL) string {
	return originFor(strings.ToLower(u.Scheme), strings.ToLower(u.Hostname()), u.Port())
}
