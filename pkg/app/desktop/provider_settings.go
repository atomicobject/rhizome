package desktop

import (
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/app/credentials"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// SetProviderAccess changes only user-global preferences or saved key aliases.
// The native caller holds its app-wide user-config lock across this operation.
func (s *Service) SetProviderAccess(req Request) (CredentialStatus, error) {
	switch req.Provider {
	case "voyage", "typesafe":
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
	_, err := repoconfig.LoadCliConfig(s.env, true)
	if err != nil {
		return CredentialStatus{}, problem("credential_config", "Cannot read user configuration. Repair it before changing provider settings.")
	}
	section := "provider_access"
	changes := map[string]*string{req.Provider: nil}
	if req.Operation == "provider-preference-set" {
		if req.Preference != "" {
			changes[req.Provider] = &req.Preference
		}
	} else {
		section = "env"
		need, _ := credentials.NeedForProvider(req.Provider, "")
		changes = map[string]*string{need.Key: nil}
		for _, name := range need.AltKeys {
			changes[name] = nil
		}
		if req.Operation == "provider-key-set" {
			changes[need.Key] = &req.Key
		}
	}
	if err := repoconfig.PatchCliConfigValues(s.env, section, changes); err != nil {
		return CredentialStatus{}, problem("credential_config", "Cannot save provider settings. Check your user configuration and file permissions.")
	}
	return s.CredentialStatus()
}
