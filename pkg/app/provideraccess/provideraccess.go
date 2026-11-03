// Package provideraccess is the one application factory that decides how
// Rhizome reaches Voyage and TypeSafe: a direct API key, the Atomic team
// gateway session, or the legacy encrypted key bundle. Provider libraries
// (pkg/embeddings, pkg/typesafe) receive the decision explicitly and never read
// the session store.
package provideraccess

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/pkg/teamkeys"
	"github.com/atomicobject/rhizome/pkg/typesafe"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

// Managed is a signed-in team session as provider code needs it. Transport
// adds the bearer token, refreshes, and returns gateway-originated errors as
// *wire.GatewayError.
type Managed interface {
	Origin() string
	Transport() http.RoundTripper
	Limits() wire.Limits
}

// Loader returns the stored session. ok is false when nobody is signed in; an
// error means the store could not be read (for example, a locked keychain).
type Loader func() (session Managed, ok bool, err error)

// TeamStatus describes the team session without any token.
type TeamStatus struct {
	SignedIn  bool       `json:"signedIn"`
	Email     string     `json:"email,omitempty"`
	Origin    string     `json:"origin,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// Source names where a provider's access comes from. It is never secret.
type Source string

const (
	SourceDirect Source = "direct"
	SourceTeam   Source = "team"
	SourceBundle Source = "bundle"
	SourceNone   Source = "none"
)

// Factory resolves provider access. The zero value has no session loader and
// therefore behaves exactly as Rhizome did before team sessions.
type Factory struct {
	Load Loader
	// Status reports the team session for status surfaces; nil means signed out.
	Status func() TeamStatus
	// DevBuild prefers a session whose origin is loopback over direct keys, so
	// a development rzm exercises the local gateway.
	DevBuild bool

	// bundle overrides the legacy bundle lookup in tests.
	bundle   func(env vaultconfig.Env, provider string) string
	recorder *runtimeRecorder
}

var installed atomic.Pointer[Factory]

// Install sets the process-wide factory. The application calls it once at
// startup, before constructing providers.
func Install(f Factory) {
	f.recorder = &runtimeRecorder{}
	installed.Store(&f)
}

// Default returns the installed factory, or the zero Factory.
func Default() Factory {
	if f := installed.Load(); f != nil {
		return *f
	}
	return Factory{}
}

type access struct {
	source  Source
	key     string
	managed Managed
	loadErr error
	report  Report
}

// resolve is shared by reports and constructors; only construction records an
// active route. Explicit preferences never fall through to another source.
func (f Factory) resolve(env vaultconfig.Env, provider, explicit string) access {
	var cfg vaultconfig.GlobalConfig
	var err error
	// An invocation without a home/config path cannot have a saved preference.
	// Preserve environment-only callers while refusing unreadable existing config.
	if _, _, pathErr := env.CliPath(); pathErr == nil {
		cfg, err = vaultconfig.LoadGlobalConfig(env, true)
	}
	report := Report{Provider: provider, Saved: savedKey(cfg, provider) != ""}
	if err != nil {
		report.Source, report.Reason = SourceNone, "configuration_unavailable"
		return access{source: SourceNone, report: report}
	}
	preference := cfg.ProviderAccess[provider]
	if preference != "" && preference != "server" && preference != "key" {
		report.Source, report.Reason = SourceNone, "invalid_preference"
		return access{source: SourceNone, report: report}
	}
	report.Preference = preference
	direct, provenance, envName := directKey(env, cfg, provider, explicit)
	loaded := false
	var session Managed
	var signedIn bool
	var loadErr error
	load := func() {
		if !loaded && f.Load != nil {
			session, signedIn, loadErr = f.Load()
		}
		loaded = true
	}
	result := func(source Source, key string, managed Managed) access {
		report.Source = source
		if source == SourceDirect {
			report.Key, report.EnvName = provenance, envName
		}
		if source == SourceTeam {
			report.Origin = managed.Origin()
		}
		return access{source: source, key: key, managed: managed, loadErr: loadErr, report: report}
	}
	if report.Preference == "server" {
		load()
		if signedIn && loadErr == nil && session != nil {
			return result(SourceTeam, "", session)
		}
		report.Reason = "server_signed_out"
		if loadErr != nil {
			report.Reason = "session_unavailable"
		}
		return result(SourceNone, "", nil)
	}
	if report.Preference == "key" {
		if direct != "" {
			return result(SourceDirect, direct, nil)
		}
		report.Reason = "key_missing"
		return result(SourceNone, "", nil)
	}
	if f.DevBuild {
		load()
		if signedIn && session != nil && isLoopbackOrigin(session.Origin()) {
			return result(SourceTeam, "", session)
		}
	}
	if direct != "" {
		return result(SourceDirect, direct, nil)
	}
	load()
	if signedIn && session != nil {
		return result(SourceTeam, "", session)
	}
	if key := f.bundleKey(env, provider); key != "" {
		return result(SourceBundle, key, nil)
	}
	report.Reason = "not_configured"
	if loadErr != nil {
		report.Reason = "session_unavailable"
	}
	return result(SourceNone, "", nil)
}

func providerKeyNames(provider string) []string {
	if provider == "voyage" {
		return []string{"RHIZOME_VOYAGE_API_KEY", "VOYAGE_API_KEY"}
	}
	return []string{"TYPESAFE_API_KEY"}
}

func savedKey(cfg vaultconfig.GlobalConfig, provider string) string {
	for _, name := range providerKeyNames(provider) {
		if key := strings.TrimSpace(cfg.Env[name]); key != "" {
			return key
		}
	}
	return ""
}

func directKey(env vaultconfig.Env, cfg vaultconfig.GlobalConfig, provider, explicit string) (string, string, string) {
	if key := strings.TrimSpace(explicit); key != "" {
		return key, "flag", ""
	}
	for _, name := range providerKeyNames(provider) {
		if value, _ := env.Lookup(name); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), "environment", name
		}
	}
	if key := savedKey(cfg, provider); key != "" {
		return key, "saved", ""
	}
	return "", "", ""
}

func (f Factory) bundleKey(env vaultconfig.Env, provider string) string {
	if f.bundle != nil {
		return f.bundle(env, provider)
	}
	if provider == "voyage" {
		return teamkeys.VoyageKey(env)
	}
	return teamkeys.TypeSafeKey(env)
}

// VoyageSource reports where Voyage access would come from.
func (f Factory) VoyageSource(env vaultconfig.Env) Source {
	return f.resolve(env, "voyage", "").source
}

// TypeSafeSource reports where TypeSafe access would come from.
func (f Factory) TypeSafeSource(env vaultconfig.Env) Source {
	return f.resolve(env, "typesafe", "").source
}

// SignedIn reports whether a team session is available.
func (f Factory) SignedIn() bool {
	if f.Load == nil {
		return false
	}
	_, ok, err := f.Load()
	return ok && err == nil
}

// TeamStatus reports the team session without tokens.
func (f Factory) TeamStatus() TeamStatus {
	if f.Status == nil {
		return TeamStatus{}
	}
	return f.Status()
}

// EmbeddingConfig resolves the provider configuration NewEmbeddings would use.
// Non-Voyage providers resolve exactly as embeddings.NewProviderForConfig does.
// apiKey is an explicit direct key (for example, a command-line flag).
func (f Factory) EmbeddingConfig(env vaultconfig.Env, cfg embeddings.Config, apiKey string) (embeddings.ProviderConfig, error) {
	if cfg.Provider != "voyage" {
		if apiKey = strings.TrimSpace(apiKey); apiKey == "" {
			apiKey = embeddings.ResolveAPIKeyForProvider(env, cfg.Provider)
		}
		return cfg.ProviderCfg(apiKey), nil
	}
	resolved := f.resolve(env, "voyage", apiKey)
	return embeddingConfig(cfg, resolved)
}

func embeddingConfig(cfg embeddings.Config, resolved access) (embeddings.ProviderConfig, error) {
	providerCfg := cfg.ProviderCfg(resolved.key)
	switch resolved.source {
	case SourceTeam:
		providerCfg.Managed = &embeddings.ManagedAccess{
			Origin:         resolved.managed.Origin(),
			Transport:      resolved.managed.Transport(),
			MaxConcurrency: resolved.managed.Limits().VoyageConcurrency,
		}
	case SourceNone:
		return providerCfg, resolved.unavailable("voyage provider requires api key: set VOYAGE_API_KEY or run `rzm auth login`")
	}
	return providerCfg, nil
}

// NewEmbeddings replaces embeddings.NewProviderForConfig for production
// callers. The caller owns the returned provider and closes it when it
// implements io.Closer, as before.
func (f Factory) NewEmbeddings(env vaultconfig.Env, cfg embeddings.Config, apiKey string) (embeddings.Provider, embeddings.ProviderConfig, error) {
	var resolved access
	var providerCfg embeddings.ProviderConfig
	var err error
	if cfg.Provider == "voyage" {
		f.attemptingVoyage(strings.TrimSpace(apiKey) != "")
		resolved = f.resolve(env, "voyage", apiKey)
		providerCfg, err = embeddingConfig(cfg, resolved)
	} else {
		providerCfg, err = f.EmbeddingConfig(env, cfg, apiKey)
	}
	if err != nil {
		return nil, providerCfg, err
	}
	provider, err := embeddings.NewConfiguredProvider(env, providerCfg)
	if err == nil && cfg.Provider == "voyage" {
		f.record(resolved.report, strings.TrimSpace(apiKey) != "")
	}
	return provider, providerCfg, err
}

// NewTypeSafe builds a TypeSafe client from the resolved access. options must
// not set a base URL or HTTP client; managed access supplies both.
func (f Factory) NewTypeSafe(env vaultconfig.Env, options ...typesafe.Option) (*typesafe.Client, error) {
	resolved := f.resolve(env, "typesafe", "")
	var client *typesafe.Client
	var err error
	switch resolved.source {
	case SourceTeam:
		managed := typesafe.Managed{
			Origin:         resolved.managed.Origin(),
			Transport:      resolved.managed.Transport(),
			MaxConcurrency: resolved.managed.Limits().TypeSafeConcurrency,
		}
		client, err = typesafe.NewClient("", append(options, typesafe.WithManaged(managed))...)
	case SourceNone:
		return nil, resolved.unavailable("TypeSafe credential unavailable: configure TYPESAFE_API_KEY, unlock the Atomic key bundle with ATOMIC_RHIZOME_KEY, or run `rzm auth login`")
	default:
		client, err = typesafe.NewClient(resolved.key, options...)
	}
	if err == nil {
		f.record(resolved.report, false)
	}
	return client, err
}

func (a access) unavailable(message string) error {
	switch a.report.Reason {
	case "configuration_unavailable", "invalid_preference":
		return errors.New("provider access configuration unavailable; check Provider access in Settings")
	}
	if a.report.Preference == "server" {
		message = "Atomic Object server unavailable: unlock the system keychain or run `rzm auth login`"
	} else if a.report.Preference == "key" {
		message = "provider key unavailable: add your key in Settings or configure the provider environment variable"
	}
	if a.loadErr != nil {
		return fmt.Errorf("%s (team session unavailable; unlock the system keychain or sign in again)", message)
	}
	return errors.New(message)
}

func isLoopbackOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
