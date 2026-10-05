package agentcode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultExecuteTimeout = 30 * time.Second
	MaxExecuteTimeout     = 24 * time.Hour
	MaxExecuteCodeBytes   = 1 << 20

	maxExecuteOutputBytes = MaxExecuteCodeBytes + 128<<10
	maxExecuteDiagnostics = 64 << 10
)

// ExecuteOptions describes one managed JavaScript task. ReadWrite governs the
// code-mode connection only; JavaScript still runs with the caller's host
// permissions.
type ExecuteOptions struct {
	Code string
	// Input is an optional JSON value the script reads as input; null when absent.
	Input          json.RawMessage
	ExecutablePath string
	VaultPath      string
	SessionID      string
	ReadWrite      bool
	Timeout        time.Duration
}

type ExecuteError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type ExecuteResult struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Error       *ExecuteError   `json:"error,omitempty"`
	Diagnostics []string        `json:"diagnostics,omitempty"`
}

type executeInput struct {
	Code           string          `json:"code"`
	Input          json.RawMessage `json:"input,omitempty"`
	ExecutablePath string          `json:"executablePath"`
	VaultPath      string          `json:"vaultPath"`
	SessionID      string          `json:"sessionId,omitempty"`
	ReadWrite      bool            `json:"readWrite"`
	Module         string          `json:"module"`
	Operations     []string        `json:"operations"`
}

type executeEnvelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Error       *ExecuteError   `json:"error"`
	Diagnostics []string        `json:"diagnostics"`
}

// Execute evaluates a script against every cataloged code-mode operation. It
// owns the generated client and its child process for the duration of the call.
func Execute(ctx context.Context, options ExecuteOptions) (ExecuteResult, error) {
	timeout, nodePath, err := validateExecuteOptions(options)
	if err != nil {
		return ExecuteResult{}, err
	}
	executionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	description, err := Describe(supportedOperationNames())
	if err != nil {
		return ExecuteResult{}, fmt.Errorf("describe managed operations: %w", err)
	}
	module := generateModule(description)
	input, err := json.Marshal(executeInput{
		Code: options.Code, Input: options.Input, ExecutablePath: options.ExecutablePath, VaultPath: options.VaultPath,
		SessionID: options.SessionID, ReadWrite: options.ReadWrite, Module: module, Operations: description.Selected,
	})
	if err != nil {
		return ExecuteResult{}, fmt.Errorf("encode execution input: %w", err)
	}
	if err := executionCtx.Err(); err != nil {
		return interruptedExecution(err, nil, false), nil
	}
	command := exec.Command(nodePath, "--input-type=module", "--eval", executeRunner)
	configureExecuteCommand(command)
	command.Dir = options.VaultPath
	command.Stdin = bytes.NewReader(input)
	command.WaitDelay = time.Second
	var stdout, stderr cappedBuffer
	stdout.limit, stderr.limit = maxExecuteOutputBytes, maxExecuteDiagnostics
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		return ExecuteResult{}, fmt.Errorf("node_runtime_unavailable: start Node.js runtime %q: %w", nodePath, err)
	}
	defer func() { _ = terminateExecuteProcessTree(command) }()
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	var waitErr error
	select {
	case waitErr = <-wait:
	case <-executionCtx.Done():
		_ = terminateExecuteProcessTree(command)
		<-wait
		return interruptedExecution(executionCtx.Err(), stderr.diagnostics(), true), nil
	}
	if stdout.overflow {
		return failedExecution("execution_output_limit_exceeded", "execution output exceeded its limit", stderr.diagnostics()), nil
	}
	result, parseErr := parseExecuteResult(stdout.Bytes())
	if parseErr != nil {
		if waitErr != nil {
			return failedExecution("node_execution_failed", "Node.js execution failed", stderr.diagnostics()), nil
		}
		return failedExecution("invalid_execution_output", parseErr.Error(), stderr.diagnostics()), nil
	}
	result.Diagnostics = appendBoundedDiagnostics(result.Diagnostics, stderr.diagnostics())
	if waitErr != nil && result.OK {
		return failedExecution("node_execution_failed", "Node.js execution failed", result.Diagnostics), nil
	}
	if encoded, err := json.Marshal(result); err != nil || len(encoded) > maxExecuteOutputBytes {
		return failedExecution("execution_output_limit_exceeded", "encoded execution result exceeded its limit", nil), nil
	}
	return result, nil
}

