package init

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func promptYesNo(reader *bufio.Reader, out io.Writer, prompt string, defaultYes bool) bool {
	def := "Y/n"
	if !defaultYes {
		def = "y/N"
	}
	fmt.Fprintf(out, "%s (%s): ", strings.TrimSpace(prompt), def)
	text, _ := reader.ReadString('\n')
	text = strings.TrimSpace(strings.ToLower(text))
	if text == "" {
		return defaultYes
	}
	return text == "y" || text == "yes"
}

func promptLine(reader *bufio.Reader, out io.Writer, prompt string) string {
	fmt.Fprint(out, prompt)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

// askChoice asks until the answer is one of valid, so a typo never picks an
// option; Enter picks def, which must be valid.
func askChoice(reader *bufio.Reader, out io.Writer, prompt, def string, valid ...string) string {
	var named []string
	for _, v := range valid {
		if v != "" {
			named = append(named, v)
		}
	}
	options := strings.Join(named, " or ")
	if len(named) > 2 {
		options = strings.Join(named[:len(named)-1], ", ") + ", or " + named[len(named)-1]
	}
	for {
		choice := promptChoice(reader, out, prompt, def)
		if contains(valid, choice) {
			return choice
		}
		fmt.Fprintf(out, "Please answer %s.\n", options)
	}
}

func promptChoice(reader *bufio.Reader, out io.Writer, prompt string, defaultChoice string) string {
	fmt.Fprint(out, prompt)
	text, _ := reader.ReadString('\n')
	text = strings.TrimSpace(strings.ToLower(text))
	if text == "" {
		return defaultChoice
	}
	// Only accept first character for simple single-letter options.
	return string(text[0])
}
