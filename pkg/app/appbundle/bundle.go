// Package appbundle identifies the macOS application that owns an executable.
package appbundle

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var plistValue = func(path, key string) ([]byte, error) {
	return exec.Command("/usr/bin/plutil", "-extract", key, "raw", "-o", "-", path).Output()
}

// Info describes an executable's enclosing macOS application.
type Info struct {
	ExecutablePath string
	Root           string
	Identifier     string
	Version        string
	Dev            bool
}

// Root returns the lexical *.app root for a path below its Contents directory.
// It does not require the path to exist, so callers can identify dangling links.
func Root(path string) string {
	path = filepath.Clean(path)
	for dir := filepath.Dir(path); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if filepath.Base(dir) == "Contents" && strings.HasSuffix(filepath.Dir(dir), ".app") {
			return filepath.Dir(dir)
		}
	}
	return ""
}

// Resolve follows symbolic links before identifying the enclosing application.
// A nil result means the executable is outside an application bundle.
func Resolve(path string) (*Info, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && Root(path) == "" {
			return nil, nil
		}
		return nil, err
	}
	root := Root(resolved)
	if root == "" {
		return nil, nil
	}
	info, err := Read(root)
	if err != nil {
		return nil, err
	}
	info.ExecutablePath = resolved
	return &info, nil
}

// Read reads the bundle's identifier and short version from Info.plist.
func Read(root string) (Info, error) {
	path := filepath.Join(root, "Contents", "Info.plist")
	data, err := os.ReadFile(path)
	if err != nil {
		return Info{}, err
	}
	values := map[string]string{}
	if strings.HasPrefix(string(data), "bplist") {
		for _, key := range []string{"CFBundleIdentifier", "CFBundleShortVersionString"} {
			out, err := plistValue(path, key)
			if err != nil {
				return Info{}, fmt.Errorf("read %s: %w", key, err)
			}
			values[key] = strings.TrimSpace(string(out))
		}
	} else {
		decoder := xml.NewDecoder(strings.NewReader(string(data)))
		key := ""
		for {
			token, err := decoder.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return Info{}, fmt.Errorf("read Info.plist: %w", err)
			}
			start, ok := token.(xml.StartElement)
			if !ok {
				continue
			}
			switch start.Name.Local {
			case "key":
				if err := decoder.DecodeElement(&key, &start); err != nil {
					return Info{}, err
				}
			case "string":
				var value string
				if err := decoder.DecodeElement(&value, &start); err != nil {
					return Info{}, err
				}
				values[key] = value
				key = ""
			}
		}
	}
	identifier, version := strings.TrimSpace(values["CFBundleIdentifier"]), strings.TrimSpace(values["CFBundleShortVersionString"])
	if identifier == "" || version == "" {
		return Info{}, fmt.Errorf("Info.plist requires CFBundleIdentifier and CFBundleShortVersionString")
	}
	return Info{Root: root, Identifier: identifier, Version: version, Dev: strings.HasSuffix(identifier, ".dev")}, nil
}
