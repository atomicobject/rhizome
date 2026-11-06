//go:build linux

package session

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	fakeCollectionPath = dbus.ObjectPath("/org/freedesktop/secrets/collection/test")
	fakeSessionPath    = dbus.ObjectPath("/org/freedesktop/secrets/session/test")
	fakePromptPath     = dbus.ObjectPath("/org/freedesktop/secrets/prompt/test")
)

type fakeSecretService struct {
	mu         sync.Mutex
	conn       *dbus.Conn
	records    map[string][]byte
	locked     bool
	prompt     string
	failure    string
	duplicates bool
}

func fakeStore(t *testing.T) (SecretService, *fakeSecretService) {
	t.Helper()
	tool, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is required for the isolated Secret Service fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, tool, "--session", "--nofork", "--print-address=1", "--address=unix:path="+filepath.Join(t.TempDir(), "bus"))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatal("private D-Bus daemon did not provide an address")
	}
	address := scanner.Text()
	conn, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	fake := &fakeSecretService{conn: conn, records: map[string][]byte{}}
	// Serialize startup and per-item exports through the same fixture lock.
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, export := range []struct {
		value any
		path  dbus.ObjectPath
		iface string
	}{
		{fake, "/org/freedesktop/secrets", secretServiceAPI + "Service"},
		{fakeCollection{fake}, fakeCollectionPath, secretServiceAPI + "Collection"},
		{fakeProperties{fake}, fakeCollectionPath, "org.freedesktop.DBus.Properties"},
		{fakePrompt{fake}, fakePromptPath, secretServiceAPI + "Prompt"},
	} {
		if err := conn.Export(export.value, export.path, export.iface); err != nil {
			t.Fatal(err)
		}
	}
	if reply, err := conn.RequestName(secretServiceName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("own synthetic Secret Service: %v, %v", reply, err)
	}
	return SecretService{connect: func(ctx context.Context) (*dbus.Conn, error) {
		return dbus.Connect(address, dbus.WithContext(ctx))
	}}, fake
}

func (*fakeSecretService) OpenSession(algorithm string, _ dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	if algorithm != "plain" {
		return dbus.MakeVariant(""), "/", dbus.MakeFailedError(errors.New("unsupported algorithm"))
	}
	return dbus.MakeVariant(""), fakeSessionPath, nil
}

type fakeCollection struct{ service *fakeSecretService }

func (c fakeCollection) SearchItems(attributes map[string]string) ([]dbus.ObjectPath, *dbus.Error) {
	f := c.service
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.records[attributes["origin"]]; attributes["service"] != secretStoreName || !ok {
		return []dbus.ObjectPath{}, nil
	}
	paths := []dbus.ObjectPath{fakeItemPath(attributes["origin"])}
	if f.duplicates {
		paths = append(paths, "/org/freedesktop/secrets/item/duplicate")
	}
	return paths, nil
}

func (*fakeSecretService) ReadAlias(alias string) (dbus.ObjectPath, *dbus.Error) {
	if alias == "default" {
		return fakeCollectionPath, nil
	}
	return "/", nil
}

func (f *fakeSecretService) Unlock(paths []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.locked {
		return []dbus.ObjectPath{}, fakePromptPath, nil
	}
	return paths, "/", nil
}

func (c fakeCollection) CreateItem(properties map[string]dbus.Variant, secret secretValue, replace bool) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	f := c.service
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failure == "save" {
		return "/", "/", syntheticStoreFailure()
	}
	attrs, ok := properties[secretServiceAPI+"Item.Attributes"].Value().(map[string]string)
	if !ok || attrs["service"] != secretStoreName || !replace || secret.Session != fakeSessionPath {
		return "/", "/", dbus.MakeFailedError(errors.New("invalid item request"))
	}
	origin := attrs["origin"]
	f.records[origin] = append([]byte(nil), secret.Value...)
	path := fakeItemPath(origin)
	if err := f.conn.Export(fakeItem{f, origin}, path, secretServiceAPI+"Item"); err != nil {
		return "/", "/", dbus.MakeFailedError(err)
	}
	return path, "/", nil
}

func fakeItemPath(origin string) dbus.ObjectPath {
	return dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/secrets/item/i%x", sha256.Sum256([]byte(origin))))
}

type fakeProperties struct{ service *fakeSecretService }

func (p fakeProperties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	p.service.mu.Lock()
	defer p.service.mu.Unlock()
	if iface != secretServiceAPI+"Collection" || name != "Locked" {
		return dbus.MakeVariant(""), dbus.MakeFailedError(errors.New("unknown property"))
	}
	return dbus.MakeVariant(p.service.locked), nil
}

type fakeItem struct {
	service *fakeSecretService
	origin  string
}

