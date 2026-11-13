package init

import (
	"fmt"
	"path/filepath"
)

func classifyReplacement(root string, file generatedFile, c *change, fingerprint func([]byte) string, record *generatedFiles) error {
	current, exists, err := readIfExists(filepath.Join(root, filepath.FromSlash(file.replaces)))
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%s changed while setup was being prepared; retry setup", file.replaces)
	}
	c.from, c.current = file.replaces, current
	c.state = classify(exists, current, c.desiredF, fingerprint, record, file.replaces)
	if c.state == stateCurrent {
		c.state = stateUnedited
	}
	if c.state == stateLocal {
		c.state = stateEdited
	}
	if record.Declined[file.rel] == c.desiredF {
		c.state = stateDeclined
	}
	if record.declinedSources == nil {
		record.declinedSources = map[string]string{}
	}
	record.declinedSources[c.key] = c.from
	return nil
}
