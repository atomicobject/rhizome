// Package wire is the v1 contract between Rhizome clients and the Atomic team
// API gateway: routes, headers, error codes, JSON bodies, and request ceilings.
// It imports only the standard library so the server can depend on it without
// pulling in Rhizome's application, vault, SQLite, or CGO packages.
package wire

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// APIVersion is reported by /v1/meta. Changes within v1 are additive.
const APIVersion = 1

// Routes. Provider routes forward to fixed upstream URLs.
const (
	RouteHealth        = "/healthz"
	RouteMeta          = "/v1/meta"
	RouteAuthGoogle    = "/v1/auth/google"
	RouteAuthRefresh   = "/v1/auth/refresh"
	RouteAuthLogout    = "/v1/auth/logout"
	RouteSession       = "/v1/session"
	RouteVoyageEmbed   = "/v1/providers/voyage/v1/embeddings"
	RouteTypeSafeEval  = "/v1/providers/typesafe/v1/systemone"
	RouteTypeSafeModel = "/v1/providers/typesafe/v1/models"
)

// Admin routes require a session whose person the server's configuration
// names as an admin. The admin list is never editable through the API.
const (
	RouteAdminUsage     = "/v1/admin/usage"                // GET ?date=YYYY-MM-DD (UTC, default today)
	RouteAdminPeople    = "/v1/admin/people"               // GET
	RouteAdminPerson    = "/v1/admin/people/{sub}"         // PATCH AdminPersonUpdate
	RouteAdminSessions  = "/v1/admin/sessions"             // GET ?sub=
	RouteAdminRevoke    = "/v1/admin/sessions/{id}/revoke" // POST
	RouteAdminRevokeAll = "/v1/admin/people/{sub}/revoke"  // POST: every session of the person
	RouteAdminConfig    = "/v1/admin/config"               // GET, PATCH AdminConfigUpdate
	RouteAdminAudit     = "/v1/admin/audit"                // GET ?limit=
)

// Headers.
const (
	// HeaderRequestID is set on every gateway response.
	HeaderRequestID = "Rhizome-Request-Id"
	// HeaderGatewayError is set only on gateway-originated errors, never on
	// upstream responses, so clients can tell the two apart.
	HeaderGatewayError = "Rhizome-Gateway-Error"
	// HeaderIdempotencyKey identifies one logical paid request. It stays the
	// same across transport retries of that request.
	HeaderIdempotencyKey = "Idempotency-Key"
)

// Token prefixes. A token is <prefix><session id>.<base64url of 32 random bytes>.
const (
	AccessTokenPrefix  = "rzt_a_"
	RefreshTokenPrefix = "rzt_r_"
)

// Request ceilings, checked before admission. The size test in this package
// shows each one covers the largest request current Rhizome clients send.
const (
	MaxVoyageInputs          = 1000
	MaxVoyageOutputDimension = 1024
	MaxVoyageRequestBytes    = 32 << 20
	MaxTypeSafeRequestBytes  = 2 << 20
	MaxResponseBytes         = 32 << 20
	MaxAuthRequestBytes      = 16 << 10
)

// ErrorCode is the stable code in a gateway error envelope and in
// HeaderGatewayError.
type ErrorCode string

const (
	// Pre-admission: nothing reached the upstream, so one replay is safe.
	CodeAccessRefreshRequired ErrorCode = "access_refresh_required"

	// The session can no longer be used; the client clears it and asks the
	// person to sign in again.
	CodeSessionRevoked ErrorCode = "session_revoked"
	CodeSessionExpired ErrorCode = "session_expired"
	CodeRefreshReused  ErrorCode = "refresh_reused"
	CodeNotEligible    ErrorCode = "not_eligible"

	CodeRateLimited      ErrorCode = "rate_limited"
	CodeBudgetExhausted  ErrorCode = "budget_exhausted"
	CodeServiceDisabled  ErrorCode = "service_disabled"
	CodeUpstreamDown     ErrorCode = "upstream_unavailable"
	CodeOutcomeUnknown   ErrorCode = "upstream_outcome_unknown"
	CodeResultLost       ErrorCode = "result_unavailable"
	CodeInProgress       ErrorCode = "request_in_progress"
	CodeIdempotencyClash ErrorCode = "idempotency_conflict"
	CodeModelNotAllowed  ErrorCode = "model_not_allowed"
	CodeTooLarge         ErrorCode = "request_too_large"
	CodeBadRequest       ErrorCode = "bad_request"
	CodeUpgradeRequired  ErrorCode = "client_upgrade_required"
	CodeNotAdmin         ErrorCode = "not_admin"
	CodeNotFound         ErrorCode = "not_found"
	// CodeInternal is a gateway fault before admission, such as an
	// unavailable state store. Nothing reached the upstream.
	CodeInternal ErrorCode = "internal_error"
)

