package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

const devJSON = `{
  "mode": "dev",
  "listen": "127.0.0.1:0",
  "data_dir": "/tmp/team-api",
  "issuer": "http://127.0.0.1:9100",
  "client_id": "dev-client",
  "accepted_domains": ["AtomicObject.com"],
  "admins": ["Admin@atomicobject.com"],
  "upstreams": {"voyage": "http://localhost:9200"},
  "person_limits": {"requests": 5, "input_bytes": 6, "charge_credits": 7}
}`

const prodJSON = `{
  "mode": "production",
  "listen": ":8080",
  "data_dir": "/var/lib/team-api",
  "issuer": "https://accounts.google.com",
  "client_id": "123.apps.googleusercontent.com",
  "accepted_domains": ["atomicobject.com"],
  "sessions": {"access_ttl": "5m"}
}`

func TestParseDevAppliesDefaults(t *testing.T) {
	cfg, err := Parse([]byte(devJSON))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scopes != "openid email" || cfg.EligibilityMode != "allowlist" {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	s := cfg.Sessions
	if time.Duration(s.AccessTTL) != 10*time.Minute || time.Duration(s.IdleTTL) != 14*24*time.Hour ||
		time.Duration(s.AbsoluteTTL) != 30*24*time.Hour || time.Duration(s.RefreshGrace) != 120*time.Second {
		t.Fatalf("session defaults: %+v", s)
	}
	if cfg.AcceptedDomains[0] != "atomicobject.com" || cfg.Admins[0] != "admin@atomicobject.com" {
		t.Fatalf("domains and admins are not normalized: %v %v", cfg.AcceptedDomains, cfg.Admins)
	}
	if *cfg.MaxPersonLimits != (wire.DailyLimits{Requests: 50, InputBytes: 60, ChargeCredits: 70}) || *cfg.MaxGlobalLimits != (wire.DailyLimits{}) {
		t.Fatalf("limit ceilings: %+v %+v", cfg.MaxPersonLimits, cfg.MaxGlobalLimits)
	}
	if cfg.Concurrency.VoyageConcurrency != 4 {
		t.Fatalf("concurrency default: %+v", cfg.Concurrency)
	}
}

func TestParseProduction(t *testing.T) {
	cfg, err := Parse([]byte(prodJSON))
	if err != nil {
		t.Fatal(err)
	}
	if time.Duration(cfg.Sessions.AccessTTL) != 5*time.Minute {
		t.Fatalf("access_ttl = %v", cfg.Sessions.AccessTTL)
	}
}

func TestRejectsInvalidConfigs(t *testing.T) {
	cases := []struct {
		name, base, from, to, want string
	}{
		{"production non-Google issuer", prodJSON, `"https://accounts.google.com"`, `"http://127.0.0.1:9100"`, "production issuer"},
		{"production lookalike issuer", prodJSON, `"https://accounts.google.com"`, `"https://accounts.google.com/"`, "production issuer"},
		{"production upstream override", prodJSON, `"sessions"`, `"upstreams": {"voyage": "https://evil.example"}, "sessions"`, "development only"},
		{"production scopes", prodJSON, `"sessions"`, `"scopes": "openid email profile", "sessions"`, "production scopes"},
		{"dev non-loopback listen", devJSON, `"127.0.0.1:0"`, `"0.0.0.0:8080"`, "dev listen"},
		{"dev non-loopback issuer", devJSON, `"http://127.0.0.1:9100"`, `"http://idp.example.com"`, "dev issuer"},
		{"dev non-loopback upstream", devJSON, `"http://localhost:9200"`, `"http://10.0.0.5:9200"`, "dev upstream"},
		{"unknown mode", devJSON, `"dev"`, `"staging"`, "mode must be"},
		{"unknown field", devJSON, `"mode"`, `"surprise": 1, "mode"`, "unknown field"},
		{"missing domains", devJSON, `["AtomicObject.com"]`, `[]`, "accepted_domains"},
		{"negative limits", devJSON, `"mode"`, `"global_limits": {"requests": -1}, "mode"`, "must not be negative"},
		{"bad duration", devJSON, `"mode"`, `"sessions": {"idle_ttl": 5}, "mode"`, "duration"},
		{"idle beyond absolute", devJSON, `"mode"`, `"sessions": {"idle_ttl": "800h"}, "mode"`, "idle_ttl <= absolute_ttl"},
		{"defaults above ceiling", devJSON, `"mode"`, `"max_person_limits": {"requests": 4, "input_bytes": 6, "charge_credits": 7}, "mode"`, "exceed max_person_limits"},
		{"two documents", devJSON + "{}", "", "", "more than one"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.base
			if tc.from != "" {
				if !strings.Contains(data, tc.from) {
					t.Fatalf("fixture does not contain %s", tc.from)
				}
				data = strings.Replace(data, tc.from, tc.to, 1)
			}
			_, err := Parse([]byte(data))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestLoadReadsClientSecretFromEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(devJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ClientSecretEnv, "synthetic-public-client-secret")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientSecret != "synthetic-public-client-secret" {
		t.Fatal("client secret not taken from the environment")
	}
}
