package desktop

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/credentials"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

type CredentialStatus struct {
	Source    *repoconfig.CredentialSource `json:"source,omitempty"`
	Available bool                         `json:"available"`
	Ready     []string                     `json:"ready"`
}

func (s *Service) CredentialStatus() (CredentialStatus, error) {
	cfg, err := repoconfig.LoadCliConfig(s.env, true)
	if err != nil {
		return CredentialStatus{}, problem("credential_config", "Cannot read user configuration. Repair it before importing credentials.")
	}
	status := CredentialStatus{Source: cfg.CredentialSource, Available: credentials.FindOnePassword(s.env) != "", Ready: []string{}}
	for _, provider := range []string{"voyage", "typesafe", "openai", "cerebras", "anthropic"} {
		need, _ := credentials.NeedForProvider(provider, "")
		keys := append([]string{need.Key}, need.AltKeys...)
		for _, key := range keys {
			value, _ := s.env.Lookup(key)
			if value != "" || cfg.Env[key] != "" {
				status.Ready = append(status.Ready, need.Key)
				break
			}
		}
	}
	return status, nil
}

var importIDPattern = regexp.MustCompile(`^[0-9]+-[0-9]+$`)

func (s *Service) ImportCredentials(ctx context.Context, req Request) (CredentialStatus, error) {
	if req.Source == nil || !importIDPattern.MatchString(req.ImportID) {
		return CredentialStatus{}, problem("invalid_request", "Choose a 1Password account, Environment, and provider keys.")
	}
	if err := credentials.ValidateSource(*req.Source); err != nil {
		return CredentialStatus{}, problem("credential_import", err.Error())
	}
	// The native request owns this marker. Canceling or dropping it removes the
	// marker, canceling op and preventing a late result from being saved.
	marker := filepath.Join(s.stateDir, "credential-import-"+req.ImportID)
	if !importMarkerPresent(marker) {
		return CredentialStatus{}, problem("credential_canceled", "1Password import was canceled. Try again.")
	}
	ctx, cancel := context.WithCancel(vaultconfig.WithEnv(ctx, s.env))
	defer cancel()
	ctx = credentialImportContext{Context: ctx, marker: marker}
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !importMarkerPresent(marker) {
					cancel()
					return
				}
			}
		}
	}()
	if _, err := credentials.ImportEnvironment(ctx, *req.Source); err != nil {
		return CredentialStatus{}, problem("credential_import", err.Error())
	}
	return s.CredentialStatus()
}

// Err checks the native marker again at the config-save boundary.
type credentialImportContext struct {
	context.Context
	marker string
}

func (c credentialImportContext) Err() error {
	if !importMarkerPresent(c.marker) {
		return context.Canceled
	}
	return c.Context.Err()
}

func importMarkerPresent(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0400 != 0
}
