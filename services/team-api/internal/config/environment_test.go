package config

import (
	"strings"
	"testing"
)

func TestEnvironmentConfig(t *testing.T) {
	t.Setenv(JSONEnv, `{"mode":"production","listen":":8080","data_dir":"/data","issuer":"https://accounts.google.com","client_id":"synthetic.apps.googleusercontent.com","accepted_domains":["example.com"],"state":{"backend":"dynamodb","table":"synthetic-state","region":"us-east-2"},"trusted_proxy_cidrs":["10.0.0.0/24"]}`)
	t.Setenv(ClientSecretEnv, "synthetic-client-secret")
	cfg, err := LoadEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.State.Backend != "dynamodb" || cfg.ClientSecret != "synthetic-client-secret" || cfg.DiagnosticsOutput != "stdout" {
		t.Fatalf("environment config not applied")
	}
	t.Setenv(JSONEnv, `{"client_id":"private-client-value","unknown":"private-value"}`)
	_, err = LoadEnvironment()
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("must reject invalid config without disclosing it: %v", err)
	}
}
