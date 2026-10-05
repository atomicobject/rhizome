package init

import (
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiBlue   = "\033[34m"
	ansiCyan   = "\033[36m"
)

func colorsEnabled(out io.Writer) bool {
	if _, exists := os.LookupEnv("NO_COLOR"); exists {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func styleHeading(out io.Writer, s string) string {
	if !colorsEnabled(out) {
		return s
	}
	return ansiBold + ansiCyan + s + ansiReset
}

func styleLabel(out io.Writer, s string) string {
	if !colorsEnabled(out) {
		return s
	}
	return ansiBold + s + ansiReset
}

func styleDim(out io.Writer, s string) string {
	if !colorsEnabled(out) {
		return s
	}
	return ansiDim + s + ansiReset
}

func styleOK(out io.Writer, s string) string {
	if !colorsEnabled(out) {
		return s
	}
	return ansiGreen + s + ansiReset
}

func styleWarn(out io.Writer, s string) string {
	if !colorsEnabled(out) {
		return s
	}
	return ansiYellow + s + ansiReset
}

// DiffColors provides color styling for diff output.
type DiffColors struct {
	enabled bool
}

// NewDiffColors creates a DiffColors instance based on the output writer.
func NewDiffColors(out io.Writer) *DiffColors {
	return &DiffColors{enabled: colorsEnabled(out)}
}

// Enabled returns whether colors are enabled.
func (c *DiffColors) Enabled() bool {
	return c.enabled
}

// FileHeader styles the file header (filename and hunk count).
func (c *DiffColors) FileHeader(s string) string {
	if !c.enabled {
		return s
	}
	return ansiBold + ansiCyan + s + ansiReset
}

// HunkHeader styles the @@ hunk header line.
func (c *DiffColors) HunkHeader(s string) string {
	if !c.enabled {
		return s
	}
	return ansiBlue + s + ansiReset
}

// Removed styles removed lines (red).
func (c *DiffColors) Removed(s string) string {
	if !c.enabled {
		return s
	}
	return ansiRed + s + ansiReset
}

// Added styles added lines (green).
func (c *DiffColors) Added(s string) string {
	if !c.enabled {
		return s
	}
	return ansiGreen + s + ansiReset
}

// Context styles context lines (dim).
func (c *DiffColors) Context(s string) string {
	if !c.enabled {
		return s
	}
	return ansiDim + s + ansiReset
}
