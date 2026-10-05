package config

import (
	"os"
	"path/filepath"
	"testing"
)

func unsetDotEnvKey(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDotEnv_QuotedComments(t *testing.T) {
	for _, tt := range []struct {
		name, value, want string
	}{
		{"escaped quote before hash", `"say \" # hello"`, `say " # hello`},
		{"comment after escaped quote", `"say \"hello" # comment`, `say "hello`},
		{"escaped backslash before closing quote", `"hello\\" # comment`, `hello\`},
		{"single quoted literal backslash", `'hello\' # comment`, `hello\`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const key = "RZ_DOTENV_QUOTED_COMMENT_TEST"
			unsetDotEnvKey(t, key)
			path := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(path, []byte(key+"="+tt.value+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadDotEnv(path); err != nil {
				t.Fatal(err)
			}
			if got := os.Getenv(key); got != tt.want {
				t.Fatalf("loaded value = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadDotEnv_SetsUnsetOnly(t *testing.T) {
	t.Setenv("EXISTING", "keep")
	for _, key := range []string{"NEW_KEY", "QUOTED", "SINGLE"} {
		unsetDotEnvKey(t, key)
	}
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("EXISTING=override\nNEW_KEY=hello\nQUOTED=\"a b\"\nSINGLE='x y'\n"), 0600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	keys, err := LoadDotEnv(envPath)
	if err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if got := os.Getenv("EXISTING"); got != "keep" {
		t.Fatalf("EXISTING = %q, want %q", got, "keep")
	}
	if got := os.Getenv("NEW_KEY"); got != "hello" {
		t.Fatalf("NEW_KEY = %q, want %q", got, "hello")
	}
	if got := os.Getenv("QUOTED"); got != "a b" {
		t.Fatalf("QUOTED = %q, want %q", got, "a b")
	}
	if got := os.Getenv("SINGLE"); got != "x y" {
		t.Fatalf("SINGLE = %q, want %q", got, "x y")
	}
	if len(keys) != 3 {
		t.Fatalf("set keys = %v, want 3 keys", keys)
	}
}

func TestLoadDotEnvUpwards_LoadsNearest(t *testing.T) {
	for _, key := range []string{"A", "B"} {
		unsetDotEnvKey(t, key)
	}
	root := t.TempDir()
	project := filepath.Join(root, "project")
	child := filepath.Join(project, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("A=from_root\n"), 0600); err != nil {
		t.Fatalf("write root .env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("A=from_project\nB=from_project\n"), 0600); err != nil {
		t.Fatalf("write project .env: %v", err)
	}

	keys, err := LoadDotEnvUpwards(child)
	if err != nil {
		t.Fatalf("LoadDotEnvUpwards: %v", err)
	}
	if got := os.Getenv("A"); got != "from_project" {
		t.Fatalf("A = %q, want %q", got, "from_project")
	}
	if got := os.Getenv("B"); got != "from_project" {
		t.Fatalf("B = %q, want %q", got, "from_project")
	}
	if len(keys) != 2 {
		t.Fatalf("set keys = %v, want 2 keys", keys)
	}
}

func TestLoadDotEnvUpwards_IgnoresInvalidLinesInDotEnv(t *testing.T) {
	for _, key := range []string{"GOOD", "ALSO_OK", "RZ_RHIZOME_DOTENV_TEST"} {
		unsetDotEnvKey(t, key)
	}
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("GOOD=ok\n\"private_key\": \"foo=bar\"\nALSO_OK=ok\n"), 0600); err != nil {
		t.Fatalf("write root .env: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755); err != nil {
		t.Fatalf("mkdir .rhizome: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".rhizome", ".env"), []byte("RZ_RHIZOME_DOTENV_TEST=from_rhizome\n"), 0600); err != nil {
		t.Fatalf("write .rhizome/.env: %v", err)
	}

	keys, err := LoadDotEnvUpwards(child)
	if err != nil {
		t.Fatalf("LoadDotEnvUpwards: %v", err)
	}
	if got := os.Getenv("GOOD"); got != "ok" {
		t.Fatalf("GOOD = %q, want %q", got, "ok")
	}
	if got := os.Getenv("ALSO_OK"); got != "ok" {
		t.Fatalf("ALSO_OK = %q, want %q", got, "ok")
	}
	if got := os.Getenv("RZ_RHIZOME_DOTENV_TEST"); got != "from_rhizome" {
		t.Fatalf("RZ_RHIZOME_DOTENV_TEST = %q, want %q", got, "from_rhizome")
	}
	if len(keys) != 3 {
		t.Fatalf("set keys = %v, want 3 keys", keys)
	}
}
