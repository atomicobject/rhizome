package cmd

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// ANSI color codes
const (
	reset      = "\033[0m"
	darkGreen  = "\033[38;5;22m"  // dark forest green
	green      = "\033[38;5;28m"  // medium green
	lightGreen = "\033[38;5;34m"  // lighter green
	teal       = "\033[38;5;30m"  // teal/cyan-green
	orange     = "\033[38;5;208m" // orange for nodes
	dimWhite   = "\033[38;5;250m" // dim white for symbols
)

// colorEnabled returns true if colors should be used
func colorEnabled() bool {
	// Respect NO_COLOR convention (https://no-color.org/)
	if _, exists := os.LookupEnv("NO_COLOR"); exists {
		return false
	}
	// Check if stdout is a terminal
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}
	return true
}

// NOTE: getTerminalWidth is defined in cmd/semantic.go

// logoLines returns the ASCII logo lines (colorized if enabled)
func logoLines(useColor bool) []string {
	// Base logo - all lines are 14 chars wide
	if !useColor {
		return []string{
			"    ||||||    ",   // 14
			" #  /||||\\ [] ",  // 14
			"   \\/||||\\/   ", // 14
			"[*]o==oo==o[*]",   // 14
			"   /\\||||/\\   ", // 14
			"  /  \\||/  \\  ", // 14
			" o----oo----o ",   // 14
			"  \\  /  \\  /  ", // 14
		}
	}

	// Colored version
	return []string{
		"    " + darkGreen + "|" + green + "|" + lightGreen + "||" + green + "|" + darkGreen + "|" + reset + "    ",
		" " + dimWhite + "#" + reset + "  " + darkGreen + "/" + green + "|" + lightGreen + "||" + green + "|" + teal + "\\" + reset + " " + dimWhite + "[]" + reset + " ",
		"   " + darkGreen + "\\/" + green + "|" + lightGreen + "||" + green + "|" + teal + "\\/" + reset + "   ",
		dimWhite + "[*]" + orange + "o" + darkGreen + "==" + orange + "oo" + teal + "==" + orange + "o" + dimWhite + "[*]" + reset,
		"   " + darkGreen + "/\\" + green + "|" + lightGreen + "||" + green + "|" + teal + "/\\" + reset + "   ",
		"  " + darkGreen + "/" + reset + "  " + green + "\\" + lightGreen + "||" + teal + "/" + reset + "  " + teal + "\\" + reset + "  ",
		" " + orange + "o" + darkGreen + "----" + orange + "oo" + teal + "----" + orange + "o" + reset + " ",
		"  " + darkGreen + "\\" + reset + "  " + green + "/" + reset + "  " + teal + "\\" + reset + "  " + teal + "/" + reset + "  ",
	}
}

// synopsis is the brief description shown next to the logo
var synopsis = []string{
	"A CLI for Obsidian vaults",
	"and codebases.",
	"",
	"Search, navigate, and manage",
	"notes and code from the terminal.",
	"Discover connections with graph",
	"analysis and semantic search.",
}

// wrapText wraps text to fit within the given width
func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string
	var current strings.Builder

	for _, word := range words {
		if current.Len() == 0 {
			current.WriteString(word)
		} else if current.Len()+1+len(word) <= width {
			current.WriteString(" ")
			current.WriteString(word)
		} else {
			lines = append(lines, current.String())
			current.Reset()
			current.WriteString(word)
		}
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

// renderBanner returns the banner with logo and synopsis side by side
func renderBanner() string {
	useColor := colorEnabled()
	termWidth := getTerminalWidth()

	logo := logoLines(useColor)
	logoWidth := 14 // visual width of longest logo line (without ANSI codes)
	gap := 4        // space between logo and synopsis

	// Calculate available width for synopsis
	synopsisWidth := termWidth - logoWidth - gap - 2
	if synopsisWidth < 20 {
		synopsisWidth = 20
	}

	// Wrap synopsis lines
	var wrappedSynopsis []string
	for _, line := range synopsis {
		if line == "" {
			wrappedSynopsis = append(wrappedSynopsis, "")
		} else {
			wrappedSynopsis = append(wrappedSynopsis, wrapText(line, synopsisWidth)...)
		}
	}

	// Combine logo and synopsis
	maxLines := len(logo)
	if len(wrappedSynopsis) > maxLines {
		maxLines = len(wrappedSynopsis)
	}

	var result strings.Builder
	result.WriteString("\n")

	for i := 0; i < maxLines; i++ {
		// Logo line (or padding)
		var logoLine string
		if i < len(logo) {
			logoLine = logo[i]
		}
		// Pad logo to fixed visual width
		logoPadded := logoLine + strings.Repeat(" ", logoWidth-visualLen(logoLine))

		// Synopsis line (or empty)
		var synLine string
		if i < len(wrappedSynopsis) {
			synLine = wrappedSynopsis[i]
		}

		result.WriteString("  ")
		result.WriteString(logoPadded)
		result.WriteString(strings.Repeat(" ", gap))
		result.WriteString(synLine)
		result.WriteString("\n")
	}

	// Add "rhizome" text under logo (centered under 14-char logo starting at col 2)
	if useColor {
		result.WriteString(fmt.Sprintf("\n   %sr h i z o m e%s\n\n", lightGreen, reset))
	} else {
		result.WriteString("\n   r h i z o m e\n\n")
	}

	return result.String()
}

// visualLen returns the visual length of a string, excluding ANSI codes
func visualLen(s string) int {
	// Remove ANSI escape sequences
	inEscape := false
	length := 0
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		length++
	}
	return length
}
