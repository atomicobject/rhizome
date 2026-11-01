// Package config loads and validates the gateway's JSON configuration. A
// production configuration accepts only Google's issuer and the compiled-in
// provider URLs; a development configuration runs only on loopback.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

const (
	ModeDev        = "dev"
	ModeProduction = "production"

	GoogleIssuer  = "https://accounts.google.com"
	DefaultScopes = "openid email"

	// ClientSecretEnv optionally holds the OAuth client secret. Google treats
	// a Desktop client's secret as public metadata, so it is not a credential
	// the gateway relies on, but it still stays out of the config file.
	ClientSecretEnv = "RHIZOME_TEAM_API_CLIENT_SECRET"
)

type Config struct {
	Mode    string `json:"mode"`
	Listen  string `json:"listen"`
	DataDir string `json:"data_dir"`

	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"-"`
	Scopes       string `json:"scopes"`
	HDHint       string `json:"hd_hint"`

	AcceptedDomains []string `json:"accepted_domains"`
	Admins          []string `json:"admins"`
	// EligibilityMode and Allowlist seed the runtime configuration on first
	// start only; admins change them through the API afterwards. The seeded
	// allowlist always includes the admins so they can sign in.
	EligibilityMode string   `json:"eligibility_mode"`
	Allowlist       []string `json:"allowlist"`

	PersonLimits wire.DailyLimits `json:"person_limits"`
	GlobalLimits wire.DailyLimits `json:"global_limits"`
	// MaxPersonLimits and MaxGlobalLimits cap what the admin API may set, so
	// a stolen admin session cannot raise spending without bound. Only this
	// file can raise them. They default to ten times the defaults.
	MaxPersonLimits *wire.DailyLimits `json:"max_person_limits"`
	MaxGlobalLimits *wire.DailyLimits `json:"max_global_limits"`
	// Concurrency is what sessions advertise to clients; the traffic side
	// enforces the same values.
	Concurrency wire.Limits `json:"concurrency"`

	Sessions         Sessions  `json:"sessions"`
	MinClientVersion string    `json:"min_client_version"`
	Upstreams        Upstreams `json:"upstreams"`
	DiagnosticsDir   string    `json:"diagnostics_dir"`
}

type Sessions struct {
	AccessTTL    Duration `json:"access_ttl"`
	IdleTTL      Duration `json:"idle_ttl"`
	AbsoluteTTL  Duration `json:"absolute_ttl"`
	RefreshGrace Duration `json:"refresh_grace"`
}

// Upstreams override the compiled-in provider base URLs. Development only.
type Upstreams struct {
	Voyage   string `json:"voyage,omitempty"`
	TypeSafe string `json:"typesafe,omitempty"`
}

// Duration is a time.Duration written as a Go duration string ("10m").
type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string like \"10m\": %w", err)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// Load reads path, applies defaults, takes the client secret from the
// environment, and validates the result.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg, err := Parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	cfg.ClientSecret = os.Getenv(ClientSecretEnv)
	return cfg, nil
}

// Parse decodes one JSON document with unknown fields rejected, applies
// defaults, and validates it.
func Parse(data []byte) (Config, error) {
	var cfg Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	if dec.More() {
		return Config{}, errors.New("invalid config: more than one JSON document")
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Scopes == "" {
		c.Scopes = DefaultScopes
	}
	if c.EligibilityMode == "" {
		c.EligibilityMode = wire.EligibilityAllowlist
	}
	if c.Concurrency == (wire.Limits{}) {
		c.Concurrency = wire.Limits{VoyageConcurrency: 4, TypeSafeConcurrency: 4}
	}
	def := func(d *Duration, v time.Duration) {
		if *d == 0 {
			*d = Duration(v)
		}
	}
	def(&c.Sessions.AccessTTL, 10*time.Minute)
	def(&c.Sessions.IdleTTL, 14*24*time.Hour)
	def(&c.Sessions.AbsoluteTTL, 30*24*time.Hour)
	def(&c.Sessions.RefreshGrace, 120*time.Second)
	times10 := func(l wire.DailyLimits) *wire.DailyLimits {
		return &wire.DailyLimits{Requests: l.Requests * 10, InputBytes: l.InputBytes * 10, ChargeCredits: l.ChargeCredits * 10}
	}
	if c.MaxPersonLimits == nil {
		c.MaxPersonLimits = times10(c.PersonLimits)
	}
	if c.MaxGlobalLimits == nil {
		c.MaxGlobalLimits = times10(c.GlobalLimits)
	}
	c.AcceptedDomains = lower(c.AcceptedDomains)
	c.Admins = lower(c.Admins)
	c.Allowlist = lower(c.Allowlist)
}

// Validate refuses any configuration the gateway must not start with.
func (c Config) Validate() error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if c.Listen == "" {
		fail("listen is required")
	}
	if c.DataDir == "" {
		fail("data_dir is required")
	}
	if c.ClientID == "" {
		fail("client_id is required")
	}
	if len(c.AcceptedDomains) == 0 {
		fail("accepted_domains must name at least one hosted domain")
	}
	if c.EligibilityMode != wire.EligibilityAll && c.EligibilityMode != wire.EligibilityAllowlist {
		fail("eligibility_mode must be %q or %q", wire.EligibilityAll, wire.EligibilityAllowlist)
	}
	for name, l := range map[string]wire.DailyLimits{"person_limits": c.PersonLimits, "global_limits": c.GlobalLimits} {
		if l.Requests < 0 || l.InputBytes < 0 || l.ChargeCredits < 0 {
			fail("%s must not be negative", name)
		}
	}
	if c.MaxPersonLimits == nil || c.MaxGlobalLimits == nil {
		fail("max_person_limits and max_global_limits are required")
	} else {
		if !Within(c.PersonLimits, *c.MaxPersonLimits) {
			fail("person_limits exceed max_person_limits")
		}
		if !Within(c.GlobalLimits, *c.MaxGlobalLimits) {
			fail("global_limits exceed max_global_limits")
		}
	}
	if c.Concurrency.VoyageConcurrency <= 0 || c.Concurrency.TypeSafeConcurrency <= 0 {
		fail("concurrency values must be positive")
	}
	s := c.Sessions
	if s.AccessTTL <= 0 || s.IdleTTL <= 0 || s.AbsoluteTTL <= 0 || s.RefreshGrace <= 0 {
		fail("session lifetimes must be positive")
	}
	if s.IdleTTL > s.AbsoluteTTL || s.AccessTTL > s.IdleTTL {
		fail("session lifetimes must satisfy access_ttl <= idle_ttl <= absolute_ttl")
	}

	switch c.Mode {
	case ModeProduction:
		if c.Issuer != GoogleIssuer {
			fail("production issuer must be exactly %s, got %q", GoogleIssuer, c.Issuer)
		}
		if c.Scopes != DefaultScopes {
			fail("production scopes must be %q", DefaultScopes)
		}
		if c.Upstreams != (Upstreams{}) {
			fail("upstream overrides are development only")
		}
	case ModeDev:
		if host, _, err := net.SplitHostPort(c.Listen); err != nil || !loopbackHost(host) {
			fail("dev listen address must be loopback (127.0.0.1, localhost, [::1]), got %q", c.Listen)
		}
		if err := loopbackURL(c.Issuer); err != nil {
			fail("dev issuer: %v", err)
		}
		for name, u := range map[string]string{"voyage": c.Upstreams.Voyage, "typesafe": c.Upstreams.TypeSafe} {
			if u == "" {
				continue
			}
			if err := loopbackURL(u); err != nil {
				fail("dev upstream %s: %v", name, err)
			}
		}
	default:
		fail("mode must be %q or %q, got %q", ModeDev, ModeProduction, c.Mode)
	}
	return errors.Join(errs...)
}

// Within reports whether every field of l is between zero and max.
func Within(l, max wire.DailyLimits) bool {
	return l.Requests >= 0 && l.InputBytes >= 0 && l.ChargeCredits >= 0 &&
		l.Requests <= max.Requests && l.InputBytes <= max.InputBytes && l.ChargeCredits <= max.ChargeCredits
}

func loopbackURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("%q is not an absolute http(s) URL", raw)
	}
	if !loopbackHost(u.Hostname()) {
		return fmt.Errorf("%q must be on a loopback host (127.0.0.1, localhost, [::1])", raw)
	}
	return nil
}

func loopbackHost(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func lower(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			out = append(out, v)
		}
	}
	return out
}
