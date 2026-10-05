package init

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type docState struct {
	existing string
	exists   bool
	stale    []string
}

// declinedRemoval marks a removal someone chose not to make.
const declinedRemoval = "keep"

// staleTemplateBlocks lists template blocks in content that the doc no longer
// plans and does not preserve.
func staleTemplateBlocks(content string, doc generatedDoc) []string {
	keep := stringSet(normalizePreservedTemplateBlockIDs(doc.preserve))
	for _, block := range doc.blocks {
		keep[block.key] = true
	}
	var stale []string
	rest := content
	for {
		at := strings.Index(rest, managedTemplateBlockStartPrefix)
		if at < 0 {
			return stale
		}
		rest = rest[at+len(managedTemplateBlockStartPrefix):]
		end := strings.Index(rest, managedTemplateBlockSuffix)
		if end < 0 {
			return stale
		}
		key := strings.TrimSpace(rest[:end])
		if !keep[strings.ToLower(key)] && !contains(stale, key) {
			stale = append(stale, key)
		}
	}
}

func classifyPlan(projectRoot string, plan *filePlan, record *generatedFiles) ([]*change, []docState, error) {
	var changes []*change
	for _, file := range plan.files {
		fingerprint := file.fingerprint
		if fingerprint == nil {
			fingerprint = contentFingerprint
		}
		current, exists, err := readIfExists(filepath.Join(projectRoot, filepath.FromSlash(file.rel)))
		if err != nil {
			return nil, nil, err
		}
		c := &change{key: file.rel, rel: file.rel, group: firstNonEmpty(file.group, file.rel), current: current, desired: file.content, desiredF: fingerprint(file.content)}
		c.state = classify(exists, current, c.desiredF, fingerprint, record, file.rel)
		if file.teamOwned && !file.refresh && exists {
			// Team-owned docs are only created. An identical copy is recorded so
			// a later requested refresh can tell it was never edited.
			if c.state == stateCurrent {
				record.recordWritten(file.rel, c.desiredF)
			}
			continue
		}
		changes = append(changes, c)
	}

	docStates := make([]docState, len(plan.docs))
	for i, doc := range plan.docs {
		current, exists, err := readIfExists(filepath.Join(projectRoot, filepath.FromSlash(doc.rel)))
		if err != nil {
			return nil, nil, err
		}
		docStates[i] = docState{existing: string(current), exists: exists}
		// Blocks of starters that are no longer active get the same edit
		// check as files Rhizome stops shipping.
		for _, key := range staleTemplateBlocks(string(current), doc) {
			body, _ := managedBlockBody(string(current), key)
			blockKey := doc.rel + "#" + key
			c := &change{key: blockKey, rel: doc.rel, group: blockKey, removal: true, current: []byte(body)}
			written, recorded := record.Written[blockKey]
			switch {
			case recorded && written != contentFingerprint([]byte(body)) && record.Declined[blockKey] == declinedRemoval:
				c.state = stateDeclined
			case recorded && written != contentFingerprint([]byte(body)):
				c.state = stateEdited
			default:
				// Fences mark an unrecorded block as Rhizome's.
				c.state = stateUnedited
			}
			changes = append(changes, c)
			docStates[i].stale = append(docStates[i].stale, key)
		}
		for _, block := range doc.blocks {
			body, present := managedBlockBody(string(current), block.key)
			desired := []byte(strings.TrimSpace(block.content))
			key := doc.rel + "#" + block.key
			c := &change{key: key, rel: doc.rel, group: key, current: []byte(body), desired: desired, desiredF: contentFingerprint(desired)}
			c.state = classify(present, []byte(body), c.desiredF, contentFingerprint, record, key)
			if c.state == stateUnknown {
				// The fences already mark the block as Rhizome's, so a block
				// from before the record existed updates like an unedited file.
				c.state = stateUnedited
			}
			changes = append(changes, c)
		}
	}

	for _, removal := range plan.removals {
		current, exists, err := readIfExists(filepath.Join(projectRoot, filepath.FromSlash(removal.rel)))
		if err != nil {
			return nil, nil, err
		}
		if !exists {
			record.forget(removal.rel)
			continue
		}
		c := &change{key: removal.rel, rel: removal.rel, group: firstNonEmpty(removal.group, removal.rel), removal: true, current: current}
		written, recorded := record.Written[removal.rel]
		switch {
		case record.Declined[removal.rel] == declinedRemoval:
			c.state = stateDeclined
		case !recorded && removal.retired:
			c.state = stateUnedited
		case !recorded:
			c.state = stateUnknown
		case written == contentFingerprint(current):
			c.state = stateUnedited
		default:
			c.state = stateEdited
		}
		changes = append(changes, c)
	}
	return changes, docStates, nil
}

