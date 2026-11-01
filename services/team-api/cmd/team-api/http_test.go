package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
)

func TestTrustedForwardedAddress(t *testing.T) {
	for _, tt := range []struct{ name, peer, header, want string }{
		{"ALB appends after spoofed prefix", "10.2.1.4:1234", "192.0.2.99, 203.0.113.7", "203.0.113.7:0"},
		{"direct caller cannot spoof", "203.0.113.8:1234", "192.0.2.99", "203.0.113.8:1234"},
		{"malformed last hop never trusts prefix", "10.2.1.4:1234", "192.0.2.99, invalid", "10.2.1.4:1234"},
		{"IPv6 normalized", "10.2.1.4:1234", "2001:db8::7", "[2001:db8::7]:0"},
		{"ALB port syntax", "10.2.1.4:1234", "[2001:db8::7]:4321", "[2001:db8::7]:0"},
		{"missing header", "10.2.1.4:1234", "", "10.2.1.4:1234"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/v1/auth/refresh", nil)
			r.RemoteAddr = tt.peer
			if tt.header != "" {
				r.Header.Set("X-Forwarded-For", tt.header)
			}
			h := trustedForwardedAddress([]string{"10.2.1.0/24"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.RemoteAddr != tt.want {
					t.Fatalf("address=%q, want %q", r.RemoteAddr, tt.want)
				}
			}))
			h.ServeHTTP(httptest.NewRecorder(), r)
		})
	}
}

func TestRequestDiagnosticsDoNotLogInput(t *testing.T) {
	var out bytes.Buffer
	rec := diagnostics.OpenStream(&out, diagnostics.Options{})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /things/{id}", func(w http.ResponseWriter, r *http.Request) {
		info := core.DiagnosticsFrom(r.Context())
		info.Sub = "subject-123"
		info.SessionID = "session-456"
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("sensitive response"))
	})
	h := requestDiagnostics(rec, trustedForwardedAddress([]string{"10.2.1.0/24"}, mux))
	r := httptest.NewRequest("POST", "/things/user@example.com?token=secret-query", strings.NewReader("secret-body"))
	r.RemoteAddr = "10.2.1.4:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	r.Header.Set("Authorization", "Bearer secret-token")
	r.Header.Set("User-Agent", "rzm/user@example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var ev diagnostics.Event
	if err := json.Unmarshal(out.Bytes(), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Attributes["route"] != "POST /things/{id}" || ev.Attributes["status"] != float64(403) || ev.Attributes["sub"] != "subject-123" || ev.Attributes["request_id"] == "" {
		t.Fatalf("missing request fields: %#v", ev.Attributes)
	}
	for _, secret := range []string{"user@example.com", "secret-query", "secret-body", "secret-token", "sensitive response"} {
		if strings.Contains(out.String(), secret) {
			t.Fatalf("log leaked %q", secret)
		}
	}
}

func TestDuplicateForwardedHeadersUsePeerBucket(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/auth/refresh", nil)
	r.RemoteAddr = "10.2.1.4:1234"
	r.Header.Add("X-Forwarded-For", "203.0.113.7")
	r.Header.Add("X-Forwarded-For", "192.0.2.99")
	h := trustedForwardedAddress([]string{"10.2.1.0/24"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr != "10.2.1.4:1234" {
			t.Fatal("ambiguous headers must not select a caller-controlled bucket")
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), r)
}

func TestAbortedRequestIsLoggedAndStillAborts(t *testing.T) {
	var out bytes.Buffer
	rec := diagnostics.OpenStream(&out, diagnostics.Options{})
	h := requestDiagnostics(rec, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		panic(http.ErrAbortHandler)
	}))
	func() {
		defer func() {
			if p := recover(); p != http.ErrAbortHandler {
				t.Fatalf("panic = %v, want http.ErrAbortHandler to propagate", p)
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/providers/voyage/v1/embeddings", nil))
	}()
	var ev diagnostics.Event
	if err := json.Unmarshal(out.Bytes(), &ev); err != nil {
		t.Fatalf("aborted request left no record: %v", err)
	}
	if ev.Attributes["aborted"] != true || ev.Attributes["status"] != float64(200) {
		t.Fatalf("aborted request fields: %#v", ev.Attributes)
	}
}
