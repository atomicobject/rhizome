package tasks

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/atomicobject/rhizome/pkg/fileio"
)

const (
	handoffFile = "handoff.md"
	maxHandoff  = 8 << 10
	handoffCut  = "\n\n[Rhizome cut this handoff note at 8 KB. Keep it shorter.]\n"
)

func handoffPath(loc Locations) string { return filepath.Join(loc.DataDir, handoffFile) }

// writeMemory tells the agent about a `memory: true` task's handoff note
// (SPEC-0127 US14).
func writeMemory(b *strings.Builder, def Definition, loc Locations) {
	if !def.Memory {
		return
	}
	fmt.Fprintf(b, "This task keeps a handoff note between runs at %s ($RZM_TASK_DATA_DIR/%s). Read it first; it may not exist yet. Rewrite it last, before your final message, with what the next run needs to know: what you did, what is left, and anything to watch. Keep it short, under 8 KB; Rhizome cuts anything longer.\n\n", handoffPath(loc), handoffFile)
}

// trimHandoff cuts a handoff note longer than the cap to its head plus a
// marker, at most maxHandoff bytes in all, ending on a whole line when one
// ends in the second half. Only a regular file in the real data folder is
// trimmed; a symlink, FIFO, or device is left alone. It reads at most one
// byte past the cap and replaces the note through a temporary file, so it
// never writes through a link.
func trimHandoff(path string) error {
	f, err := openRegular(path, os.O_RDONLY, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxHandoff+1))
	_ = f.Close()
	if err != nil || len(data) <= maxHandoff {
		return err
	}
	head := data[:maxHandoff-len(handoffCut)]
	if i := bytes.LastIndexByte(head, '\n'); i >= len(head)/2 {
		head = head[:i]
	}
	// Drop a rune the cut split.
	for len(head) > 0 {
		if r, size := utf8.DecodeLastRune(head); r != utf8.RuneError || size > 1 {
			break
		}
		head = head[:len(head)-1]
	}
	return fileio.WriteFileAtomic(path, append(bytes.TrimRight(head, "\n"), handoffCut...), 0o600)
}
