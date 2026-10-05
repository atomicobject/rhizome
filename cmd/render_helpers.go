package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

func getTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return 100
	}
	return width
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func reverseBreadcrumb(bc string) string {
	if bc == "" {
		return ""
	}
	parts := strings.Split(bc, " > ")
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, " < ")
}

func printIndentedText(w io.Writer, text string, indent string, maxWidth int) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	text = strings.ReplaceAll(text, "\r\n", "\n")
	paragraphs := strings.Split(text, "\n\n")

	contentWidth := maxWidth - len(indent)
	if contentWidth < 20 {
		contentWidth = 20
	}

	for i, para := range paragraphs {
		if i > 0 {
			fmt.Fprintln(w)
		}
		para = strings.ReplaceAll(para, "\n", " ")
		para = strings.Join(strings.Fields(para), " ")

		words := strings.Fields(para)
		if len(words) == 0 {
			continue
		}

		line := indent
		lineLen := 0
		for _, word := range words {
			wordLen := len(word)
			if lineLen > 0 && lineLen+1+wordLen > contentWidth {
				fmt.Fprintln(w, line)
				line = indent + word
				lineLen = wordLen
			} else {
				if lineLen > 0 {
					line += " "
					lineLen++
				}
				line += word
				lineLen += wordLen
			}
		}
		if lineLen > 0 {
			fmt.Fprintln(w, line)
		}
	}
}
