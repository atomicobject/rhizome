package embeddings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

var (
	ErrOllamaNotInstalled = errors.New("ollama not installed")
	ErrOllamaNotRunning   = errors.New("ollama not running")
	ErrOllamaModelMissing = errors.New("ollama model missing")
)

type OllamaError struct {
	Kind   error
	Detail string
}

func (e OllamaError) Error() string {
	if e.Detail == "" {
		return "ollama error"
	}
	return e.Detail
}

func (e OllamaError) Unwrap() error {
	return e.Kind
}

func OllamaInstalled() bool {
	if strings.TrimSpace(os.Getenv("RHIZOME_DISABLE_OLLAMA")) != "" {
		return false
	}
	_, err := exec.LookPath("ollama")
	return err == nil
}

func OllamaInstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "Install Ollama (macOS: installer from ollama.com or Homebrew)."
	case "windows":
		return "Install Ollama (Windows: installer from ollama.com)."
	case "linux":
		return "Install Ollama (Linux: distro package manager or the official install script)."
	default:
		return "Install Ollama from ollama.com."
	}
}

func EnsureOllamaReady(ctx context.Context, model string, pull bool, out io.Writer) error {
	if !OllamaInstalled() {
		return OllamaError{Kind: ErrOllamaNotInstalled, Detail: "ollama executable not found in PATH"}
	}

	if ok := ollamaRunning(ctx); !ok {
		if err := startOllamaServer(); err != nil {
			return OllamaError{Kind: ErrOllamaNotRunning, Detail: fmt.Sprintf("failed to start ollama: %v", err)}
		}
		if ok := waitForOllama(ctx, 10*time.Second); !ok {
			return OllamaError{Kind: ErrOllamaNotRunning, Detail: "ollama did not become ready"}
		}
	}

	if strings.TrimSpace(model) == "" {
		return OllamaError{Kind: ErrOllamaModelMissing, Detail: "ollama model not configured"}
	}

	_, err := ollamaListModels(ctx)
	if err != nil {
		return OllamaError{Kind: ErrOllamaNotRunning, Detail: fmt.Sprintf("ollama list failed: %v", err)}
	}
	if hasModel(ctx, model) {
		return nil
	}
	if !pull {
		return OllamaError{Kind: ErrOllamaModelMissing, Detail: fmt.Sprintf("ollama model %q not found", model)}
	}
	if err := ollamaPull(ctx, model, out); err != nil {
		return OllamaError{Kind: ErrOllamaModelMissing, Detail: fmt.Sprintf("ollama pull %q failed: %v", model, err)}
	}
	if !hasModel(ctx, model) {
		return OllamaError{Kind: ErrOllamaModelMissing, Detail: fmt.Sprintf("ollama model %q still unavailable after pull", model)}
	}
	return nil
}

func ollamaRunning(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := ollamaListModels(ctx)
	return err == nil
}

func waitForOllama(ctx context.Context, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if ctx.Err() != nil {
			return false
		}
		if ollamaRunning(ctx) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func startOllamaServer() error {
	cmd := exec.Command("ollama", "serve")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Start()
}

func ollamaListModels(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "ollama", "list")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		out := strings.TrimSpace(buf.String())
		if out != "" {
			return nil, fmt.Errorf("%v: %s", err, out)
		}
		return nil, err
	}
	return parseOllamaList(buf.String()), nil
}

func parseOllamaList(out string) []string {
	lines := strings.Split(out, "\n")
	models := make([]string, 0, len(lines))
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if i == 0 && strings.HasPrefix(strings.ToUpper(line), "NAME") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		models = append(models, fields[0])
	}
	return models
}

func hasModel(ctx context.Context, target string) bool {
	models, err := ollamaListModels(ctx)
	if err != nil {
		return false
	}
	target = strings.TrimSpace(strings.ToLower(target))
	for _, m := range models {
		if strings.ToLower(strings.TrimSpace(m)) == target {
			return true
		}
	}
	return false
}

func ollamaPull(ctx context.Context, model string, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	fmt.Fprintf(out, "Ollama: pulling model %q (this may take a while)...\n", model)
	cmd := exec.CommandContext(ctx, "ollama", "pull", model)
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}
