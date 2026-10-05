package llm

import (
	"testing"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

func mockNoGlobalHome(t *testing.T) {
	t.Helper()
	original := vaultconfig.UserHomeDirectory
	home := t.TempDir()
	vaultconfig.UserHomeDirectory = func() (string, error) { return home, nil }
	t.Cleanup(func() {
		vaultconfig.UserHomeDirectory = original
	})
}

func TestResolveProfile_Defaults(t *testing.T) {
	mockNoGlobalHome(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	resolved, err := ResolveProfile("instant", nil, nil, "")
	if err != nil {
		t.Fatalf("expected defaults to resolve: %v", err)
	}
	if resolved.Provider != "openai" {
		t.Fatalf("expected provider openai, got %s", resolved.Provider)
	}
	if resolved.Model != "gpt-5.2" {
		t.Fatalf("expected model gpt-5.2, got %s", resolved.Model)
	}
	if resolved.ReasoningEffort != ReasoningNone {
		t.Fatalf("expected reasoning none, got %s", resolved.ReasoningEffort)
	}
}

func TestResolveProfile_FastAlias(t *testing.T) {
	mockNoGlobalHome(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	resolved, err := ResolveProfile("fast", nil, nil, "")
	if err != nil {
		t.Fatalf("expected fast to resolve: %v", err)
	}
	if resolved.Model != "gpt-5.2" {
		t.Fatalf("expected model gpt-5.2, got %s", resolved.Model)
	}
}

func TestResolveProfile_LocalOverridesGlobal(t *testing.T) {
	mockNoGlobalHome(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	global := &Config{Profiles: map[string]Profile{
		"instant": {Model: "gpt-5.1"},
	}}
	local := &Config{Profiles: map[string]Profile{
		"instant": {Model: "gpt-4.1"},
	}}
	resolved, err := ResolveProfile("instant", local, global, "")
	if err != nil {
		t.Fatalf("expected merge to resolve: %v", err)
	}
	if resolved.Model != "gpt-4.1" {
		t.Fatalf("expected local model override, got %s", resolved.Model)
	}
}

func TestParseModelSpec_WithProviderAndEffort(t *testing.T) {
	spec, err := ParseModelSpec("openai/gpt-5.2:high")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if spec.Provider != "openai" {
		t.Fatalf("expected provider openai, got %s", spec.Provider)
	}
	if spec.Model != "gpt-5.2" {
		t.Fatalf("expected model gpt-5.2, got %s", spec.Model)
	}
	if spec.ReasoningEffort != ReasoningHigh {
		t.Fatalf("expected reasoning high, got %s", spec.ReasoningEffort)
	}
}

func TestParseModelSpec_WithExtraHighEffort(t *testing.T) {
	spec, err := ParseModelSpec("openai/gpt-5.5:xhigh")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if spec.ReasoningEffort != ReasoningXHigh {
		t.Fatalf("expected reasoning xhigh, got %s", spec.ReasoningEffort)
	}
}

func TestParseModelSpec_InvalidEffort(t *testing.T) {
	_, err := ParseModelSpec("gpt-5.2:turbo")
	if err == nil {
		t.Fatalf("expected invalid effort error")
	}
}

func TestResolveProfile_ModelOverrideInference(t *testing.T) {
	mockNoGlobalHome(t)
	t.Setenv("GEMINI_API_KEY", "test-key")
	resolved, err := ResolveProfile("instant", nil, nil, "gemini/gemini-1.5:low")
	if err != nil {
		t.Fatalf("expected override to resolve: %v", err)
	}
	if resolved.Provider != "gemini" {
		t.Fatalf("expected provider gemini, got %s", resolved.Provider)
	}
	if resolved.ReasoningEffort != ReasoningLow {
		t.Fatalf("expected reasoning low, got %s", resolved.ReasoningEffort)
	}
}

func TestResolveProfile_MissingAPIKey(t *testing.T) {
	mockNoGlobalHome(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("CEREBRAS_API_KEY", "")
	t.Setenv("ATOMIC_RHIZOME_KEY", "") // Disable team keys
	_, err := ResolveProfile("instant", nil, nil, "")
	if err == nil {
		t.Fatalf("expected missing api key error")
	}
}
