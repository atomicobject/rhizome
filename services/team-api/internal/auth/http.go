package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

// WithRequestID sets wire.HeaderRequestID on every response unless an outer
// handler already did.
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ensureRequestID(w)
		next.ServeHTTP(w, r)
	})
}

func ensureRequestID(w http.ResponseWriter) {
	if w.Header().Get(wire.HeaderRequestID) == "" {
		w.Header().Set(wire.HeaderRequestID, randomString(16))
	}
}

// WriteError writes a gateway-originated error envelope.
func WriteError(w http.ResponseWriter, code wire.ErrorCode, message string) {
	ensureRequestID(w)
	w.Header().Set(wire.HeaderGatewayError, string(code))
	retryable := (&wire.GatewayError{Code: code}).Retryable()
	writeJSON(w, code.Status(), wire.ErrorBody{Error: wire.ErrorDetail{Code: code, Message: message, Retryable: retryable}})
}

// WriteJSON writes v with status 200.
func WriteJSON(w http.ResponseWriter, v any) { writeJSON(w, http.StatusOK, v) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	ensureRequestID(w)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// DecodeJSON reads exactly one JSON document of at most wire.MaxAuthRequestBytes
// into v, rejecting unknown fields. On failure it writes the error and
// returns false.
// bodyReadTimeout bounds reading one auth or admin request body. A variable
// for tests.
var bodyReadTimeout = 10 * time.Second

func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	// Sign-in and refresh are unauthenticated, so a stalled body must not hold
	// the connection: bound the read as the paid routes do.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(bodyReadTimeout))
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, wire.MaxAuthRequestBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		WriteError(w, wire.CodeTooLarge, "request body is too large")
		return false
	}
	if err != nil {
		WriteError(w, wire.CodeBadRequest, "request body could not be read")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil || dec.More() {
		WriteError(w, wire.CodeBadRequest, "request body must be one JSON object with known fields")
		return false
	}
	return true
}

// Tokens are <prefix><session id>.<base64url of 32 random bytes>. Only the
// SHA-256 of the whole token is stored.
func newToken(prefix, sessionID string) (token string, hash []byte) {
	token = prefix + sessionID + "." + randomString(32)
	return token, hashToken(token)
}

// parseToken returns the session ID of a well-formed token with prefix.
func parseToken(raw, prefix string) (string, bool) {
	rest, ok := strings.CutPrefix(raw, prefix)
	if !ok {
		return "", false
	}
	id, secret, ok := strings.Cut(rest, ".")
	if !ok || id == "" || strings.ContainsAny(id, ".") {
		return "", false
	}
	b, err := base64.RawURLEncoding.DecodeString(secret)
	return id, err == nil && len(b) == 32
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func hashEqual(a, b []byte) bool {
	return len(a) == sha256.Size && subtle.ConstantTimeCompare(a, b) == 1
}

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func bearer(r *http.Request) string {
	v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}

// clientVersion returns <version> from "User-Agent: rzm/<version> ...".
func clientVersion(r *http.Request) string {
	first, _, _ := strings.Cut(r.UserAgent(), " ")
	v, ok := strings.CutPrefix(first, "rzm/")
	if !ok {
		return ""
	}
	return v
}

// versionAllowed reports whether version meets min. "dev" always passes; an
// unknown or unparsable version fails once a minimum is set. Pre-release and
// build suffixes are ignored, so 1.2.0-rc1 counts as 1.2.0.
func versionAllowed(version, min string) bool {
	if min == "" || version == "dev" {
		return true
	}
	have, ok1 := parseVersion(version)
	want, ok2 := parseVersion(min)
	if !ok1 || !ok2 {
		return false
	}
	for i := range have {
		if have[i] != want[i] {
			return have[i] > want[i]
		}
	}
	return true
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
