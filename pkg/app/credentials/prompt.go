package credentials

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// EnsureNeeds runs one consolidated credential pass over needs: it first
// uses credentials already present in the environment without persisting them, then
// prompts for each need that is neither satisfied nor skipped. Later calls in
// the same run consult the shared session state, so a key or skip collected
// once never re-prompts. No-op on non-interactive sessions.
func (s *Session) EnsureNeeds(needs []Need) error {
	if !s.CanPrompt() || len(needs) == 0 {
		return nil
	}

	for _, need := range needs {
		if s.Skipped(need.Key) || s.Satisfied(need) {
			continue
		}
		if err := s.PromptForNeed(need); err != nil {
			return err
		}
	}
	return nil
}

// OffersTeamKey reports whether prompts for need should mention the Atomic
// Object Rhizome key: only builds that bundle team keys can use it.
func (s *Session) OffersTeamKey(need Need) bool {
	return need.AllowTeamKey && s.teamBundled()
}

// PromptForNeed asks once for a key that satisfies need. In a build that
// bundles team keys it leads with the Atomic Object Rhizome key and
// recognizes which key was pasted. An empty answer records a skip so later
// commands print a hint instead of asking again. Provided keys are persisted
// immediately. No-op on non-interactive sessions.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US5-AC5]]
func (s *Session) PromptForNeed(need Need) error {
	if !s.CanPrompt() || s.Skipped(need.Key) || s.Satisfied(need) {
		return nil
	}
	fmt.Fprintln(s.out)
	fmt.Fprintln(s.out, styleHeading(s.out, "Credentials"))
	prompt := fmt.Sprintf("Paste your %s for %s (Enter to skip): ", need.Label, need.Purpose)
	if s.OffersTeamKey(need) {
		fmt.Fprintln(s.out, "This Rhizome build includes Atomic Object team keys.")
		prompt = fmt.Sprintf("Paste your Atomic Object Rhizome key or your %s for %s (Enter to skip): ", need.Label, need.Purpose)
	}
	value := promptLine(s.reader, s.out, prompt)
	return s.ProvideKeyFor(need, value)
}

// ProvideKeyFor saves a pasted value for need: as the Atomic Object Rhizome
// key when it unlocks this build's bundle, otherwise as the provider key. An
// empty value records a skip.
func (s *Session) ProvideKeyFor(need Need, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return s.Skip(need.Key)
	}
	if s.OffersTeamKey(need) && s.teamUnlocks(value) {
		return s.Provide(AtomicRhizomeKey, value)
	}
	return s.Provide(need.Key, value)
}

func promptLine(reader *bufio.Reader, out io.Writer, prompt string) string {
	fmt.Fprint(out, prompt)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

func styleHeading(out io.Writer, s string) string {
	if !colorsEnabled(out) {
		return s
	}
	return "\033[1m\033[36m" + s + "\033[0m"
}

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
