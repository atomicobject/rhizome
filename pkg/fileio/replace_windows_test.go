package fileio

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const imageProbeArgument = "fileio-running-image-probe"
const imageProbeMarker = "\nfileio-image="

func TestReplaceRunningExecutable(t *testing.T) {
	if len(os.Args) > 1 && os.Args[len(os.Args)-1] == imageProbeArgument {
		runImageProbe(t)
		return
	}

	for _, tc := range []struct {
		name    string
		replace func(string, string) error
	}{
		{name: "ordinary_rename_diagnostic", replace: os.Rename},
		{name: "cooperative_replace", replace: Replace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target, staged := filepath.Join(dir, "running.exe"), filepath.Join(dir, "staged.exe")
			copyProbeImage(t, target, "old")
			copyProbeImage(t, staged, "new")
			oldBytes, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			stagedInfo, err := os.Stat(staged)
			if err != nil {
				t.Fatal(err)
			}
			// Windows resolves Stat file identity lazily; capture it before renaming.
			if !os.SameFile(stagedInfo, stagedInfo) {
				t.Fatal("could not capture the staged executable's file identity")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, target, "-test.run=^TestReplaceRunningExecutable$", "--", imageProbeArgument)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = stdin.Close()
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			})
			output := bufio.NewScanner(stdout)
			oldReply := fmt.Sprintf("old %d", cmd.Process.Pid)
			probeReply(t, output, oldReply)

			replaceErr := tc.replace(staged, target)
			if replaceErr != nil && !errors.Is(replaceErr, os.ErrPermission) && !errors.Is(replaceErr, windows.ERROR_SHARING_VIOLATION) {
				t.Fatalf("unexpected replacement failure: %v", replaceErr)
			}
			t.Logf("replacement while process %d is running: %v", cmd.Process.Pid, replaceErr)
			if _, err := fmt.Fprintln(stdin, "ping"); err != nil {
				t.Fatal(err)
			}
			probeReply(t, output, oldReply)

			current, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			wantMarker := "old"
			if replaceErr == nil {
				wantMarker = "new"
				if !os.SameFile(stagedInfo, current) {
					t.Fatal("target does not name the staged executable after successful replacement")
				}
			} else {
				if _, err := os.Stat(staged); err != nil {
					t.Fatalf("failed rename did not preserve its staged source: %v", err)
				}
				currentBytes, err := os.ReadFile(target)
				if err != nil || !bytes.Equal(oldBytes, currentBytes) {
					t.Fatalf("failed replacement changed the old executable: %v", err)
				}
			}

			probeInvocation(t, ctx, target, wantMarker)
			if err := stdin.Close(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatalf("old executable failed after replacement: %v", err)
			}
			if replaceErr != nil {
				if err := tc.replace(staged, target); err != nil {
					t.Fatalf("replacement after the old process exits: %v", err)
				}
				probeInvocation(t, ctx, target, "new")
			}
		})
	}
}

func probeInvocation(t *testing.T, ctx context.Context, target, marker string) {
	t.Helper()
	next := exec.CommandContext(ctx, target, "-test.run=^TestReplaceRunningExecutable$", "--", imageProbeArgument)
	output, err := next.CombinedOutput()
	if err != nil {
		t.Fatalf("launch executable after replacement: %v\n%s", err, output)
	}
	want := fmt.Sprintf("%s %d\n", marker, next.Process.Pid)
	if !strings.HasPrefix(string(output), want) {
		t.Fatalf("new invocation = %q, want prefix %q", output, want)
	}
}

func copyProbeImage(t *testing.T, path, marker string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	src, err := OpenRead(self)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(dst, "%s%s\n", imageProbeMarker, marker); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
}

func runImageProbe(t *testing.T) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := OpenRead(self)
	if err != nil {
		t.Fatal(err)
	}
	const markerSize = len(imageProbeMarker) + len("old\n")
	if _, err := file.Seek(-int64(markerSize), io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	marker := make([]byte, markerSize)
	if _, err := io.ReadFull(file, marker); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	value := strings.TrimSuffix(strings.TrimPrefix(string(marker), imageProbeMarker), "\n")
	if value != "old" && value != "new" {
		t.Fatalf("invalid executable marker %q", marker)
	}
	if _, err := fmt.Printf("%s %d\n", value, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	input := bufio.NewScanner(os.Stdin)
	for input.Scan() {
		if input.Text() != "ping" {
			t.Fatalf("unexpected probe input %q", input.Text())
		}
		if _, err := fmt.Printf("%s %d\n", value, os.Getpid()); err != nil {
			t.Fatal(err)
		}
	}
	if err := input.Err(); err != nil {
		t.Fatal(err)
	}
}

func probeReply(t *testing.T, output *bufio.Scanner, want string) {
	t.Helper()
	if !output.Scan() {
		t.Fatalf("waiting for probe reply %q: %v", want, output.Err())
	}
	if output.Text() != want {
		t.Fatalf("probe reply = %q, want %q", output.Text(), want)
	}
}
