package httpapi

import (
	"context"
	"net"
	"net/http"
	"strings"
)

const clientIPKey ctxKey = iota + 100

// ClientIP returns the client address determined by the clientIP middleware,
// or "" when the middleware did not run.
func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey).(string)
	return ip
}

// clientIP stores the client's IP address in the request context.
//
// trustedProxies is the number of reverse proxies in front of the API that
// each append the address of their peer to X-Forwarded-For. With 0 the header
// is ignored, because any client can forge it. With N the client is the Nth
// entry counted from the right: entries further left were supplied by the
// client or an untrusted hop, so they are never used.
func clientIP(trustedProxies int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := peerIP(r.RemoteAddr)
			if trustedProxies > 0 {
				if forwarded, ok := forwardedClient(r.Header.Values("X-Forwarded-For"), trustedProxies); ok {
					ip = forwarded
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey, ip)))
		})
	}
}

func peerIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func forwardedClient(headers []string, trusted int) (string, bool) {
	var entries []string
	for _, h := range headers {
		for _, e := range strings.Split(h, ",") {
			entries = append(entries, strings.TrimSpace(e))
		}
	}
	if len(entries) < trusted {
		return "", false
	}
	candidate := entries[len(entries)-trusted]
	if net.ParseIP(candidate) == nil {
		return "", false
	}
	return candidate, true
}
