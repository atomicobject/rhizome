package init

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/credentials"
)

// promptWorkflow asks which workflow to install. current marks the existing
// choice on reruns.
func promptWorkflow(reader *bufio.Reader, out io.Writer, current []string) []string {
	fmt.Fprintln(out)
	fmt.Fprintln(out, styleHeading(out, "Workflow"))
	fmt.Fprintln(out, "  1  Agentic Engineering (recommended)")
	fmt.Fprintln(out, styleDim(out, "     Specs, efforts, and engineering policy docs your agents follow"))
	fmt.Fprintln(out, "  2  Agentic Engineering with domain modeling")
	fmt.Fprintln(out, styleDim(out, "     Adds sources, requirements, and traceability for regulated or complex domains"))
	fmt.Fprintln(out, "  3  Search and agent guidance only")
	fmt.Fprintln(out, styleDim(out, "     Search, code navigation, and Rhizome guidance, with no workflow docs"))
	def := "1"
	switch {
	case contains(current, templateComplexDomain):
		def = "2"
	case current != nil && !contains(current, templateAgenticEngineering):
		def = "3"
	}
	switch askChoice(reader, out, "Choose ["+def+"]: ", def, "1", "2", "3") {
	case "2":
		return []string{templateComplexDomain}
	case "3":
		return nil
	default:
		return []string{templateAgenticEngineering}
	}
}

// promptSearchKey asks for a key for provider. In a build that bundles team
// keys it leads with the Atomic Object Rhizome key. Enter defers; "other"
// offers OpenAI, Ollama, or off.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US1-AC2]]
func promptSearchKey(reader *bufio.Reader, out io.Writer, provider string, session *credentials.Session) (string, bool, string, error) {
	need, _ := searchNeed(provider)
	fmt.Fprintln(out)
	fmt.Fprintln(out, styleHeading(out, "Semantic search"))
	fmt.Fprintf(out, "  Rhizome uses %s to search notes and code by meaning.\n", providerDisplayName(provider))
	if session.OffersTeamKey(need) {
		fmt.Fprintln(out, "  This Rhizome build includes Atomic Object team keys. Paste your")
		fmt.Fprintf(out, "  Atomic Object Rhizome key to unlock them, or paste your own %s.\n", need.Label)
	} else {
		fmt.Fprintf(out, "  Paste your %s.\n", need.Label)
	}
	fmt.Fprintln(out, "  Press Enter to set it up later, or type \"other\" for OpenAI or Ollama.")
	value := promptLine(reader, out, "Key: ")
	switch strings.ToLower(value) {
	case "":
		return provider, false, "", nil
	case "other":
		return promptOtherSearch(reader, out, session)
	}
	ready, label, err := saveKey(session, need, value)
	return provider, ready, label, err
}

// askKey asks for provider's key. In a build that bundles team keys it leads
// with the Atomic Object Rhizome key. Enter defers.
func askKey(reader *bufio.Reader, out io.Writer, provider string, session *credentials.Session) (bool, string, error) {
	need, _ := searchNeed(provider)
	prompt := fmt.Sprintf("Paste your %s (Enter to set it up later): ", need.Label)
	if session.OffersTeamKey(need) {
		fmt.Fprintln(out, "  This Rhizome build includes Atomic Object team keys.")
		prompt = fmt.Sprintf("Paste your Atomic Object Rhizome key or your %s (Enter to set it up later): ", need.Label)
	}
	value := promptLine(reader, out, prompt)
	if value == "" {
		return false, "", nil
	}
	return saveKey(session, need, value)
}

// saveKey saves a pasted key and returns whether need is now met and the
// label of what was saved.
func saveKey(session *credentials.Session, need credentials.Need, value string) (bool, string, error) {
	if err := session.ProvideKeyFor(need, value); err != nil {
		return false, "", err
	}
	label := need.Label
	if session.Resolve(credentials.AtomicRhizomeKey) == strings.TrimSpace(value) {
		label = "Atomic Object Rhizome key"
	}
	return session.Satisfied(need), label, nil
}

func promptOtherSearch(reader *bufio.Reader, out io.Writer, session *credentials.Session) (string, bool, string, error) {
	openAIReady := searchReady(searchOpenAI, session, nil)
	ollamaReady := searchReady(searchOllama, session, nil)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  1  OpenAI (%s)\n", readyLabel(openAIReady, "key found", "needs a key"))
	fmt.Fprintf(out, "  2  Ollama, runs locally and free (%s)\n", readyLabel(ollamaReady, "installed", "not installed"))
	fmt.Fprintln(out, "  3  Turn off semantic search")
	switch askChoice(reader, out, "Choose: ", "", "", "1", "2", "3") {
	case "1":
		if openAIReady {
			return searchOpenAI, true, "", nil
		}
		ready, saved, err := askKey(reader, out, searchOpenAI, session)
		return searchOpenAI, ready, saved, err
	case "2":
		return searchOllama, ollamaReady, "", nil
	case "3":
		return searchOff, false, "", nil
	default:
		return searchVoyage, false, "", nil
	}
}
