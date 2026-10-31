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
	bundle func(env vaultconfig.Env, provider string) string
}

var installed atomic.Pointer[Factory]

// Install sets the process-wide factory. The application calls it once at
// startup, before constructing providers.
func Install(f Factory) { installed.Store(&f) }

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
}

// resolve applies the precedence: a direct key, then the team session, then
// the legacy bundle. A development build first takes a loopback session.
func (f Factory) resolve(env vaultconfig.Env, provider, directKey string) access {
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
	if f.DevBuild {
		if load(); signedIn && isLoopbackOrigin(session.Origin()) {
			return access{source: SourceTeam, managed: session}
		}
	}
	if directKey = strings.TrimSpace(directKey); directKey != "" {
		return access{source: SourceDirect, key: directKey}
	}
	if load(); signedIn {
		return access{source: SourceTeam, managed: session}
	}
	if key := f.bundleKey(env, provider); key != "" {
		return access{source: SourceBundle, key: key}
	}
	return access{source: SourceNone, loadErr: loadErr}
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

func voyageDirectKey(env vaultconfig.Env, explicit string) string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit
	}
	return vaultconfig.ResolveValue(env, "RHIZOME_VOYAGE_API_KEY", "VOYAGE_API_KEY")
}

func typeSafeDirectKey(env vaultconfig.Env) string {
	return vaultconfig.ResolveValue(env, "TYPESAFE_API_KEY")
}

// VoyageSource reports where Voyage access would come from.
func (f Factory) VoyageSource(env vaultconfig.Env) Source {
	return f.resolve(env, "voyage", voyageDirectKey(env, "")).source
}

// TypeSafeSource reports where TypeSafe access would come from.
func (f Factory) TypeSafeSource(env vaultconfig.Env) Source {
	return f.resolve(env, "typesafe", typeSafeDirectKey(env)).source
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
	resolved := f.resolve(env, "voyage", voyageDirectKey(env, apiKey))
	providerCfg := cfg.ProviderCfg(resolved.key)
	switch resolved.source {
	case SourceTeam:
		providerCfg.Managed = &embeddings.ManagedAccess{
			Origin:         resolved.managed.Origin(),
			Transport:      resolved.managed.Transport(),
			MaxConcurrency: resolved.managed.Limits().VoyageConcurrency,
		}
	case SourceNone:
		return providerCfg, unavailable("voyage provider requires api key: set VOYAGE_API_KEY or run `rzm auth login`", resolved.loadErr)
	}
	return providerCfg, nil
}

// NewEmbeddings replaces embeddings.NewProviderForConfig for production
// callers. The caller owns the returned provider and closes it when it
// implements io.Closer, as before.
func (f Factory) NewEmbeddings(env vaultconfig.Env, cfg embeddings.Config, apiKey string) (embeddings.Provider, embeddings.ProviderConfig, error) {
	providerCfg, err := f.EmbeddingConfig(env, cfg, apiKey)
	if err != nil {
		return nil, providerCfg, err
	}
	provider, err := embeddings.NewConfiguredProvider(env, providerCfg)
	return provider, providerCfg, err
}

// NewTypeSafe builds a TypeSafe client from the resolved access. options must
// not set a base URL or HTTP client; managed access supplies both.
func (f Factory) NewTypeSafe(env vaultconfig.Env, options ...typesafe.Option) (*typesafe.Client, error) {
	resolved := f.resolve(env, "typesafe", typeSafeDirectKey(env))
	switch resolved.source {
	case SourceTeam:
		managed := typesafe.Managed{
			Origin:         resolved.managed.Origin(),
			Transport:      resolved.managed.Transport(),
			MaxConcurrency: resolved.managed.Limits().TypeSafeConcurrency,
		}
		return typesafe.NewClient("", append(options, typesafe.WithManaged(managed))...)
	case SourceNone:
		return nil, unavailable("TypeSafe credential unavailable: configure TYPESAFE_API_KEY, unlock the Atomic key bundle with ATOMIC_RHIZOME_KEY, or run `rzm auth login`", resolved.loadErr)
	}
	return typesafe.NewClient(resolved.key, options...)
}

func unavailable(message string, loadErr error) error {
	if loadErr != nil {
		return fmt.Errorf("%s (team session unavailable: %w)", message, loadErr)
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
