package httpapi

import (
	"context"
	"net"
	"net/http"
	"strings"
)

const clientIPKey ctxKey = iota + 100

// canonicalIP returns the canonical form of an IP address.
// If the IP is invalid, it returns the input unchanged.
// If the IP is IPv4-mapped IPv6, it returns the plain IPv4 form.
// Otherwise, it returns the lowercase canonical IPv6 form.
func canonicalIP(s string) string {
	ip := net.ParseIP(s)
	if ip == nil {
		return s
	}
	if ip.To4() != nil {
		return ip.To4().String()
	}
	return ip.String()
}

// rateLimitKey returns the key a per-IP rate limiter should use.
// Unparsable input is returned unchanged.
// An IPv4 address (including IPv4-mapped IPv6) returns its canonical dotted form.
// An IPv6 address returns the string form of its /64 network, because one IPv6 client
// normally controls a whole /64 and would otherwise get a fresh bucket per address.
func rateLimitKey(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip
	}
	// IPv4 or IPv4-mapped IPv6: return canonical IPv4
	if v4 := parsed.To4(); v4 != nil {
		return v4.String()
	}
	// IPv6: return /64 network
	return parsed.Mask(net.CIDRMask(64, 128)).String()
}

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
		// If SplitHostPort fails, the entire remoteAddr is treated as the host
		// (e.g., no port present)
		host = remoteAddr
	}
	// Only canonicalise if it's a valid IP; leave unparseable addresses as-is
	if net.ParseIP(host) != nil {
		host = canonicalIP(host)
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
	return canonicalIP(candidate), true
}
