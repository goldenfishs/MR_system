package main

import (
	"net/http/httptest"
	"testing"
)

func TestRateKeyTrustedProxy(t *testing.T) {
	for _, tc := range []struct{ name, trusted, peer, real, want string }{
		{"default rejects forwarded", "", "172.18.0.1:80", "198.51.100.2", "172.18.0.1|user"},
		{"untrusted peer", "172.18.0.1", "198.51.100.3:80", "198.51.100.2", "198.51.100.3|user"},
		{"trusted client one", "172.18.0.1", "172.18.0.1:80", "198.51.100.2", "198.51.100.2|user"},
		{"trusted client two", "172.18.0.1", "172.18.0.1:80", "198.51.100.3", "198.51.100.3|user"},
		{"invalid forwarded", "172.18.0.1", "172.18.0.1:80", "198.51.100.2, 1.2.3.4", "172.18.0.1|user"},
		{"ipv6", "127.0.0.1, ::1", "[::1]:80", "2001:db8::1", "2001:db8::1|user"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{config: Config{TrustedProxyIPs: tc.trusted}}
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Real-IP", tc.real)
			r.Header.Set("X-Forwarded-For", "1.2.3.4")
			if got := a.rateKey(r, "user"); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
