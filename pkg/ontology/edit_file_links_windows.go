//go:build windows

package ontology

import "os"

func editFileHasMultipleLinks(os.FileInfo) bool { return false }
