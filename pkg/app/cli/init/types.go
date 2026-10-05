package init

import (
	"io"
	"os"

	"github.com/mattn/go-isatty"
)

// IsTTY returns true if the given file is a terminal.
func IsTTY(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

// RunOptions controls the init flow.
type RunOptions struct {
	Dir string
	// Interactive means a person at a terminal answers questions. Every
	// other run uses the recommendations, keeps edited files, and lists them.
	Interactive bool
	// IndexNow builds the search index for projectRoot the way rzm index
	// does. A run in a terminal offers it at the end; nil leaves indexing to
	// the person.
	IndexNow func(projectRoot string) error
	// AcceptSuggestions applies suggestions in a run without a terminal,
	// after the person agreed to them.
	AcceptSuggestions bool
	// Check reports what init would change without writing or asking.
	Check         bool
	BinaryManager string // optional binary ownership mode; currently external
	// Workflow chooses the workflow without asking: agentic-engineering,
	// domain, none, or starter ids, comma-separated.
	Workflow string
	// Agents chooses agent integrations without asking: a comma-separated
	// subset of claude, codex, and cursor, or none.
	Agents string
	// Search chooses the semantic search provider: voyage, openai, ollama, or off.
	Search string

	RefreshDocs    bool // offer updates to existing starter docs; by default they are only created
	IncludeIgnored []string
	Eject          string // keep these workflows' files (comma-separated) and stop updating them
	Restore        string // resume updates for an ejected workflow

	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
}

const (
	agentModeAuto = "auto"
	agentModeOn   = "on"
	agentModeOff  = "off"
)

const (
	sectionRhizome        = "rhizome"
	sectionNotes          = "notes"
	sectionCode           = "code"
	sectionFileContext    = "fileContext"
	sectionNoteEmbeddings = "noteEmbeddings"
	sectionCodeEmbeddings = "codeEmbeddings"
	sectionGraph          = "graph"
	sectionAgents         = "agents"
	sectionIndexPath      = "indexPath"
	sectionCompression    = "compression"
	sectionWorkflowTmpls  = "workflowTemplates"
	sectionWorkflowAddons = "workflowTemplateAddons"
	sectionWorkflowMgmt   = "workflowTemplateManagement"
	sectionRefresh        = "refresh"
	sectionTemplateDocs   = "templateDocs"
)

type changeSet map[string]bool

func (c changeSet) mark(section string) {
	if c == nil || section == "" {
		return
	}
	c[section] = true
}

func (c changeSet) any() bool {
	return len(c) > 0
}
