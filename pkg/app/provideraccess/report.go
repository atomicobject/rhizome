package provideraccess

import (
	"sync"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

// Report describes one routing decision without retaining credentials. Key is
// provenance only (flag, environment, or saved), never the key or its digest.
type Report struct {
	Provider   string `json:"provider"`
	Preference string `json:"preference,omitempty"`
	Source     Source `json:"source"`
	Key        string `json:"key,omitempty"`
	EnvName    string `json:"envName,omitempty"`
	Saved      bool   `json:"saved"`
	Origin     string `json:"origin,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// RuntimeReport distinguishes an actually constructed connection from what a
// new construction in this running process would use. Older runtime health
// answers omit this field, which consumers must treat as unknown.
type RuntimeReport struct {
	Provider        string  `json:"provider"`
	Active          *Report `json:"active,omitempty"`
	Next            Report  `json:"next"`
	RestartRequired bool    `json:"restartRequired"`
}

type runtimeRecorder struct {
	mu              sync.RWMutex
	active          map[string]Report
	voyageExplicit  bool
	voyageAttempted bool
}

func (f Factory) attemptingVoyage(explicit bool) {
	if f.recorder == nil {
		return
	}
	f.recorder.mu.Lock()
	defer f.recorder.mu.Unlock()
	f.recorder.voyageAttempted = true
	f.recorder.voyageExplicit = explicit
}

// Reports re-reads current user-global preferences using this invocation's
// environment. It neither constructs clients nor marks a route as active.
func (f Factory) Reports(env vaultconfig.Env) []Report {
	return []Report{f.resolve(env, "voyage", "").report, f.resolve(env, "typesafe", "").report}
}

func (f Factory) record(report Report, explicit bool) {
	if f.recorder == nil {
		return
	}
	f.recorder.mu.Lock()
	defer f.recorder.mu.Unlock()
	if f.recorder.active == nil {
		f.recorder.active = make(map[string]Report)
	}
	// Voyage connections belong to the runtime lifetime; Jev clients are
	// constructed for individual evaluations and the latest construction wins.
	if _, ok := f.recorder.active[report.Provider]; report.Provider == "voyage" && ok {
		return
	}
	f.recorder.active[report.Provider] = report
	if report.Provider == "voyage" {
		f.recorder.voyageExplicit = explicit
	}
}

// RuntimeReports resolves current access using this running process's inputs.
// Its recorder cannot inherit a desktop bridge's clients. Health uses the
// nonblocking CachedRuntimeReports wrapper rather than doing this I/O inline.
func (f Factory) RuntimeReports(env vaultconfig.Env) []RuntimeReport {
	result := make([]RuntimeReport, 0, 2)
	for _, provider := range []string{"voyage", "typesafe"} {
		var active Report
		var ok, explicit, attempted bool
		if f.recorder != nil {
			f.recorder.mu.RLock()
			active, ok = f.recorder.active[provider]
			explicit = provider == "voyage" && f.recorder.voyageExplicit
			attempted = f.recorder.voyageAttempted
			f.recorder.mu.RUnlock()
		}
		// Keep only the fact that the runtime constructor received an explicit
		// key. A sentinel projects its routing precedence without retaining the
		// actual key, a partial value, or a digest in diagnostic state.
		keyPresence := ""
		if explicit {
			keyPresence = "configured"
		}
		next := f.resolve(env, provider, keyPresence).report
		current := RuntimeReport{Provider: provider, Next: next}
		// Becoming configured after startup cannot create the missing Voyage
		// connection; only a fresh runtime can initialize it.
		current.RestartRequired = provider == "voyage" && attempted && next.Source != SourceNone
		if ok {
			current.Active = &active
			current.RestartRequired = provider == "voyage" && !sameRoute(active, next)
		}
		result = append(result, current)
	}
	return result
}

// sameRoute compares routing provenance. It deliberately makes no claim that
// two values saved in the same location contain identical key bytes.
func sameRoute(a, b Report) bool {
	return a.Source == b.Source && a.Origin == b.Origin && a.Key == b.Key && a.EnvName == b.EnvName
}
