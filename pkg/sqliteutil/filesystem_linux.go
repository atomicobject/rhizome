//go:build linux

package sqliteutil

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func filesystemType(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return "", err
	}
	defer f.Close()

	bestMount := ""
	bestType := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		separator := -1
		for i, field := range fields {
			if field == "-" {
				separator = i
				break
			}
		}
		if separator < 0 || separator+1 >= len(fields) || len(fields) < 5 {
			continue
		}
		mount := unescapeMountInfo(fields[4])
		if pathWithinMount(abs, mount) && len(mount) > len(bestMount) {
			bestMount = mount
			bestType = fields[separator+1]
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if bestType == "" {
		return "", fmt.Errorf("filesystem mount not found for %q", abs)
	}
	return bestType, nil
}
