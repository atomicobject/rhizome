//go:build !windows

package appbundle

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
)

func nativePortableBaseDirectory() (string, error) {
	if runtime.GOOS == "darwin" {
		return "", nil
	}
	home, err := nativeAccountHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "rhizome", "desktop"), nil
}

func nativeAccountHome() (string, error) {
	account, err := user.LookupId(strconv.Itoa(os.Getuid()))
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(account.HomeDir) {
		return "", errors.New("the operating-system account home is unavailable")
	}
	return account.HomeDir, nil
}
