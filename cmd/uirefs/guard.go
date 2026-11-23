package main

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
)

// allowedHosts is the set of host names the service answers to. IP literals
// and localhost are always allowed: a web page cannot DNS-rebind them. Any
// other name must be listed, in UIREFS_PUBLIC_URL or UIREFS_ALLOWED_HOSTS
// (comma separated), so a hostile site that points its own name at this
// machine gets a 403 instead of the library.
type allowedHosts map[string]bool

func hostsFromEnv() allowedHosts {
	h := allowedHosts{}
	if v := strings.TrimSpace(os.Getenv("UIREFS_PUBLIC_URL")); v != "" {
		if u, err := url.Parse(v); err == nil && u.Hostname() != "" {
			h[strings.ToLower(u.Hostname())] = true
		}
	}
	for _, name := range strings.Split(os.Getenv("UIREFS_ALLOWED_HOSTS"), ",") {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			h[name] = true
		}
	}
	return h
}

func (a allowedHosts) ok(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || a[host] {
		return true
	}
	_, err := netip.ParseAddr(host)
	return err == nil
}

// guard rejects requests for an unknown Host and adds the frame header that
// keeps other sites from framing the UI. /demo is left alone: it sends its
// own sandbox policy.
func guard(next http.Handler, hosts allowedHosts) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts.ok(r.Host) {
			http.Error(w, "unknown host; set UIREFS_PUBLIC_URL or UIREFS_ALLOWED_HOSTS", http.StatusForbidden)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/demo/") {
			w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
		}
		next.ServeHTTP(w, r)
	})
}
