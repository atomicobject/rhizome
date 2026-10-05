package sqliteutil

import (
	"path/filepath"
	"strings"
)

func pathWithinMount(path, mount string) bool {
	if mount == "/" {
		return strings.HasPrefix(path, "/")
	}
	return path == mount || strings.HasPrefix(path, mount+string(filepath.Separator))
}

func unescapeMountInfo(value string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return replacer.Replace(value)
}
