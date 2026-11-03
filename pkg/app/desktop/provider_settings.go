package desktop

import (
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// SetProviderAccess changes only user-global preferences or saved key aliases.
// The native caller holds its app-wide user-config lock across this operation.
func (s *Service) SetProviderAccess(req Request) (CredentialStatus, error) {
	var aliases []string
	switch req.Provider {
	case "voyage":
		aliases = []string{"VOYAGE_API_KEY", "RHIZOME_VOYAGE_API_KEY"}
	case "typesafe":
		aliases = []string{"TYPESAFE_API_KEY"}
	default:
		return CredentialStatus{}, problem("invalid_request", "Choose Voyage or Jev.")
	}
	switch req.Operation {
	case "provider-preference-set":
		if req.Preference != "" && req.Preference != "server" && req.Preference != "key" {
			return CredentialStatus{}, problem("invalid_request", "Choose the server, your key, or automatic routing.")
		}
	case "provider-key-set":
		if req.Key == "" || len(req.Key) > 16384 || strings.TrimSpace(req.Key) != req.Key || strings.ContainsFunc(req.Key, unicode.IsControl) {
			return CredentialStatus{}, problem("invalid_request", "Enter a nonempty provider key without spaces at its ends or control characters.")
		}
	case "provider-key-clear":
	default:
		return CredentialStatus{}, problem("invalid_request", "Unknown provider settings operation.")
	}
	cfg, err := repoconfig.LoadCliConfig(s.env, true)
	if err != nil {
		return CredentialStatus{}, problem("credential_config", "Cannot read user configuration. Repair it before changing provider settings.")
	}
	if req.Operation == "provider-preference-set" {
		if cfg.ProviderAccess == nil {
			cfg.ProviderAccess = map[string]string{}
		}
		if req.Preference == "" {
			delete(cfg.ProviderAccess, req.Provider)
		} else {
			cfg.ProviderAccess[req.Provider] = req.Preference
		}
	} else {
		for _, name := range aliases {
			delete(cfg.Env, name)
		}
		if req.Operation == "provider-key-set" {
			if cfg.Env == nil {
				cfg.Env = map[string]string{}
			}
			cfg.Env[aliases[0]] = req.Key
		}
	}
	if err := repoconfig.SaveCliConfig(s.env, cfg); err != nil {
		return CredentialStatus{}, problem("credential_config", "Cannot save provider settings. Check user configuration permissions.")
	}
	return s.CredentialStatus()
}
