//go:build windows

package session

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const credentialBlobLimit = 2560 // CRED_MAX_CREDENTIAL_BLOB_SIZE.

var (
	credentialDLL    = windows.NewLazySystemDLL("advapi32.dll")
	credentialRead   = credentialDLL.NewProc("CredReadW")
	credentialWrite  = credentialDLL.NewProc("CredWriteW")
	credentialDelete = credentialDLL.NewProc("CredDeleteW")
	credentialFree   = credentialDLL.NewProc("CredFree")
)

// CredentialManager stores opaque session records in the current user's vault.
type CredentialManager struct{}

// DefaultStore uses Windows Credential Manager without a file fallback.
func DefaultStore() (Store, error) { return CredentialManager{}, nil }

type windowsCredential struct {
	Flags, Type         uint32
	TargetName, Comment *uint16
	LastWritten         windows.Filetime
	BlobSize            uint32
	Blob                *byte
	Persist, Attributes uint32
	AttributeList       uintptr
	TargetAlias, User   *uint16
}

func credentialTarget(origin string) (*uint16, error) {
	canonical, err := CanonicalOrigin(origin)
	if err != nil || canonical != origin {
		return nil, fmt.Errorf("%w: invalid session origin", ErrStoreUnavailable)
	}
	return windows.UTF16PtrFromString("rhizome-team-session:" + origin)
}

func credentialError(err error) error {
	return fmt.Errorf("%w: Windows Credential Manager refused access: %v", ErrStoreUnavailable, err)
}

func (CredentialManager) Load(origin string) ([]byte, error) {
	target, err := credentialTarget(origin)
	if err != nil {
		return nil, err
	}
	var credential *windowsCredential
	ok, _, callErr := credentialRead.Call(uintptr(unsafe.Pointer(target)), 1, 0, uintptr(unsafe.Pointer(&credential)))
	if ok == 0 {
		if errors.Is(callErr, windows.ERROR_NOT_FOUND) {
			return nil, ErrNotFound
		}
		return nil, credentialError(callErr)
	}
	defer credentialFree.Call(uintptr(unsafe.Pointer(credential)))
	if credential == nil || credential.BlobSize > credentialBlobLimit || (credential.BlobSize > 0 && credential.Blob == nil) {
		return nil, fmt.Errorf("%w: Windows Credential Manager returned an invalid record", ErrStoreUnavailable)
	}
	return append([]byte(nil), unsafe.Slice(credential.Blob, credential.BlobSize)...), nil
}

func (CredentialManager) Save(origin string, record []byte) error {
	target, err := credentialTarget(origin)
	if err != nil {
		return err
	}
	if len(record) > credentialBlobLimit {
		return fmt.Errorf("%w: session record exceeds Windows Credential Manager's %d-byte limit", ErrStoreUnavailable, credentialBlobLimit)
	}
	credential := windowsCredential{Type: 1, TargetName: target, BlobSize: uint32(len(record)), Persist: 2}
	if len(record) > 0 {
		credential.Blob = &record[0]
	}
	ok, _, callErr := credentialWrite.Call(uintptr(unsafe.Pointer(&credential)), 0)
	runtime.KeepAlive(record)
	if ok == 0 {
		return credentialError(callErr)
	}
	return nil
}

func (CredentialManager) Delete(origin string) error {
	target, err := credentialTarget(origin)
	if err != nil {
		return err
	}
	ok, _, callErr := credentialDelete.Call(uintptr(unsafe.Pointer(target)), 1, 0)
	if ok == 0 && !errors.Is(callErr, windows.ERROR_NOT_FOUND) {
		return credentialError(callErr)
	}
	return nil
}
