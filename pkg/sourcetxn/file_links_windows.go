//go:build windows

package sourcetxn

import (
	"golang.org/x/sys/windows"
	"os"
)

func fileHasMultipleLinks(path string, _ os.FileInfo) (bool, error) {
	count, err := fileLinkCount(path, nil)
	return count > 1, err
}

func fileLinkCount(path string, _ os.FileInfo) (uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		return 0, err
	}
	return uint64(info.NumberOfLinks), nil
}
