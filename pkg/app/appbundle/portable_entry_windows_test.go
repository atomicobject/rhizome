package appbundle

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

const entryHelper = "TestPortableWindowsEntryHelper"

func windowsEntryFixture(t *testing.T) (string, string) {
	t.Helper()
	root, record, _ := portableFixture(t)
	self, err := os.Executable()
	require.NoError(t, err)
	data, err := os.ReadFile(self)
	require.NoError(t, err)
	hash, err := hashFile(self)
	require.NoError(t, err)
	image := ImagePath(root, hash)
	require.NoError(t, os.MkdirAll(filepath.Dir(image), 0o700))
	require.NoError(t, os.WriteFile(image, data, 0o700))
	record.ImageHash = hash
	writePortableFixture(t, root, record)
	entry, err := EntryPath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(entry), 0o700))
	require.NoError(t, os.WriteFile(entry, data, 0o700))
	return entry, filepath.Dir(root)
}

func TestPortableWindowsDispatcherPreservesStreamsArgumentsAndExit(t *testing.T) {
	entry, base := windowsEntryFixture(t)
	arguments := []string{"space ' ; λ", `a"quoted"value`, ""}
	args := append([]string{"-test.run=^" + entryHelper + "$", "--", "echo", base}, arguments...)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := ownedEntryCommand(ctx, entry, args...)
	command.Stdin = strings.NewReader("synthetic input\n")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 37, exit.ExitCode())
	var result struct {
		Arguments []string `json:"arguments"`
		Input     string   `json:"input"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result), stdout.String())
	require.Equal(t, arguments, result.Arguments)
	require.Equal(t, "synthetic input\n", result.Input)
	require.Equal(t, "synthetic stderr\n", stderr.String())
}

func TestPortableWindowsDispatcherLetsChildHandleControlC(t *testing.T) {
	_, base := windowsEntryFixture(t)
	self, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	controller := ownedEntryCommand(ctx, self, "-test.run=^"+entryHelper+"$", "--", "controller", base)
	controller.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE}
	out, err := controller.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "control-c preserved")
}

func TestPortableWindowsDispatcherRefusesDisabledRegistration(t *testing.T) {
	entry, base := windowsEntryFixture(t)
	root := filepath.Join(base, ReleaseIdentifier)
	record, err := ReadInstallation(root)
	require.NoError(t, err)
	record.Enabled = false
	writePortableFixture(t, root, record)
	var stderr bytes.Buffer
	handled, code := dispatchEntry(entry, []string{"--version"}, strings.NewReader(""), &bytes.Buffer{}, &stderr)
	require.True(t, handled)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "integration is unavailable")
}

func TestPortableWindowsEntryRemovalPreservesRunningCommand(t *testing.T) {
	entry, base := windowsEntryFixture(t)
	root := filepath.Join(base, ReleaseIdentifier)
	bundle, err := Read(root)
	require.NoError(t, err)
	record, err := ReadInstallation(root)
	require.NoError(t, err)
	original := entrySearchPath
	var removed []string
	entrySearchPath = func(directory string, install bool) error {
		require.False(t, install, "removal must never install a PATH entry")
		removed = append(removed, directory)
		return nil
	}
	t.Cleanup(func() { entrySearchPath = original })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := ownedEntryCommand(ctx, entry, "-test.run=^"+entryHelper+"$", "--", "hold", base)
	stdin, err := command.StdinPipe()
	require.NoError(t, err)
	stdout, err := command.StdoutPipe()
	require.NoError(t, err)
	command.Stderr = os.Stderr
	require.NoError(t, command.Start())
	t.Cleanup(func() {
		_ = stdin.Close()
		if command.ProcessState == nil {
			_ = command.Cancel()
			_ = command.Wait()
		}
	})
	output := bufio.NewScanner(stdout)
	require.True(t, output.Scan(), "command did not become ready: %v", output.Err())
	var childPID int
	count, err := fmt.Sscanf(output.Text(), "ready %d", &childPID)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Positive(t, childPID)
	require.NotEqual(t, command.Process.Pid, childPID, "the stable dispatcher must be waiting for a separate image process")

	require.NoError(t, RemoveEntryExpected(ctx, bundle, entry, record.Generation), "removing integration must not require stopping an active user command")
	require.NoFileExists(t, entry)
	require.Equal(t, []string{filepath.Dir(entry)}, removed)
	_, err = fmt.Fprintln(stdin, "ping")
	require.NoError(t, err)
	require.True(t, output.Scan(), "active child stopped during integration removal: %v", output.Err())
	require.Equal(t, fmt.Sprintf("alive %d", childPID), output.Text())
	require.NoError(t, stdin.Close())
	err = command.Wait()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 29, exit.ExitCode(), "removal must preserve the active command's exit status")
}

func ownedEntryCommand(ctx context.Context, executable string, arguments ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Cancel = func() error {
		directory, err := windows.GetSystemDirectory()
		if err != nil {
			return err
		}
		return exec.Command(filepath.Join(directory, "taskkill.exe"), "/PID", fmt.Sprint(command.Process.Pid), "/T", "/F").Run()
	}
	command.WaitDelay = 2 * time.Second
	return command
}

func TestPortableWindowsEntryHelper(t *testing.T) {
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || len(os.Args) < separator+3 {
		return
	}
	mode, base := os.Args[separator+1], os.Args[separator+2]
	portableBaseDirectory = func() (string, error) { return base, nil }
	if mode == "controller" {
		runEntryController()
		return
	}
	if handled, code := DispatchEntry(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled {
		os.Exit(code)
	}
	switch mode {
	case "echo":
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"arguments": os.Args[separator+3:], "input": string(input)})
		fmt.Fprintln(os.Stderr, "synthetic stderr")
		os.Exit(37)
	case "interrupt":
		interrupts := make(chan os.Signal, 1)
		signal.Notify(interrupts, os.Interrupt)
		fmt.Println("ready")
		<-interrupts
		fmt.Println("interrupted")
		os.Exit(23)
	case "hold":
		fmt.Printf("ready %d\n", os.Getpid())
		input := bufio.NewScanner(os.Stdin)
		for input.Scan() {
			if input.Text() != "ping" {
				os.Exit(1)
			}
			fmt.Printf("alive %d\n", os.Getpid())
		}
		if err := input.Err(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(29)
	default:
		os.Exit(1)
	}
}

func runEntryController() {
	// This process has its own console, so the broadcast cannot reach the test runner.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	entry, err := EntryPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	base, _ := portableBaseDirectory()
	command := exec.Command(entry, "-test.run=^"+entryHelper+"$", "--", "interrupt", base)
	stdout, err := command.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	output := bufio.NewScanner(stdout)
	if !output.Scan() || output.Text() != "ready" {
		fmt.Fprintln(os.Stderr, "child did not become ready", output.Text(), output.Err())
		os.Exit(1)
	}
	if err := windows.GenerateConsoleCtrlEvent(0, 0); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !output.Scan() || output.Text() != "interrupted" {
		fmt.Fprintln(os.Stderr, "child did not handle Ctrl-C", output.Text(), output.Err())
		os.Exit(1)
	}
	if err := command.Wait(); childExitCode(err) != 23 {
		fmt.Fprintln(os.Stderr, "dispatcher did not preserve interrupt exit:", err)
		os.Exit(1)
	}
	fmt.Println("control-c preserved")
	os.Exit(0)
}
