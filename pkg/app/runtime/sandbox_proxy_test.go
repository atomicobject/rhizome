package runtime

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// sandboxDeniesLoopback makes direct dials fail the way a sandbox refuses
// them, while the proxy (any other address) stays reachable.
func sandboxDeniesLoopback(t *testing.T, proxyAddr string) {
	t.Helper()
	saved := directTransport
	directTransport = saved.Clone()
	dialer := &net.Dialer{}
	directTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != proxyAddr {
			return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.EPERM}
		}
		return dialer.DialContext(ctx, network, addr)
	}
	t.Cleanup(func() { directTransport = saved })
}

func TestRuntimeRequestsRetryThroughTheSandboxProxy(t *testing.T) {
	var seen string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = r.Method + " " + r.URL.String() + " " + string(body)
		_, _ = io.WriteString(w, "via proxy")
	}))
	defer proxy.Close()
	sandboxDeniesLoopback(t, proxy.Listener.Addr().String())
	client := &http.Client{Transport: loopbackTransport{}}

	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	_, err := client.Post("http://127.0.0.1:9/api", "application/json", strings.NewReader(`{"a":1}`))
	require.ErrorIs(t, err, syscall.EPERM, "without a sandbox proxy the denial stands")

	t.Setenv("HTTP_PROXY", proxy.URL)
	resp, err := client.Post("http://127.0.0.1:9/api", "application/json", strings.NewReader(`{"a":1}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, "via proxy", string(body))
	require.Equal(t, `POST http://127.0.0.1:9/api {"a":1}`, seen, "the retry resends the same request")
}

func TestRuntimeRequestsKeepTheDenialWhenTheProxyRefusesLoopback(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "host not allowed", http.StatusForbidden)
	}))
	defer proxy.Close()
	sandboxDeniesLoopback(t, proxy.Listener.Addr().String())
	t.Setenv("HTTP_PROXY", proxy.URL)
	_, err := (&http.Client{Transport: loopbackTransport{}}).Get("http://127.0.0.1:9/health")
	require.ErrorIs(t, err, syscall.EPERM, "a refusing proxy must not look like an unresponsive runtime")
}

func TestRuntimeRequestsRetryOnlyThroughALoopbackProxy(t *testing.T) {
	t.Setenv("http_proxy", "")
	for proxy, sandboxed := range map[string]bool{
		"http://127.0.0.1:53310":            true,
		"http://user:secret@localhost:8080": true,
		"http://[::1]:3128":                 true,
		"http://proxy.example.com:8080":     false, // a person's own proxy never gets the token
		"http://10.0.0.5:3128":              false,
		"":                                  false,
	} {
		t.Setenv("HTTP_PROXY", proxy)
		require.Equal(t, sandboxed, sandboxProxy() != nil, proxy)
	}
}

func TestRuntimeRequestsUseNoProxyWhenTheDirectConnectionWorks(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "direct")
	}))
	defer runtime.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	resp, err := (&http.Client{Transport: loopbackTransport{}}).Get(runtime.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, "direct", string(body))
}
