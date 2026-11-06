package appbundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/atomicobject/rhizome/pkg/fileio"
)

const ReleaseIdentifier = "com.atomicobject.rhizome"
const DevIdentifier = "com.atomicobject.rhizome.dev"

// Installation is shared with the native desktop host. The record describes
// ownership; repository paths and configuration never select its location.
type Installation struct {
	V          int    `json:"v"`
	Identifier string `json:"identifier"`
	Generation uint64 `json:"generation"`
	AppPath    string `json:"appPath"`
	Version    string `json:"version"`
	ImageHash  string `json:"imageHash"`
	Enabled    bool   `json:"enabled"`
}

var portableBaseDirectory = nativePortableBaseDirectory

// PortableRoot returns the real OS account's installation directory.
func PortableRoot(identifier string) (string, error) {
	if !validIdentifier(identifier) {
		return "", errors.New("invalid desktop application identifier")
	}
	base, err := portableBaseDirectory()
	if err != nil || base == "" {
		return "", err
	}
	base, err = canonicalDirectory(base)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, identifier), nil
}

func canonicalDirectory(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) || filepath.Dir(path) == path {
		return "", err
	}
	parent, err := canonicalDirectory(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

func validIdentifier(identifier string) bool {
	return identifier == ReleaseIdentifier || identifier == DevIdentifier
}

func portableRoot(root string) bool {
	want, err := PortableRoot(filepath.Base(root))
	return err == nil && want != "" && SamePath(want, root)
}

// SamePath compares installation path spellings, including Rust's canonical
// extended-length Windows paths, without following a final replacement symlink.
func SamePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(comparablePath(a), comparablePath(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func comparablePath(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" && strings.HasPrefix(path, `\\?\`) {
		if len(path) >= 8 && strings.EqualFold(path[4:8], `UNC\`) {
			return `\\` + path[8:]
		}
		if len(path) >= 7 && path[5] == ':' && path[6] == '\\' {
			return path[4:]
		}
	}
	return path
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "rzm.exe"
	}
	return "rzm"
}

// ImagePath addresses one immutable, app-owned executable.
func ImagePath(root, hash string) string {
	return filepath.Join(root, "images", hash, executableName())
}

func validHash(hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == sha256.Size && hash == strings.ToLower(hash)
}

// ReadInstallation reads a bounded record and validates its fixed owner.
func ReadInstallation(root string) (Installation, error) {
	var record Installation
	if !portableRoot(root) {
		return record, errors.New("desktop registration is outside the OS account's installation directory")
	}
	file, err := fileio.OpenRead(filepath.Join(root, "installation.json"))
	if err != nil {
		return record, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return record, errors.New("desktop registration exceeds its size limit or could not be read")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return record, fmt.Errorf("read desktop registration: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return record, errors.New("desktop registration must contain one JSON record")
	}
	if record.V != 1 || record.Generation == 0 || record.Identifier != filepath.Base(root) ||
		!filepath.IsAbs(record.AppPath) || record.Version == "" || !validHash(record.ImageHash) {
		return record, errors.New("desktop registration has an invalid identity, version, path, or image hash")
	}
	return record, nil
}

func readPortable(root string) (Info, error) {
	record, err := ReadInstallation(root)
	if err != nil {
		return Info{}, err
	}
	app, err := os.Stat(record.AppPath)
	if err != nil || !app.Mode().IsRegular() {
		return Info{}, errors.New("the registered Rhizome app is missing; reinstall it or remove its desktop integration")
	}
	return Info{
		Root: root, Identifier: record.Identifier, Version: record.Version,
		Dev: record.Identifier == DevIdentifier, AppPath: record.AppPath, Portable: true,
		ExecutablePath: ImagePath(root, record.ImageHash),
	}, nil
}

func resolvePortable(path string) (*Info, error) {
	root := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	if !portableRoot(root) || filepath.Base(filepath.Dir(filepath.Dir(path))) != "images" {
		return nil, nil
	}
	if filepath.Base(path) != executableName() || !validHash(filepath.Base(filepath.Dir(path))) {
		return nil, errors.New("invalid app-owned executable path")
	}
	info, err := readPortable(root)
	if err != nil {
		return nil, err
	}
	// An old process can keep its immutable image after the app updates.
	info.ExecutablePath = path
	return &info, nil
}

// OwnsPath also recognizes missing immutable paths, so a repository pin cannot
// claim an app-owned destination before its image has been materialized.
func OwnsPath(path string) bool {
	if Root(path) != "" {
		return true
	}
	base, err := portableBaseDirectory()
	if err != nil || base == "" {
		return false
	}
	base, err = canonicalDirectory(base)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(comparablePath(base), comparablePath(path))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// InstalledApps returns enabled, existing desktop installations in preference order.
func InstalledApps() ([]Info, error) {
	var apps []Info
	for _, identifier := range []string{ReleaseIdentifier, DevIdentifier} {
		root, err := PortableRoot(identifier)
		if err != nil {
			return nil, err
		}
		if root == "" {
			continue
		}
		record, err := ReadInstallation(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !record.Enabled {
			continue
		}
		info, err := readPortable(root)
		if err == nil {
			apps = append(apps, info)
		}
	}
	return apps, nil
}

func verifyImage(path, hash string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != hash {
		return errors.New("the app-owned executable failed its content check; reinstall Rhizome")
	}
	return nil
}
