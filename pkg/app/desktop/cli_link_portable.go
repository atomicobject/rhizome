package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/appbundle"
	"github.com/atomicobject/rhizome/pkg/app/repoexec"
)

var portableCLIEntryPath = appbundle.EntryPath

func portableCLILinkInspect(bundle *appbundle.Info) (CLILinkInfo, error) {
	info := CLILinkInfo{State: "unsupported"}
	path, err := portableCLIEntryPath()
	if err != nil {
		return info, err
	}
	info.LinkPath = path
	if bundle.Dev || !bundle.Rhizome() {
		return info, nil
	}
	info.State, info.Target, err = appbundle.EntryState(path, *bundle)
	return info, err
}

func (s *Service) portableCLILinkShell(ctx context.Context, bundle *appbundle.Info) CLILinkShell {
	result := CLILinkShell{}
	search, _ := s.env.Lookup("PATH")
	for _, directory := range filepath.SplitList(search) {
		if !filepath.IsAbs(directory) {
			continue
		}
		path := filepath.Join(directory, repoexec.ExecutableName())
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			result.Path = path
			break
		}
	}
	if result.Path == "" {
		result.Error = "Open a new terminal after installing the command-line tool. If it is still missing, add its directory to your user PATH."
		return result
	}
	entry, _ := portableCLIEntryPath()
	resolved, err := filepath.EvalSymlinks(result.Path)
	if err == nil {
		result.IsApp = resolved == bundle.ExecutablePath
	}
	if result.Path == entry {
		state, _, _ := appbundle.EntryState(entry, *bundle)
		result.IsApp = state == "installed"
	}
	out, err := repoexec.Probe(ctx, result.Path, s.stateDir, "--version")
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Version = repoexec.ParseVersionOutput(strings.TrimSpace(out))
	if result.Version == "" {
		result.Error = "The terminal's rzm did not report a Rhizome version."
	}
	return result
}

func (s *Service) mutatePortableCLILink(ctx context.Context, bundle *appbundle.Info, remove bool, generation uint64) (CLILinkInfo, error) {
	info, err := portableCLILinkInspect(bundle)
	if err != nil {
		return info, problem("link_failed", err.Error())
	}
	if info.State == "unsupported" {
		return info, problem("link_unsupported", "Install the command-line tool from a Rhizome release app.")
	}
	if info.State == "foreign" {
		return info, problem("link_foreign", "The command-line entry belongs to another installation and was left in place.")
	}
	if remove {
		err = appbundle.RemoveEntryExpected(ctx, *bundle, info.LinkPath, generation)
	} else {
		err = appbundle.InstallEntry(ctx, *bundle, info.LinkPath)
	}
	if err != nil {
		return info, problem("link_failed", err.Error())
	}
	return s.CLILinkStatus(ctx)
}
