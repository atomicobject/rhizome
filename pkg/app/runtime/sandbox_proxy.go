package runtime

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
)

// loopbackTransport connects to the runtime directly and, when a sandbox
// refuses that connection, once more through the sandbox's HTTP proxy.
// Codex's loopback-only permission profile admits 127.0.0.1 only through its
// proxy, and Go never sends loopback requests to a proxy on its own. The
// retry happens only after a permission denial and only through a loopback
// proxy, so a remote proxy never sees runtime traffic. Both hosts replace
// HTTP_PROXY with their own sandbox proxy; a person's own loopback proxy is
// reached only if their sandbox admits it while refusing the runtime.
type loopbackTransport struct{}

var directTransport = http.DefaultTransport.(*http.Transport).Clone()

func (loopbackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, denied := directTransport.RoundTrip(req)
	if denied == nil || !errors.Is(denied, os.ErrPermission) {
		return resp, denied
	}
	proxy := sandboxProxy()
	if proxy == nil || (req.Body != nil && req.GetBody == nil) {
		return nil, denied
	}
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, denied
		}
		retry.Body = body
	}
	// ponytail: a transport per retry without keep-alive; share one per proxy
	// URL if sandboxed calls become frequent enough for connection setup to show.
	via := directTransport.Clone()
	via.Proxy = http.ProxyURL(proxy)
	via.DisableKeepAlives = true
	proxied, proxyErr := via.RoundTrip(retry)
	// A proxy that does not admit loopback (Claude Code's, unless the host
	// is allowed) refuses with 403 or 407. The runtime itself answers 403
	// only to a non-loopback Host, which this client never sends. Either
	// way the sandbox's denial is the answer, not an unresponsive runtime.
	if proxyErr != nil {
		return nil, denied
	}
	if proxied.StatusCode == http.StatusForbidden || proxied.StatusCode == http.StatusProxyAuthRequired {
		proxied.Body.Close()
		return nil, denied
	}
	return proxied, nil
}

// sandboxProxy is the loopback HTTP proxy the environment names, or nil.
// Sandbox proxies (Codex's, Claude Code's) listen on loopback; a remote
// proxy is a person's own, which must never receive the runtime's token.
func sandboxProxy() *url.URL {
	for _, name := range []string{"HTTP_PROXY", "http_proxy"} {
		if value := os.Getenv(name); value != "" {
			u, err := url.Parse(value)
			if err != nil || u.Host == "" {
				continue
			}
			if ip := net.ParseIP(u.Hostname()); u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()) {
				return u
			}
		}
	}
	return nil
}
