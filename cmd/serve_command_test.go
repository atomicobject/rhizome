package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/fileio"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeCommandPrintsURLAndRegistersInstance(t *testing.T) {
	testServeLikeCommand(t, "serve", false)
}

func TestServeCommandReuseDiscoveryPortFlag(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n",
	})
	require.NoError(t, appruntime.WriteLastPort(vault.path, 45123))

	ctx, cancel := context.WithCancel(context.Background())

	cmd := newServeCommand("serve", "test", false)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{
		"--vault", vault.name,
		"--host", "127.0.0.1",
		"--port", "0",
		"--reuse-discovery-port",
	})

	origListen := serveListen

	listener := newStubListener("127.0.0.1", 45123)
	addresses := make(chan string, 1)
	serveListen = func(network, address string) (net.Listener, error) {
		addresses <- address
		return listener, nil
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.ExecuteContext(ctx)
	}()
	finished := false
	cleanupServeCommand(t, cancel, done, &finished, origListen)

	select {
	case address := <-addresses:
		require.Equal(t, "127.0.0.1:45123", address)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not bind in time")
	}

	waitForPublishedServeManifest(t, vault.path, 45123)
	cancel()

	select {
	case err := <-done:
		finished = true
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop after cancel")
	}
	require.NoFileExists(t, appruntime.ManifestPath(vault.path))
}

func TestServeCommandReuseDiscoveryPortFallsBackWhenPortIsBusy(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n",
	})
	require.NoError(t, appruntime.WriteLastPort(vault.path, 45123))

	ctx, cancel := context.WithCancel(context.Background())

	cmd := newServeCommand("serve", "test", false)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{
		"--vault", vault.name,
		"--host", "127.0.0.1",
		"--port", "0",
		"--reuse-discovery-port",
	})

	origListen := serveListen

	listener := newStubListener("127.0.0.1", 46231)
	addresses := make(chan string, 2)
	serveListen = func(network, address string) (net.Listener, error) {
		addresses <- address
		if address == "127.0.0.1:45123" {
			return nil, fmt.Errorf("listen tcp %s: %w", address, syscall.EADDRINUSE)
		}
		return listener, nil
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.ExecuteContext(ctx)
	}()
	finished := false
	cleanupServeCommand(t, cancel, done, &finished, origListen)

	select {
	case address := <-addresses:
		require.Equal(t, "127.0.0.1:45123", address)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not attempt discovery port in time")
	}

	select {
	case address := <-addresses:
		require.Equal(t, "127.0.0.1:0", address)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not retry on a random port in time")
	}

	waitForPublishedServeManifest(t, vault.path, 46231)
	cancel()

	select {
	case err := <-done:
		finished = true
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop after cancel")
	}
	require.NoFileExists(t, appruntime.ManifestPath(vault.path))
}

func TestServeCommandReusesDiscoveryPortByDefault(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n",
	})
	require.NoError(t, appruntime.WriteLastPort(vault.path, 47411))

	ctx, cancel := context.WithCancel(context.Background())

	cmd := newServeCommand("serve", "test", false)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{
		"--vault", vault.name,
		"--host", "127.0.0.1",
		"--port", "0",
	})

	origListen := serveListen

	listener := newStubListener("127.0.0.1", 47411)
	addresses := make(chan string, 1)
	serveListen = func(network, address string) (net.Listener, error) {
		addresses <- address
		return listener, nil
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.ExecuteContext(ctx)
	}()
	finished := false
	cleanupServeCommand(t, cancel, done, &finished, origListen)

	select {
	case address := <-addresses:
		require.Equal(t, "127.0.0.1:47411", address)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not reuse discovery port by default")
	}

	waitForPublishedServeManifest(t, vault.path, 47411)
	cancel()

	select {
	case err := <-done:
		finished = true
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop after cancel")
	}
	require.NoFileExists(t, appruntime.ManifestPath(vault.path))
}

func waitForPublishedServeManifest(t *testing.T, vaultPath string, port int) {
	t.Helper()
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		manifest, err := appruntime.ReadManifest(vaultPath)
		require.NoError(collect, err, "read runtime manifest")
		assert.Equal(collect, os.Getpid(), manifest.PID, "manifest PID")
		assert.NotEmpty(collect, manifest.RunID, "manifest RunID")
		assert.Equal(collect, vaultPath, manifest.VaultPath, "manifest vault path")
		assert.Equal(collect, port, manifest.HTTPPort, "manifest HTTP port")
		assert.Equal(collect, fmt.Sprintf("http://127.0.0.1:%d", port), manifest.HTTPURL, "manifest HTTP URL")
	}, 10*time.Second, 25*time.Millisecond, "serve did not publish its runtime manifest")
}

func cleanupServeCommand(t *testing.T, cancel context.CancelFunc, done <-chan error, finished *bool, origListen func(string, string) (net.Listener, error)) {
	t.Helper()
	t.Cleanup(func() {
		if !*finished {
			select {
			case err := <-done:
				*finished = true
				t.Logf("serve exited before cleanup cancellation: %v", err)
			default:
				t.Log("serve still running before cleanup cancellation")
			}
		}
		cancel()
		if !*finished {
			select {
			case err := <-done:
				t.Logf("serve returned after cleanup cancellation: %v", err)
			case <-time.After(10 * time.Second):
				t.Error("serve did not stop during cleanup")
			}
		}
		serveListen = origListen
	})
}

func TestStartCommandOpensBrowserByDefault(t *testing.T) {
	testServeLikeCommand(t, "start", true)
}

func TestStartCommandOpenFlagCanBeDisabled(t *testing.T) {
	testServeLikeCommand(t, "start", false, "--open=false")
}

