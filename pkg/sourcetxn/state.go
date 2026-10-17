package sourcetxn

import (
	"errors"
	"os"
)

type fileState struct {
	Path            string
	Absolute        string
	OriginalPath    string
	Internal        bool
	Omit            bool
	OriginalExists  bool
	OriginalContent []byte `json:"-"`
	OriginalMode    os.FileMode
	FinalExists     bool
	FinalContent    []byte `json:"-"`
	FinalMode       os.FileMode
}

type preparedTransaction struct {
	DecisionSynced bool
	Directory      string
	Manifest       Manifest
	Files          []fileState
	Installed      int
}

type Hooks struct {
	AfterJournalPublished                func(path string) error
	BeforeInstall                        func(index int, path string) error
	AfterMutation                        func(index int, path string) error
	AfterInstall                         func(index int, path string) error
	BeforeInstallDirectorySync           func(index int, path string) error
	AfterCleanupBoundary                 func(boundary, path string) error
	BeforeRollbackMutation               func(index int, path string) error
	AfterNamespaceIndexPublication       func() error
	BeforeNamespaceDecisionDirectorySync func(marker string) error
	BeforeNamespaceGitRelease            func(decision string) error
	BeforeSourceSettlement               func(directory string) error
	AfterSourceSettlement                func(directory string) error
	afterCommitMarkerPublished           func() error
}

var ErrSimulatedInterruption = errors.New("simulated repair process interruption")