func classify(exists bool, current []byte, desiredF string, fingerprint func([]byte) string, record *generatedFiles, key string) fileState {
	if !exists {
		return stateMissing
	}
	currentF := fingerprint(current)
	if currentF == desiredF {
		return stateCurrent
	}
	written, recorded := record.Written[key]
	switch {
	case recorded && written == currentF:
		return stateUnedited
	case recorded && written == desiredF:
		return stateLocal
	case record.Declined[key] == desiredF:
		return stateDeclined
	case recorded:
		return stateEdited
	default:
		return stateUnknown
	}
}

// resolveChanges decides every change. Missing and unedited targets are taken
// without asking. Unknown targets get one grouped question first, then edited
// targets are asked about once per group. Without a UI nothing edited is taken.
func resolveChanges(changes []*change, ui ownershipUI) {
	for _, c := range changes {
		c.take = c.state == stateMissing || c.state == stateUnedited
	}
	if ui == nil {
		return
	}
	var unknownUpdates, unknownRemovals []*change
	for _, c := range changes {
		if c.state != stateUnknown {
			continue
		}
		if c.removal {
			unknownRemovals = append(unknownRemovals, c)
		} else {
			unknownUpdates = append(unknownUpdates, c)
		}
	}
	// Updates and removals are separate questions so a deletion is never
	// hidden in a list that defaults to yes.
	for _, unknown := range [][]*change{unknownUpdates, unknownRemovals} {
		if len(unknown) == 0 {
			continue
		}
		switch ui.decideUnknown(unknown) {
		case "update":
			for _, c := range unknown {
				c.take, c.asked = true, true
			}
		case "review":
			for _, c := range unknown {
				c.state = stateEdited
			}
		default:
			for _, c := range unknown {
				c.asked = true
			}
		}
	}
	decided := map[string]bool{}
	mirrors := map[string]int{}
	for _, c := range changes {
		if c.state == stateEdited {
			mirrors[c.group]++
		}
	}
	for _, c := range changes {
		if c.state != stateEdited {
			continue
		}
		take, ok := decided[c.group]
		if !ok {
			take = ui.decideEdited(c, mirrors[c.group]-1)
			decided[c.group] = take
		}
		c.take, c.asked = take, true
	}
}

func applyFileChange(projectRoot string, file generatedFile, c *change, record *generatedFiles, report *syncReport) error {
	if c == nil {
		return nil
	}
	switch {
	case c.state == stateCurrent:
		record.recordWritten(c.key, c.desiredF)
		return nil
	case c.take:
		path := filepath.Join(projectRoot, filepath.FromSlash(file.rel))
		if err := ensureDirExists(filepath.Dir(path)); err != nil {
			if IsSkippablePathError(err) {
				report.Warnings = append(report.Warnings, fmt.Sprintf("skipping %s: %v", file.rel, err))
				return nil
			}
			return err
		}
		if err := os.WriteFile(path, file.content, file.mode); err != nil {
			return err
		}
		if err := os.Chmod(path, file.mode); err != nil {
			return err
		}
		record.recordWritten(c.key, c.desiredF)
		if c.state == stateMissing {
			report.Created = append(report.Created, file.rel)
		} else {
			report.Updated = append(report.Updated, file.rel)
		}
	case c.state == stateEdited || c.state == stateUnknown:
		if c.asked {
			record.recordDeclined(c.key, c.desiredF)
			report.Declined = append(report.Declined, file.rel)
		} else {
			report.Kept = append(report.Kept, file.rel)
		}
	}
	return nil
}

