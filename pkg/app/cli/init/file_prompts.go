package init

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/cli/init/diff"
)

// terminalOwnershipUI asks in a terminal.
type terminalOwnershipUI struct {
	reader *bufio.Reader
	out    io.Writer
	colors *DiffColors
}

func newTerminalOwnershipUI(reader *bufio.Reader, out io.Writer) *terminalOwnershipUI {
	return &terminalOwnershipUI{reader: reader, out: out, colors: NewDiffColors(out)}
}

func (u *terminalOwnershipUI) decideUnknown(changes []*change) string {
	paths := uniqueGroupPaths(changes)
	removal := len(changes) > 0 && changes[0].removal
	files := countNoun(len(paths), "file")
	if copies := len(changes) - len(paths); copies > 0 {
		files += " (and " + countNoun(copies, "copy") + ")"
	}
	fmt.Fprintln(u.out)
	if removal {
		fmt.Fprintf(u.out, "Rhizome no longer ships %s, but can't tell whether %s edited:\n", files, pluralVerb(len(paths), "it was", "they were"))
	} else {
		fmt.Fprintf(u.out, "Rhizome can't tell whether %s %s edited since Rhizome wrote %s:\n", files, pluralVerb(len(paths), "was", "were"), pluralVerb(len(paths), "it", "them"))
	}
	const shown = 8
	for i, path := range paths {
		if i == shown {
			fmt.Fprintf(u.out, "  and %d more\n", len(paths)-shown)
			break
		}
		fmt.Fprintf(u.out, "  %s\n", path)
	}
	action, prompt, fallback := "update them all", "Choose [y]: ", "y"
	if removal {
		action, prompt, fallback = "remove them all", "Choose [n]: ", "n"
	}
	fmt.Fprintf(u.out, "  y  %s    n  keep them all    r  review each\n", action)
	switch askChoice(u.reader, u.out, prompt, fallback, "y", "n", "r") {
	case "n":
		return "keep"
	case "r":
		return "review"
	default:
		return "update"
	}
}

func (u *terminalOwnershipUI) decideEdited(c *change, mirrors int) bool {
	fmt.Fprintln(u.out)
	label := c.rel
	if _, block, ok := strings.Cut(c.key, "#"); ok {
		if block == "rhizome" {
			block = "Rhizome"
		}
		label = fmt.Sprintf("The %s block in %s", block, c.rel)
	}
	if mirrors > 0 {
		label += fmt.Sprintf(" (and %s)", countNoun(mirrors, "copy"))
	}
	take, keep := "take the update", "keep my version"
	if c.removal {
		fmt.Fprintf(u.out, "%s has local edits, and Rhizome no longer ships it.\n", label)
		take, keep = "remove it", "keep it"
	} else {
		fmt.Fprintf(u.out, "%s has local edits and a newer Rhizome version.\n", label)
	}
	for {
		fmt.Fprintf(u.out, "  y  %s    n  %s    d  show the diff\n", take, keep)
		switch askChoice(u.reader, u.out, "Choose [n]: ", "n", "y", "n", "d") {
		case "y":
			return true
		case "d":
			u.showDiff(c)
		default:
			return false
		}
	}
}

func (u *terminalOwnershipUI) showDiff(c *change) {
	_, hunks, err := diff.GenerateUnifiedDiff(string(c.current), string(c.desired), c.rel)
	if err != nil || len(hunks) == 0 {
		fmt.Fprintln(u.out, "  (no line differences)")
		return
	}
	fmt.Fprintln(u.out, styleDim(u.out, "  Lines marked - are only in your version; lines marked + come from the update."))
	for i := range hunks {
		fmt.Fprint(u.out, hunks[i].ColoredString(u.colors))
	}
}

func uniqueGroupPaths(changes []*change) []string {
	seen := map[string]bool{}
	var paths []string
	for _, c := range changes {
		if seen[c.group] {
			continue
		}
		seen[c.group] = true
		paths = append(paths, c.key)
	}
	return paths
}

func countNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	if strings.HasSuffix(noun, "y") {
		return fmt.Sprintf("%d %sies", n, strings.TrimSuffix(noun, "y"))
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func pluralVerb(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// starterValidationSelector names the validation selector to run after
// schema, saved query, or view files change, or "" when none changed.
func starterValidationSelector(updated []string) string {
	checks := map[string]bool{}
	for _, path := range updated {
		switch {
		case strings.HasPrefix(path, ".rhizome/ontology/"):
			checks["ontology"] = true
		case strings.HasPrefix(path, ".rhizome/query-recipes/"):
			checks["query-recipes"] = true
		case strings.HasPrefix(path, ".rhizome/views/"):
			checks["views"] = true
		}
	}
	var ordered []string
	for _, check := range []string{"ontology", "query-recipes", "views"} {
		if checks[check] {
			ordered = append(ordered, check)
		}
	}
	switch len(ordered) {
	case 0:
		return ""
	case 1:
		return ordered[0]
	default:
		return "all"
	}
}
