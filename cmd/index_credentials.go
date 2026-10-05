package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/credentials"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"golang.org/x/term"
)

// ErrEmbeddingCredentialsMissing is returned when the index command needs an
// API key for an enabled embeddings provider but the key cannot be resolved
// from env, the user CLI config, or a team key — and we cannot prompt.
var ErrEmbeddingCredentialsMissing = errors.New("embeddings provider keys missing")

// ensureEmbeddingCredentials resolves the API keys the index command needs
// through a shared credentials.Session. Keys the user skipped during init are
// never re-prompted: they get a one-line hint and the missing-credentials
// error. Truly missing keys prompt only when stdin is a TTY.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US5-AC4]]
func ensureEmbeddingCredentials(vaultPath string, in io.Reader, out io.Writer) error {
	cfg, err := obsidian.LoadLocalConfig(vaultPath)
	if err != nil {
		if errors.Is(err, obsidian.ErrNoLocalConfig) {
			return nil
		}
		return err
	}
	needs := credentials.CollectEmbeddingsNeeds(cfg)
	if len(needs) == 0 {
		return nil
	}

	var opts []credentials.Option
	if isInteractive(in) {
		opts = append(opts, credentials.WithPrompts(bufio.NewReader(in), out))
	}
	return ensureEmbeddingCredentialsWithSession(credentials.NewSession(opts...), needs, out)
}

// ensureEmbeddingCredentialsWithSession applies the per-need policy: satisfied
// needs pass, skipped needs hint without prompting, and unresolved needs
// prompt once when the session is interactive. Any need still missing after
// the pass fails with ErrEmbeddingCredentialsMissing so the caller does not
// proceed into a half-configured index run.
func ensureEmbeddingCredentialsWithSession(session *credentials.Session, needs []credentials.Need, out io.Writer) error {
	var missing []string
	missingTeamCovered := false
	for _, need := range needs {
		if session.Satisfied(need) {
			continue
		}
		if session.Skipped(need.Key) {
			fmt.Fprintf(out, "%s was skipped during init but is needed for %s; set the env var or rerun 'rzm init' to provide it.\n", need.Key, need.Purpose)
			missing = append(missing, need.Key)
			missingTeamCovered = missingTeamCovered || need.AllowTeamKey
			continue
		}
		if session.CanPrompt() {
			if err := session.PromptForNeed(need); err != nil {
				return err
			}
			if session.Satisfied(need) {
				continue
			}
		}
		missing = append(missing, need.Key)
		missingTeamCovered = missingTeamCovered || need.AllowTeamKey
	}
	if len(missing) == 0 {
		return nil
	}
	labels := missing
	if missingTeamCovered {
		labels = append([]string{credentials.AtomicRhizomeKey}, missing...)
	}
	return fmt.Errorf("%w: provide one of [%s] via env, ~/.config/rhizome/config.yml, or rerun 'rzm init'",
		ErrEmbeddingCredentialsMissing, strings.Join(labels, ", "))
}

func isInteractive(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
