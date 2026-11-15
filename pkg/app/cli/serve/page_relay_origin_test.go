package serve

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPageRelayOriginReachesTheAddressTheServerListensOn(t *testing.T) {
	for _, tc := range []struct {
		ip   string
		want string
	}{
		{"0.0.0.0", "http://127.0.0.1:4100"},
		{"127.0.0.1", "http://127.0.0.1:4100"},
		{"::", "http://[::1]:4100"},
		{"::1", "http://[::1]:4100"},
		// A server bound to one LAN address does not listen on loopback.
		{"192.168.1.20", "http://192.168.1.20:4100"},
	} {
		addr := &net.TCPAddr{IP: net.ParseIP(tc.ip), Port: 4100}
		require.Equal(t, tc.want, pageRelayOrigin(addr), tc.ip)
	}
}