func applyDocChanges(projectRoot string, doc generatedDoc, state docState, byKey map[string]*change, record *generatedFiles, report *syncReport) error {
	for _, block := range doc.blocks {
		c := byKey[doc.rel+"#"+block.key]
		switch {
		case c.take || c.state == stateCurrent:
			record.recordWritten(c.key, c.desiredF)
		case c.asked:
			record.recordDeclined(c.key, c.desiredF)
			report.Declined = append(report.Declined, c.key)
		case c.state == stateEdited || c.state == stateUnknown:
			report.Kept = append(report.Kept, c.key)
		}
	}
	for _, key := range state.stale {
		c := byKey[doc.rel+"#"+key]
		switch {
		case c.take:
			record.forget(c.key)
		case c.asked:
			record.recordDeclined(c.key, declinedRemoval)
			report.Declined = append(report.Declined, c.key)
		case c.state == stateEdited:
			report.Kept = append(report.Kept, c.key)
		}
	}
	next := renderDoc(doc, state, byKey)
	if state.exists && next == state.existing {
		return nil
	}
	path := filepath.Join(projectRoot, filepath.FromSlash(doc.rel))
	if err := ensureDirExists(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return err
	}
	if state.exists {
		report.Updated = append(report.Updated, doc.rel)
	} else {
		report.Created = append(report.Created, doc.rel)
	}
	return nil
}

// renderDoc renders a shared instruction file with the decided blocks:
// taken blocks get the new version, kept ones stay as they are.
func renderDoc(doc generatedDoc, state docState, byKey map[string]*change) string {
	blocks := make([]managedDocBlock, 0, len(doc.blocks))
	preserve := append([]string(nil), doc.preserve...)
	for _, block := range doc.blocks {
		c := byKey[doc.rel+"#"+block.key]
		switch {
		case c.take || c.state == stateCurrent:
			blocks = append(blocks, block)
		case block.key == "rhizome":
			// The core block is replaced in place, so keeping it means writing
			// its current text back unchanged.
			blocks = append(blocks, managedDocBlock{key: block.key, content: string(c.current)})
		default:
			preserve = append(preserve, block.key)
		}
	}
	for _, key := range state.stale {
		if !byKey[doc.rel+"#"+key].take {
			preserve = append(preserve, key)
		}
	}
	return renderAgentHarnessDocWithOptions(state.existing, doc.header, blocks, preserve)
}

func applyRemovals(projectRoot string, plan *filePlan, byKey map[string]*change, record *generatedFiles, report *syncReport) error {
	desired := map[string][]byte{}
	for _, file := range plan.files {
		desired[file.rel] = file.content
	}
	pruneDirs := map[string]bool{}
	for _, removal := range plan.removals {
		c := byKey[removal.rel]
		if c == nil || !c.removal {
			continue
		}
		if !c.take {
			if c.asked {
				record.recordDeclined(removal.rel, declinedRemoval)
				report.Declined = append(report.Declined, removal.rel)
			} else if c.state == stateEdited || c.state == stateUnknown {
				report.Kept = append(report.Kept, removal.rel)
			}
			continue
		}
		if removal.requires != "" && !fileMatches(projectRoot, removal.requires, desired[removal.requires]) {
			continue
		}
		if err := os.Remove(filepath.Join(projectRoot, filepath.FromSlash(removal.rel))); err != nil && !os.IsNotExist(err) {
			return err
		}
		record.forget(removal.rel)
		report.Removed = append(report.Removed, removal.rel)
		if removal.pruneDir != "" {
			pruneDirs[removal.pruneDir] = true
		}
	}
	dirs := make([]string, 0, len(pruneDirs))
	for dir := range pruneDirs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		if err := removeEmptySkillDir(filepath.Join(projectRoot, filepath.FromSlash(dir))); err != nil {
			return err
		}
	}
	return nil
}

func fileMatches(projectRoot, rel string, want []byte) bool {
	got, err := os.ReadFile(filepath.Join(projectRoot, filepath.FromSlash(rel)))
	return err == nil && contentFingerprint(got) == contentFingerprint(want)
}

// removeEmptySkillDir removes a skill folder once only legacy marker files and
// empty subfolders remain.
func removeEmptySkillDir(dir string) error {
	empty := true
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Dir(path) == dir {
			for _, legacy := range legacyManagedSkillFiles {
				if d.Name() == legacy {
					return nil
				}
			}
		}
		empty = false
		return filepath.SkipAll
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !empty {
		return nil
	}
	return os.RemoveAll(dir)
}

func readIfExists(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

// managedBlockBody returns the trimmed body of a managed block and whether the
// block exists in content.
func managedBlockBody(content, key string) (string, bool) {
	start, end := managedRhizomeBlockStart, managedRhizomeBlockEnd
	if key != "rhizome" {
		start = managedTemplateBlockStartPrefix + key + managedTemplateBlockSuffix
		end = managedTemplateBlockEndPrefix + key + managedTemplateBlockSuffix
	}
	if !strings.Contains(content, start) || !strings.Contains(content, end) {
		return "", false
	}
	return managedBlockContent(content, start, end), true
}
