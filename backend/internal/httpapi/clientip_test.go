package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		trusted    int
		remoteAddr string
		xff        []string
		want       string
	}{
		{"no proxy: remote address", 0, "203.0.113.7:5555", nil, "203.0.113.7"},
		{"no proxy: X-Forwarded-For is ignored (spoofable)", 0, "203.0.113.7:5555", []string{"1.2.3.4"}, "203.0.113.7"},
		{"one proxy: last entry is the client", 1, "10.0.0.1:80", []string{"198.51.100.9"}, "198.51.100.9"},
		{"one proxy: client-supplied prefix is ignored", 1, "10.0.0.1:80", []string{"6.6.6.6, 198.51.100.9"}, "198.51.100.9"},
		{"two proxies: second from the right", 2, "10.0.0.2:80", []string{"198.51.100.9, 10.0.0.1"}, "198.51.100.9"},
		{"header split across several lines", 2, "10.0.0.2:80", []string{"6.6.6.6", "198.51.100.9, 10.0.0.1"}, "198.51.100.9"},
		{"fewer entries than trusted proxies: fall back to the peer", 2, "10.0.0.2:80", []string{"198.51.100.9"}, "10.0.0.2"},
		{"missing header: fall back to the peer", 1, "10.0.0.1:80", nil, "10.0.0.1"},
		{"garbage entry: fall back to the peer", 1, "10.0.0.1:80", []string{"not-an-ip"}, "10.0.0.1"},
		{"ipv6 peer", 0, "[2001:db8::1]:4000", nil, "2001:db8::1"},
		{"ipv6 client behind a proxy", 1, "10.0.0.1:80", []string{"2001:db8::9"}, "2001:db8::9"},
		{"peer without a port", 0, "203.0.113.7", nil, "203.0.113.7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			h := clientIP(tt.trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = ClientIP(r.Context())
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for _, v := range tt.xff {
				req.Header.Add("X-Forwarded-For", v)
			}

			h.ServeHTTP(httptest.NewRecorder(), req)

			if got != tt.want {
				t.Errorf("ClientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClientIPIsEmptyWithoutTheMiddleware(t *testing.T) {
	if got := ClientIP(httptest.NewRequest(http.MethodGet, "/", nil).Context()); got != "" {
		t.Errorf("ClientIP = %q, want empty", got)
	}
}