// ErrorBody is the JSON body of every gateway-originated error.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable"`
}

// Meta is the unauthenticated /v1/meta response. It tells the client where to
// send the browser: Google's authorization endpoint when deployed, the
// development identity provider in local runs. The client trusts it because
// it trusts the origin the person signed in to.
type Meta struct {
	APIVersion            int    `json:"api_version"`
	ClientID              string `json:"client_id"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	Scopes                string `json:"scopes"`
	HostedDomainHint      string `json:"hd_hint,omitempty"`
	MinClientVersion      string `json:"min_client_version"`
}

// GoogleAuthRequest exchanges an authorization code obtained through the
// loopback redirect. The gateway performs the token exchange with the
// identity provider and verifies the ID token.
type GoogleAuthRequest struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	RedirectURI  string `json:"redirect_uri"`
	Nonce        string `json:"nonce"`
	DeviceName   string `json:"device_name"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// SessionGrant is returned by sign-in and by every refresh. Refresh rotates
// both tokens; SessionExpiresAt never moves later.
type SessionGrant struct {
	SessionID        string    `json:"session_id"`
	Email            string    `json:"email"`
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	SessionExpiresAt time.Time `json:"session_expires_at"`
	Limits           Limits    `json:"limits"`
}

// Limits tells clients how many paid calls to keep in flight.
type Limits struct {
	VoyageConcurrency   int `json:"voyage_concurrency"`
	TypeSafeConcurrency int `json:"typesafe_concurrency"`
}

// SessionInfo is the /v1/session response. It never contains tokens.
type SessionInfo struct {
	SessionID        string          `json:"session_id"`
	Email            string          `json:"email"`
	DeviceName       string          `json:"device_name"`
	SessionExpiresAt time.Time       `json:"session_expires_at"`
	Limits           Limits          `json:"limits"`
	Today            UsageWithLimits `json:"today"`
}

// DailyLimits are the hard per-UTC-day admission limits for one person or for
// everyone together. ChargeCredits are integer units of the conservative
// charge reserved before each paid call. Zero means no calls are admitted.
type DailyLimits struct {
	Requests      int64 `json:"requests"`
	InputBytes    int64 `json:"input_bytes"`
	ChargeCredits int64 `json:"charge_credits"`
}

// DailyUsage counts what was admitted and reserved on one UTC day.
// ReservedCredits stay held until settlement proves part of them unused.
type DailyUsage struct {
	Requests        int64 `json:"requests"`
	InputBytes      int64 `json:"input_bytes"`
	ReservedCredits int64 `json:"reserved_credits"`
	SettledCredits  int64 `json:"settled_credits"`
	Unknown         int64 `json:"unknown_outcomes"`
}

// AdminUsage is the /v1/admin/usage response.
type AdminUsage struct {
	Date   string          `json:"date"`
	Global UsageWithLimits `json:"global"`
	People []PersonUsage   `json:"people"`
}

type UsageWithLimits struct {
	Usage  DailyUsage  `json:"usage"`
	Limits DailyLimits `json:"limits"`
}

type PersonUsage struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	UsageWithLimits
}

// PersonStatus values.
const (
	PersonActive   = "active"
	PersonDisabled = "disabled"
)

type AdminPerson struct {
	Sub            string       `json:"sub"`
	Email          string       `json:"email"`
	HostedDomain   string       `json:"hd"`
	Status         string       `json:"status"`
	Admin          bool         `json:"admin"`
	CreatedAt      time.Time    `json:"created_at"`
	LimitsOverride *DailyLimits `json:"limits_override,omitempty"`
}

// AdminPersonUpdate changes only the fields that are set. ClearLimits removes
// a per-person override so the defaults apply again.
type AdminPersonUpdate struct {
	Status      *string      `json:"status,omitempty"`
	Limits      *DailyLimits `json:"limits,omitempty"`
	ClearLimits bool         `json:"clear_limits,omitempty"`
}

