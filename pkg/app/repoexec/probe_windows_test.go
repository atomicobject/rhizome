//go:build windows

package repoexec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

type consoleProbeState struct {
	Console uintptr `json:"console"`
	Guards  bool    `json:"guards"`
	Cwd     bool    `json:"cwd"`
}

type nestedConsoleProbe struct {
	Outer uintptr           `json:"outer"`
	Child consoleProbeState `json:"child"`
}

// A GUI's consoleless bridge must not cause Probe's console-subsystem child
// to allocate a console. The outer fixture deliberately bypasses Configure.
func TestProbeFromConsolelessParentDoesNotAllocateConsole(t *testing.T) {
	executable, err := os.Executable()
	require.NoError(t, err)
	cwd := filepath.Join(t.TempDir(), "probe folder ü")
	require.NoError(t, os.Mkdir(cwd, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "cwd-marker"), nil, 0o600))
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	outer := exec.CommandContext(ctx, executable, "-test.run=^TestProbeConsoleFixture$", "--", "rhizome-console-fixture", "outer")
	outer.Dir = cwd
	outer.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	output, err := outer.CombinedOutput()
	require.NoError(t, err, string(output))
	var result nestedConsoleProbe
	require.NoError(t, json.Unmarshal(output, &result), string(output))
	require.Zero(t, result.Outer, "fixture parent must really be consoleless")
	require.Zero(t, result.Child.Console, "Probe must suppress the nested console")
	require.True(t, result.Child.Guards, "Probe must preserve delegation guards")
	require.True(t, result.Child.Cwd, "Probe must preserve the selected working directory")
}

func TestProbeConsoleFixture(t *testing.T) {
	var mode string
	for i, arg := range os.Args {
		if arg == "rhizome-console-fixture" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
			break
		}
	}
	if mode == "" {
		return
	}
	getConsoleWindow := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")
	console, _, _ := getConsoleWindow.Call()
	switch mode {
	case "outer":
		executable, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		output, err := Probe(context.Background(), executable, cwd, "-test.run=^TestProbeConsoleFixture$", "--", "rhizome-console-fixture", "child")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		var child consoleProbeState
		if err := json.Unmarshal([]byte(output), &child); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := json.NewEncoder(os.Stdout).Encode(nestedConsoleProbe{Outer: console, Child: child}); err != nil {
			os.Exit(2)
		}
	case "child":
		_, err := os.Stat("cwd-marker")
		state := consoleProbeState{
			Console: console,
			Guards:  os.Getenv("RZM_REPO_DELEGATED") == "1" && os.Getenv("RZM_SKIP_REPO_DELEGATE") == "1",
			Cwd:     err == nil,
		}
		if err := json.NewEncoder(os.Stdout).Encode(state); err != nil {
			os.Exit(2)
		}
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
