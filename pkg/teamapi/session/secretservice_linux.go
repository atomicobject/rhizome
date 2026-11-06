//go:build linux

package session

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	secretServiceName = "org.freedesktop.secrets"
	secretServiceAPI  = "org.freedesktop.Secret."
	secretStoreName   = "rhizome-team-session"
)

// SecretService uses the OS user's desktop keyring, never a repository's bus.
type SecretService struct {
	connect func(context.Context) (*dbus.Conn, error)
	timeout time.Duration
}

// DefaultStore uses Secret Service without a file fallback.
func DefaultStore() (Store, error) { return SecretService{}, nil }

type secretValue struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

type secretConnection struct {
	ctx     context.Context
	conn    *dbus.Conn
	session dbus.ObjectPath
}

func secretServiceBusPath() (string, string, error) {
	account, err := user.Current()
	if err != nil {
		return "", "", err
	}
	if _, err := strconv.ParseUint(account.Uid, 10, 32); err != nil {
		return "", "", err
	}
	return filepath.Join("/run/user", account.Uid, "bus"), account.Uid, nil
}

func connectSecretService(ctx context.Context) (*dbus.Conn, error) {
	path, uid, err := secretServiceBusPath()
	if err != nil {
		return nil, err
	}
	var dialer net.Dialer
	socket, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	conn, err := dbus.ConnectUnix(socket.(*net.UnixConn), dbus.WithContext(ctx), dbus.WithAuth(dbus.AuthExternal(uid)))
	if err != nil {
		_ = socket.Close()
	}
	return conn, err
}

func secretStoreError() error {
	// Service errors may echo method arguments, so never include their bodies.
	return fmt.Errorf("%w: unlock the desktop keyring and retry; a running Secret Service is required", ErrStoreUnavailable)
}

func (s SecretService) open(origin string) (*secretConnection, func(), error) {
	canonical, err := CanonicalOrigin(origin)
	if err != nil || canonical != origin {
		return nil, nil, fmt.Errorf("%w: invalid session origin", ErrStoreUnavailable)
	}
	timeout := s.timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	connect := s.connect
	if connect == nil {
		connect = connectSecretService
	}
	conn, err := connect(ctx)
	if err != nil {
		cancel()
		return nil, nil, secretStoreError()
	}
	close := func() { _ = conn.Close(); cancel() }
	c := &secretConnection{ctx: ctx, conn: conn}
	var output dbus.Variant
	if err := c.call("/org/freedesktop/secrets", "Service.OpenSession", "plain", dbus.MakeVariant("")).Store(&output, &c.session); err != nil || c.session == "/" || !c.session.IsValid() {
		close()
		return nil, nil, secretStoreError()
	}
	return c, close, nil
}

func (c *secretConnection) call(path dbus.ObjectPath, method string, args ...any) *dbus.Call {
	return c.conn.Object(secretServiceName, path).CallWithContext(c.ctx, secretServiceAPI+method, 0, args...)
}

func secretAttributes(origin string) map[string]string {
	return map[string]string{"service": secretStoreName, "origin": origin}
}

func (c *secretConnection) find(origin string) (dbus.ObjectPath, error) {
	collection, err := c.collection()
	if err != nil {
		return "", err
	}
	var items []dbus.ObjectPath
	if err := c.call(collection, "Collection.SearchItems", secretAttributes(origin)).Store(&items); err != nil {
		return "", secretStoreError()
	}
	if len(items) == 0 {
		return "", ErrNotFound
	}
	if len(items) != 1 || items[0] == "/" || !items[0].IsValid() {
		return "", secretStoreError()
	}
	return items[0], nil
}

func (c *secretConnection) collection() (dbus.ObjectPath, error) {
	var path dbus.ObjectPath
	if err := c.call("/org/freedesktop/secrets", "Service.ReadAlias", "default").Store(&path); err != nil || path == "/" || !path.IsValid() {
		return "", secretStoreError()
	}
	return path, nil
}