type AdminSession struct {
	SessionID        string     `json:"session_id"`
	Sub              string     `json:"sub"`
	DeviceName       string     `json:"device_name"`
	ClientVersion    string     `json:"client_version"`
	CreatedAt        time.Time  `json:"created_at"`
	LastUsedAt       time.Time  `json:"last_used_at"`
	SessionExpiresAt time.Time  `json:"session_expires_at"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
}

// Eligibility modes.
const (
	EligibilityAll       = "all"
	EligibilityAllowlist = "allowlist"
)

// AdminConfig is the runtime configuration operators may change. Accepted
// domains and the admin list come from server configuration, read-only here.
type AdminConfig struct {
	PaidCallsEnabled bool        `json:"paid_calls_enabled"`
	VoyageEnabled    bool        `json:"voyage_enabled"`
	TypeSafeEnabled  bool        `json:"typesafe_enabled"`
	EligibilityMode  string      `json:"eligibility_mode"`
	Allowlist        []string    `json:"allowlist"`
	PersonLimits     DailyLimits `json:"person_limits"`
	GlobalLimits     DailyLimits `json:"global_limits"`
	AcceptedDomains  []string    `json:"accepted_domains"`
	Admins           []string    `json:"admins"`
}

// AdminConfigUpdate changes only the fields that are set.
type AdminConfigUpdate struct {
	PaidCallsEnabled *bool        `json:"paid_calls_enabled,omitempty"`
	VoyageEnabled    *bool        `json:"voyage_enabled,omitempty"`
	TypeSafeEnabled  *bool        `json:"typesafe_enabled,omitempty"`
	EligibilityMode  *string      `json:"eligibility_mode,omitempty"`
	Allowlist        *[]string    `json:"allowlist,omitempty"`
	PersonLimits     *DailyLimits `json:"person_limits,omitempty"`
	GlobalLimits     *DailyLimits `json:"global_limits,omitempty"`
}

// AuditEntry records one admin change. It never contains tokens.
type AuditEntry struct {
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Action string    `json:"action"`
	Target string    `json:"target"`
	Detail string    `json:"detail,omitempty"`
}

// ErrSignInRequired means the stored session can no longer be used and the
// person must run `rzm auth login`. The message names a credential so callers
// that detect credential failures by text recognize it.
var ErrSignInRequired = errors.New("team API credential required: run rzm auth login")

// GatewayError is a gateway-originated error as a client sees it. Clients
// return it from their transport instead of passing the response on.
type GatewayError struct {
	Code       ErrorCode
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *GatewayError) Error() string {
	return fmt.Sprintf("team API %s (HTTP %d): %s", e.Code, e.Status, e.Message)
}

// Retryable reports whether the same logical request may be sent again after
// RetryAfter: only when the gateway guarantees nothing reached the provider.
func (e *GatewayError) Retryable() bool {
	return e.Code == CodeRateLimited || e.Code == CodeUpstreamDown || e.Code == CodeInProgress
}

// Unwrap makes errors.Is(err, ErrSignInRequired) true for codes that end the
// session.
func (e *GatewayError) Unwrap() error {
	switch e.Code {
	case CodeSessionRevoked, CodeSessionExpired, CodeRefreshReused, CodeNotEligible:
		return ErrSignInRequired
	}
	return nil
}

// Status is the HTTP status the gateway sends with each code.
func (c ErrorCode) Status() int {
	switch c {
	case CodeAccessRefreshRequired, CodeSessionRevoked, CodeSessionExpired, CodeRefreshReused:
		return http.StatusUnauthorized
	case CodeNotEligible, CodeNotAdmin:
		return http.StatusForbidden
	case CodeRateLimited, CodeBudgetExhausted:
		return http.StatusTooManyRequests
	case CodeServiceDisabled, CodeUpstreamDown:
		return http.StatusServiceUnavailable
	case CodeOutcomeUnknown, CodeResultLost, CodeInProgress, CodeIdempotencyClash:
		return http.StatusConflict
	case CodeTooLarge:
		return http.StatusRequestEntityTooLarge
	case CodeUpgradeRequired:
		return http.StatusUpgradeRequired
	case CodeNotFound:
		return http.StatusNotFound
	case CodeInternal:
		return http.StatusInternalServerError
	default: // CodeModelNotAllowed, CodeBadRequest
		return http.StatusBadRequest
	}
}