func testServeLikeCommand(t *testing.T, commandName string, expectOpen bool, extraArgs ...string) {
	t.Helper()

	vault := setupAgentTestVault(t, map[string]string{
		"Alpha.md": "# Alpha\n\nhello world\n",
	})

	home := t.TempDir()
	origHome := vaultconfig.UserHomeDirectory
	vaultconfig.UserHomeDirectory = func() (string, error) { return home, nil }
	defer func() { vaultconfig.UserHomeDirectory = origHome }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct {
		stdout string
		stderr string
		err    error
	}, 1)
	earlyDone := make(chan struct {
		stdout string
		stderr string
		err    error
	}, 1)

	origListen := serveListen
	origOpenBrowser := openBrowserCmd

	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	errR, errW, err := os.Pipe()
	require.NoError(t, err)

	listener := newStubListener("127.0.0.1", 43123)
	serveListen = func(network, address string) (net.Listener, error) {
		return listener, nil
	}

	var (
		openMu       sync.Mutex
		openedURLs   []string
		openBrowserC = make(chan struct{}, 2)
	)
	openBrowserCmd = func(url string) error {
		openMu.Lock()
		openedURLs = append(openedURLs, url)
		openMu.Unlock()
		select {
		case openBrowserC <- struct{}{}:
		default:
		}
		return nil
	}

	var (
		outBytes []byte
		errBytes []byte
		readWG   sync.WaitGroup
		readErrs = make(chan error, 2)
	)
	readPipe := func(dst *[]byte, src *os.File) {
		defer readWG.Done()
		data, readErr := io.ReadAll(src)
		if readErr != nil {
			readErrs <- readErr
			return
		}
		*dst = data
	}
	readWG.Add(2)
	go readPipe(&outBytes, outR)
	go readPipe(&errBytes, errR)

	cmd := newServeCommand(commandName, commandName, commandName == "start")
	cmd.SetOut(outW)
	cmd.SetErr(errW)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	args := []string{"--vault", vault.name, "--host", "127.0.0.1", "--port", "0"}
	args = append(args, extraArgs...)
	cmd.SetArgs(args)
	defer func() {
		serveListen = origListen
		openBrowserCmd = origOpenBrowser
	}()

	go func() {
		runErr := cmd.ExecuteContext(ctx)
		_ = outW.Close()
		_ = errW.Close()
		readWG.Wait()
		close(readErrs)
		done <- struct {
			stdout string
			stderr string
			err    error
		}{stdout: strings.TrimSpace(string(outBytes)), stderr: strings.TrimSpace(string(errBytes)), err: runErr}
	}()

	instancesDir := filepath.Join(home, ".config", "rhizome", "instances")
	registrationPath := filepath.Join(instancesDir, appruntime.InstanceID(vault.path)+".json")
	discoveryPath := appruntime.ManifestPath(vault.path)
	var manifest appruntime.InstanceManifest
	require.Eventually(t, func() bool {
		select {
		case result := <-done:
			earlyDone <- result
			return true
		default:
		}
		data, err := fileio.ReadFile(registrationPath)
		if err != nil {
			return false
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			return false
		}
		return manifest.HTTPPort > 0 && strings.HasPrefix(manifest.HTTPURL, "http://127.0.0.1:")
	}, 10*time.Second, 100*time.Millisecond)
	select {
	case result := <-earlyDone:
		t.Fatalf("%s exited before manifest registration: err=%v stderr=%q stdout=%q", commandName, result.err, result.stderr, result.stdout)
	default:
	}

	require.Eventually(t, func() bool {
		discovery, err := appruntime.ReadManifest(vault.path)
		if err != nil {
			return false
		}
		return discovery.HTTPURL == manifest.HTTPURL && discovery.VaultPath == vault.path
	}, 5*time.Second, 50*time.Millisecond)

	if expectOpen {
		require.Eventually(t, func() bool {
			select {
			case <-openBrowserC:
				return true
			default:
				return false
			}
		}, 3*time.Second, 25*time.Millisecond)
	} else {
		select {
		case <-openBrowserC:
			t.Fatalf("%s unexpectedly opened browser", commandName)
		case <-time.After(300 * time.Millisecond):
		}
	}

	cancel()

	var result struct {
		stdout string
		stderr string
		err    error
	}
	select {
	case result = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("serve command did not stop after cancel")
	}
	for readErr := range readErrs {
		require.NoError(t, readErr)
	}

	require.NoError(t, result.err)
	require.Empty(t, result.stdout)
	require.Contains(t, result.stderr, "Rhizome serve")
	require.Contains(t, result.stderr, manifest.HTTPURL)
	require.Contains(t, result.stderr, "Rhizome serve stopped")

	openMu.Lock()
	defer openMu.Unlock()
	if expectOpen {
		require.Contains(t, openedURLs, manifest.HTTPURL)
	} else {
		require.Empty(t, openedURLs)
	}

	require.NoFileExists(t, registrationPath)
	registry, err := appruntime.NewRegistry()
	require.NoError(t, err)
	intents, err := registry.ListIntents(context.Background())
	require.NoError(t, err)
	require.Empty(t, intents)
	_, err = os.Stat(discoveryPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

type stubListener struct {
	addr   net.Addr
	closed chan struct{}
	once   sync.Once
}

func newStubListener(host string, port int) *stubListener {
	return &stubListener{
		addr:   &net.TCPAddr{IP: net.ParseIP(host), Port: port},
		closed: make(chan struct{}),
	}
}

func (l *stubListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *stubListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *stubListener) Addr() net.Addr {
	return l.addr
}

func (l *stubListener) SetDeadline(time.Time) error {
	return errors.New("not supported")
}