func validateExecuteOptions(options ExecuteOptions) (time.Duration, string, error) {
	if len(options.Code) > MaxExecuteCodeBytes {
		return 0, "", fmt.Errorf("code exceeds %d byte limit", MaxExecuteCodeBytes)
	}
	if err := ValidateExecuteInput(options.Input); err != nil {
		return 0, "", err
	}
	if !filepath.IsAbs(options.ExecutablePath) || !filepath.IsAbs(options.VaultPath) {
		return 0, "", errors.New("executablePath and vaultPath must be absolute")
	}
	if info, err := os.Stat(options.ExecutablePath); err != nil || info.IsDir() {
		return 0, "", fmt.Errorf("executablePath is not an executable file: %q", options.ExecutablePath)
	}
	if info, err := os.Stat(options.VaultPath); err != nil || !info.IsDir() {
		return 0, "", fmt.Errorf("vaultPath is not a directory: %q", options.VaultPath)
	}
	if strings.Contains(options.SessionID, "\x00") {
		return 0, "", errors.New("sessionId must not contain NUL")
	}
	timeout := options.Timeout
	if timeout == 0 {
		timeout = DefaultExecuteTimeout
	}
	if timeout < time.Millisecond || timeout > MaxExecuteTimeout {
		return 0, "", fmt.Errorf("timeout must be between 1ms and %s", MaxExecuteTimeout)
	}
	resolved, err := exec.LookPath("node")
	if err != nil {
		return 0, "", fmt.Errorf("node_runtime_unavailable: Node.js runtime %q was not found: %w", "node", err)
	}
	return timeout, resolved, nil
}

// ValidateExecuteInput accepts an absent input or one JSON value within the
// script size limit.
func ValidateExecuteInput(input json.RawMessage) error {
	if len(input) == 0 {
		return nil
	}
	if len(input) > MaxExecuteCodeBytes {
		return fmt.Errorf("input exceeds %d byte limit", MaxExecuteCodeBytes)
	}
	if !json.Valid(input) {
		return errors.New("input must be one JSON value")
	}
	return nil
}

func interruptedExecution(err error, diagnostics []string, started bool) ExecuteResult {
	code, message := "cancelled", "execution was cancelled"
	if errors.Is(err, context.DeadlineExceeded) {
		code, message = "deadline_exceeded", "execution exceeded its deadline"
	}
	result := failedExecution(code, message, diagnostics)
	result.Error.Details = map[string]any{"mayHaveExecuted": started}
	return result
}

func failedExecution(code, message string, diagnostics []string) ExecuteResult {
	return ExecuteResult{OK: false, Result: json.RawMessage("null"), Error: &ExecuteError{
		Code: code, Message: message, Details: map[string]any{"mayHaveExecuted": true},
	}, Diagnostics: diagnostics}
}

func parseExecuteResult(output []byte) (ExecuteResult, error) {
	output = bytes.TrimSpace(output)
	if len(output) == 0 {
		return ExecuteResult{}, errors.New("Node.js runtime emitted no result")
	}
	var envelope executeEnvelope
	if err := json.Unmarshal(output, &envelope); err != nil {
		return ExecuteResult{}, fmt.Errorf("decode Node.js result: %w", err)
	}
	if envelope.Result == nil {
		envelope.Result = json.RawMessage("null")
	}
	if !json.Valid(envelope.Result) {
		return ExecuteResult{}, errors.New("Node.js result contains invalid JSON")
	}
	return ExecuteResult{OK: envelope.OK, Result: envelope.Result, Error: envelope.Error, Diagnostics: trimDiagnostics(envelope.Diagnostics)}, nil
}

type cappedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (buffer *cappedBuffer) Write(value []byte) (int, error) {
	remaining := buffer.limit - buffer.Len()
	if remaining <= 0 {
		buffer.overflow = true
		return len(value), nil
	}
	if len(value) > remaining {
		buffer.overflow = true
		_, _ = buffer.Buffer.Write(value[:remaining])
		return len(value), nil
	}
	return buffer.Buffer.Write(value)
}

func (buffer *cappedBuffer) diagnostics() []string {
	if buffer.Len() == 0 {
		return nil
	}
	return []string{string(buffer.Bytes())}
}

func trimDiagnostics(values []string) []string {
	return appendBoundedDiagnostics(nil, values)
}

func appendBoundedDiagnostics(existing, values []string) []string {
	all := append(append([]string(nil), existing...), values...)
	bounded := make([]string, 0, len(all))
	remaining := maxExecuteDiagnostics
	for _, value := range all {
		if remaining <= 0 {
			break
		}
		if len(value) > remaining {
			value = value[:remaining]
		}
		bounded = append(bounded, value)
		remaining -= len(value)
	}
	return bounded
}
