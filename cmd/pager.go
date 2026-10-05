package cmd

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	noPager bool

	pagerMu     sync.Mutex
	pagerActive bool
	pagerIn     io.WriteCloser
	pagerCmd    *exec.Cmd
)

func maybeStartPager(cmd *cobra.Command) error {
	if !shouldUsePager(cmd) {
		return nil
	}

	pagerMu.Lock()
	defer pagerMu.Unlock()
	if pagerActive {
		return nil
	}

	p, in, err := startPager()
	if err != nil {
		return nil // best-effort; fall back to normal stdout
	}
	pagerCmd = p
	pagerIn = in
	pagerActive = true

	cmd.SetOut(pagerIn)
	return nil
}

func closePager() error {
	pagerMu.Lock()
	active := pagerActive
	in := pagerIn
	p := pagerCmd
	pagerActive = false
	pagerIn = nil
	pagerCmd = nil
	pagerMu.Unlock()

	if !active {
		return nil
	}

	_ = in.Close()
	if p != nil {
		return p.Wait()
	}
	return nil
}

func shouldUsePager(cmd *cobra.Command) bool {
	if noPager {
		return false
	}
	if _, ok := os.LookupEnv("NO_PAGER"); ok {
		return false
	}
	if termEnv := strings.TrimSpace(os.Getenv("TERM")); termEnv == "" || termEnv == "dumb" {
		return false
	}
	if runtime.GOOS == "windows" {
		return false
	}

	// Don't ever page daemon or machine-facing commands.
	if strings.HasPrefix(cmd.CommandPath(), "rzm serve") || strings.HasPrefix(cmd.CommandPath(), "rzm start") || strings.HasPrefix(cmd.CommandPath(), "rzm agent") {
		return false
	}

	// Only page when writing to an interactive terminal (i.e., not piped).
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}

	// Avoid paging "do work / mutate things" commands where real-time output matters.
	// Paging is intentionally opt-in for a short list of "read-only, potentially long" commands.
	return isPagerEligible(cmd.CommandPath())
}

func isPagerEligible(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || path == "rzm" {
		return false
	}

	// Graph analysis output is commonly long.
	if strings.HasPrefix(path, "rzm graph ") {
		// Writes config; keep it direct.
		if path == "rzm graph ignore" {
			return false
		}
		return true
	}

	switch path {
	case "rzm changelog",
		"rzm code search",
		"rzm code complexity",
		"rzm search",
		"rzm code overview",
		"rzm semantic search",
		"rzm semantic find-connections":
		return true
	default:
		return false
	}
}

func startPager() (*exec.Cmd, io.WriteCloser, error) {
	pager := strings.TrimSpace(os.Getenv("RZM_PAGER"))
	if pager == "" {
		pager = strings.TrimSpace(os.Getenv("PAGER"))
	}
	if pager == "" {
		pager = "less"
	}

	parts := strings.Fields(pager)
	if len(parts) == 0 {
		return nil, nil, errors.New("empty pager")
	}
	name := parts[0]
	args := append([]string{}, parts[1:]...)

	path, err := exec.LookPath(name)
	if err != nil {
		// Best-effort fallback.
		if name != "less" {
			if p, err2 := exec.LookPath("less"); err2 == nil {
				path = p
				name = "less"
				args = nil
			}
		}
		if path == "" {
			return nil, nil, err
		}
	}

	// Preserve ANSI colors through the pager.
	if base := filepathBase(name); base == "less" {
		hasR := false
		hasF := false
		hasX := false
		for _, a := range args {
			switch strings.TrimSpace(a) {
			case "-R", "--RAW-CONTROL-CHARS":
				hasR = true
			case "-F", "--quit-if-one-screen":
				hasF = true
			case "-X", "--no-init":
				hasX = true
			}
		}
		if !hasR {
			args = append(args, "-R")
		}
		if !hasF {
			args = append(args, "-F")
		}
		if !hasX {
			args = append(args, "-X")
		}
	}

	c := exec.Command(path, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	in, err := c.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := c.Start(); err != nil {
		_ = in.Close()
		return nil, nil, err
	}
	return c, in, nil
}

func filepathBase(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}
