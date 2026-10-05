package obsidian

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// GovernedMoveRequest describes a pure identifier-backed note rename plan.
type GovernedMoveRequest struct {
	SourcePath   string
	OldID        string
	NewID        string
	SiblingPaths []string
}

// GovernedMovePlan describes the normalized source and proposed destination.
// Renamed is false when the basename contains no governed identifier token.
type GovernedMovePlan struct {
	SourcePath      string
	DestinationPath string
	Renamed         bool
}

// MoveConflict reports an existing portable sibling destination.
type MoveConflict struct {
	DestinationPath string
	ExistingPath    string
}

// PlanGovernedIdentifierMove computes a filename move without touching the filesystem.
func PlanGovernedIdentifierMove(req GovernedMoveRequest) (GovernedMovePlan, *MoveConflict, error) {
	sourceInput := strings.TrimSpace(req.SourcePath)
	oldID := strings.TrimSpace(req.OldID)
	newID := strings.TrimSpace(req.NewID)
	if sourceInput == "" {
		return GovernedMovePlan{}, nil, fmt.Errorf("governed move source path is required")
	}
	if oldID == "" {
		return GovernedMovePlan{}, nil, fmt.Errorf("governed move old identifier is required")
	}
	if newID == "" {
		return GovernedMovePlan{}, nil, fmt.Errorf("governed move new identifier is required")
	}
	if strings.ContainsAny(oldID, "/\\\x00") || strings.ContainsAny(newID, "/\\\x00") {
		return GovernedMovePlan{}, nil, fmt.Errorf("governed move identifiers must be single filename tokens")
	}

	sourceRel, err := paths.CleanRelPath(sourceInput)
	if err != nil || sourceRel == "" {
		return GovernedMovePlan{}, nil, fmt.Errorf("governed move source path must be vault-relative: %w", err)
	}
	source := sourceRel.String()
	plan := GovernedMovePlan{
		SourcePath:      source,
		DestinationPath: source,
	}
	if oldID == newID {
		return plan, nil, nil
	}

	newBase, changed := rewriteGovernedIdentifierTokens(path.Base(source), oldID, newID)
	if !changed {
		return plan, nil, nil
	}

	dir := path.Dir(source)
	destination := newBase
	if dir != "." {
		destination = path.Join(dir, newBase)
	}
	destinationRel, err := paths.CleanRelPath(destination)
	if err != nil || destinationRel == "" {
		return GovernedMovePlan{}, nil, fmt.Errorf("governed move destination path must be vault-relative: %w", err)
	}
	plan.DestinationPath = destinationRel.String()
	plan.Renamed = plan.DestinationPath != plan.SourcePath
	if !plan.Renamed {
		return plan, nil, nil
	}

	siblings := make([]string, 0, len(req.SiblingPaths))
	for _, sibling := range req.SiblingPaths {
		if strings.TrimSpace(sibling) == "" {
			continue
		}
		siblingRel, err := paths.CleanRelPath(sibling)
		if err != nil || siblingRel == "" {
			return GovernedMovePlan{}, nil, fmt.Errorf("governed move sibling path must be vault-relative: %w", err)
		}
		siblings = append(siblings, siblingRel.String())
	}
	sort.Strings(siblings)
	destinationDir := path.Dir(plan.DestinationPath)
	for _, sibling := range siblings {
		if sibling == plan.SourcePath {
			continue
		}
		if !strings.EqualFold(path.Dir(sibling), destinationDir) {
			continue
		}
		if sibling == plan.DestinationPath || strings.EqualFold(path.Base(sibling), path.Base(plan.DestinationPath)) {
			return plan, &MoveConflict{
				DestinationPath: plan.DestinationPath,
				ExistingPath:    sibling,
			}, nil
		}
	}

	return plan, nil, nil
}

func rewriteGovernedIdentifierTokens(basename, oldID, newID string) (string, bool) {
	var out strings.Builder
	cursor := 0
	searchFrom := 0
	changed := false
	for searchFrom <= len(basename)-len(oldID) {
		relative := strings.Index(basename[searchFrom:], oldID)
		if relative < 0 {
			break
		}
		start := searchFrom + relative
		end := start + len(oldID)
		if governedIdentifierBoundaryBefore(basename, start) && governedIdentifierBoundaryAfter(basename, end) {
			out.WriteString(basename[cursor:start])
			out.WriteString(newID)
			cursor = end
			changed = true
		}
		searchFrom = end
	}
	if !changed {
		return basename, false
	}
	out.WriteString(basename[cursor:])
	return out.String(), true
}

func governedIdentifierBoundaryBefore(value string, offset int) bool {
	if offset == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(value[:offset])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

func governedIdentifierBoundaryAfter(value string, offset int) bool {
	if offset == len(value) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(value[offset:])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}
