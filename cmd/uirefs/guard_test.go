package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGuardHostAndFrameHeader(t *testing.T) {
	h := guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }),
		allowedHosts{"uirefs.example.com": true})

	for host, want := range map[string]int{
		"127.0.0.1:4880":      200,
		"localhost:4880":      200,
		"[::1]:4880":          200,
		"100.64.0.7":          200,
		"uirefs.example.com":  200,
		"rebind.evil.example": 403,
		"evil.example:4880":   403,
	} {
		req := httptest.NewRequest("GET", "/api/references", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("host %q: got %d, want %d", host, rec.Code, want)
		}
	}

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Host = "127.0.0.1:4880"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	ui := get("/")
	if ui.Header().Get("Content-Security-Policy") != "frame-ancestors 'self'" {
		t.Error("UI must not be frameable by other sites")
	}
	demo := get("/demo/a_1")
	if demo.Header().Get("Content-Security-Policy") != "" {
		t.Error("/demo keeps its own policy")
	}
}