func (c *secretConnection) prompt(path dbus.ObjectPath) error {
	if path == "/" {
		return nil
	}
	if !path.IsValid() {
		return secretStoreError()
	}
	match := []dbus.MatchOption{dbus.WithMatchSender(secretServiceName), dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(secretServiceAPI + "Prompt"), dbus.WithMatchMember("Completed")}
	if err := c.conn.AddMatchSignalContext(c.ctx, match...); err != nil {
		return secretStoreError()
	}
	signals := make(chan *dbus.Signal, 1)
	c.conn.Signal(signals)
	defer c.conn.RemoveSignal(signals)
	if err := c.call(path, "Prompt.Prompt", "").Err; err != nil {
		return secretStoreError()
	}
	for {
		select {
		case signal, ok := <-signals:
			if !ok {
				return secretStoreError()
			}
			if signal.Path != path || signal.Name != secretServiceAPI+"Prompt.Completed" {
				continue
			}
			if len(signal.Body) != 2 {
				return secretStoreError()
			}
			dismissed, ok := signal.Body[0].(bool)
			if !ok || dismissed {
				return secretStoreError()
			}
			return nil
		case <-c.ctx.Done():
			return secretStoreError()
		}
	}
}

func (c *secretConnection) unlock(path dbus.ObjectPath) error {
	var locked dbus.Variant
	if err := c.conn.Object(secretServiceName, path).CallWithContext(c.ctx, "org.freedesktop.DBus.Properties.Get", 0, secretServiceAPI+"Collection", "Locked").Store(&locked); err != nil {
		return secretStoreError()
	}
	isLocked, ok := locked.Value().(bool)
	if !ok {
		return secretStoreError()
	}
	if !isLocked {
		return nil
	}
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := c.call("/org/freedesktop/secrets", "Service.Unlock", []dbus.ObjectPath{path}).Store(&unlocked, &prompt); err != nil {
		return secretStoreError()
	}
	return c.prompt(prompt)
}

func (s SecretService) Load(origin string) ([]byte, error) {
	c, close, err := s.open(origin)
	if err != nil {
		return nil, err
	}
	defer close()
	item, err := c.find(origin)
	if err != nil {
		return nil, err
	}
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := c.call("/org/freedesktop/secrets", "Service.Unlock", []dbus.ObjectPath{item}).Store(&unlocked, &prompt); err != nil || c.prompt(prompt) != nil {
		return nil, secretStoreError()
	}
	return c.read(item)
}

func (c *secretConnection) read(item dbus.ObjectPath) ([]byte, error) {
	var secret secretValue
	if err := c.call(item, "Item.GetSecret", c.session).Store(&secret); err != nil || secret.Session != c.session || len(secret.Parameters) != 0 {
		return nil, secretStoreError()
	}
	return append([]byte(nil), secret.Value...), nil
}

func (s SecretService) Save(origin string, record []byte) error {
	c, close, err := s.open(origin)
	if err != nil {
		return err
	}
	defer close()
	collection, err := c.collection()
	if err != nil {
		return err
	}
	if err := c.unlock(collection); err != nil {
		return err
	}
	properties := map[string]dbus.Variant{
		secretServiceAPI + "Item.Label":      dbus.MakeVariant("Rhizome team session"),
		secretServiceAPI + "Item.Attributes": dbus.MakeVariant(secretAttributes(origin)),
	}
	secret := secretValue{Session: c.session, Parameters: []byte{}, Value: record, ContentType: "application/octet-stream"}
	var item, prompt dbus.ObjectPath
	if err := c.call(collection, "Collection.CreateItem", properties, secret, true).Store(&item, &prompt); err != nil {
		return secretStoreError()
	}
	if err := c.prompt(prompt); err != nil {
		return err
	}
	item, err = c.find(origin)
	if err != nil {
		return secretStoreError()
	}
	stored, err := c.read(item)
	if err != nil || !bytes.Equal(stored, record) {
		return secretStoreError()
	}
	return nil
}

func (s SecretService) Delete(origin string) error {
	c, close, err := s.open(origin)
	if err != nil {
		return err
	}
	defer close()
	item, err := c.find(origin)
	if err == ErrNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	var prompt dbus.ObjectPath
	if err := c.call(item, "Item.Delete").Store(&prompt); err != nil {
		return secretStoreError()
	}
	return c.prompt(prompt)
}