func (i fakeItem) GetSecret(session dbus.ObjectPath) (secretValue, *dbus.Error) {
	i.service.mu.Lock()
	defer i.service.mu.Unlock()
	if i.service.failure == "load" {
		return secretValue{}, syntheticStoreFailure()
	}
	return secretValue{Session: session, Parameters: []byte{}, Value: i.service.records[i.origin], ContentType: "application/octet-stream"}, nil
}

func (i fakeItem) Delete() (dbus.ObjectPath, *dbus.Error) {
	i.service.mu.Lock()
	defer i.service.mu.Unlock()
	if i.service.failure == "delete" {
		return "/", syntheticStoreFailure()
	}
	delete(i.service.records, i.origin)
	return "/", nil
}

type fakePrompt struct{ service *fakeSecretService }

func (p fakePrompt) Prompt(_ string) *dbus.Error {
	p.service.mu.Lock()
	mode := p.service.prompt
	if mode == "unlock" {
		p.service.locked = false
	}
	p.service.mu.Unlock()
	if mode != "blocked" {
		_ = p.service.conn.Emit(fakePromptPath, secretServiceAPI+"Prompt.Completed", mode != "unlock", dbus.MakeVariant([]dbus.ObjectPath{fakeCollectionPath}))
	}
	return nil
}

func syntheticStoreFailure() *dbus.Error {
	return dbus.NewError(secretServiceAPI+"Error.IsLocked", []any{"synthetic-secret-must-not-appear"})
}

func TestSecretServiceRoundTripAndOriginIsolation(t *testing.T) {
	store, _ := fakeStore(t)
	const origin = "https://team.example.invalid"
	if _, err := store.Load(origin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load absent = %v", err)
	}
	for _, record := range [][]byte{{0, 1, 127, 255}, []byte("synthetic replacement")} {
		if err := store.Save(origin, record); err != nil {
			t.Fatal(err)
		}
		if got, err := store.Load(origin); err != nil || !bytes.Equal(got, record) {
			t.Fatalf("Load did not preserve exact bytes: %v", err)
		}
	}
	if _, err := store.Load("https://other.example.invalid"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load another origin = %v", err)
	}
	for range 2 {
		if err := store.Delete(origin); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Load(origin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load after Delete = %v", err)
	}
}

func TestSecretServiceUnlockCancellationAndTimeout(t *testing.T) {
	for _, mode := range []string{"unlock", "dismiss", "blocked"} {
		t.Run(mode, func(t *testing.T) {
			store, fake := fakeStore(t)
			fake.mu.Lock()
			fake.locked, fake.prompt = true, mode
			fake.mu.Unlock()
			if mode == "blocked" {
				store.timeout = 100 * time.Millisecond
			}
			start := time.Now()
			err := store.Save("https://team.example.invalid", []byte("synthetic"))
			if mode == "unlock" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrStoreUnavailable) {
				t.Fatalf("Save = %v, want ErrStoreUnavailable", err)
			}
			if elapsed := time.Since(start); mode == "blocked" && elapsed > 2*time.Second {
				t.Fatalf("Save ignored its deadline: %s", elapsed)
			}
			fake.mu.Lock()
			defer fake.mu.Unlock()
			if mode != "unlock" && len(fake.records) != 0 {
				t.Fatal("failed unlock wrote a credential")
			}
		})
	}
}

func TestSecretServiceRefusesAmbiguousItemsAndRedactsErrors(t *testing.T) {
	store, fake := fakeStore(t)
	const origin = "https://team.example.invalid"
	if err := store.Save(origin, []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.duplicates = true
	fake.mu.Unlock()
	if _, err := store.Load(origin); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Load duplicate entries = %v", err)
	}
	fake.mu.Lock()
	fake.duplicates = false
	fake.mu.Unlock()
	for _, operation := range []string{"save", "load", "delete"} {
		fake.mu.Lock()
		fake.failure = operation
		fake.mu.Unlock()
		var err error
		switch operation {
		case "save":
			err = store.Save(origin, []byte("synthetic"))
		case "load":
			_, err = store.Load(origin)
		case "delete":
			err = store.Delete(origin)
		}
		if !errors.Is(err, ErrStoreUnavailable) || strings.Contains(err.Error(), "synthetic-secret-must-not-appear") {
			t.Fatalf("%s returned an unsafe error: %v", operation, err)
		}
	}
}

func TestSecretServiceIgnoresRepositoryBusEnvironment(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/tmp/repository-controlled-bus")
	t.Setenv("XDG_RUNTIME_DIR", "/tmp/repository-controlled-runtime")
	path, uid, err := secretServiceBusPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/run/user", uid, "bus"); path != want {
		t.Fatalf("bus path = %q, want %q", path, want)
	}
	store := SecretService{connect: func(context.Context) (*dbus.Conn, error) {
		return nil, errors.New("synthetic missing bus")
	}}
	if _, err := store.Load("https://team.example.invalid"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Load without bus = %v", err)
	}
}
