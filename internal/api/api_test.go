package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/uesteibar/uirefs/internal/store"
)

var png1x1 = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

var mp4Clip = append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, bytes.Repeat([]byte{7}, 32)...)

func testServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	mux := http.NewServeMux()
	New(st).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, st
}

func addRef(t *testing.T, st *store.Store, in store.NewReference) *store.Reference {
	t.Helper()
	r, err := st.Add(context.Background(), in)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	return r
}

func getJSON(t *testing.T, url string, status int) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != status {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s: status %d want %d: %s", url, resp.StatusCode, status, body)
	}
	var v map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return v
}

func TestListAndDetailContract(t *testing.T) {
	srv, st := testServer(t)

	r := addRef(t, st, store.NewReference{
		Type: store.TypeComponent, Title: "Pricing card", Description: "Annual toggle",
		Behavior: "peels from the top-left on click", Tags: []string{"pricing", "toggle"},
		SourceURL: "https://example.com/pricing", Locator: "#hero",
		Assets: []store.AssetInput{
			{Kind: store.KindImage, Role: "primary", Viewport: "1440x900", Data: png1x1},
			{Kind: store.KindDemo, Data: []byte("<!doctype html><p>demo</p>")},
		},
	})
	if r.ID != 1 {
		t.Fatalf("expected id 1, got %d", r.ID)
	}

	list := getJSON(t, srv.URL+"/api/references", 200)
	if list["total"] != float64(1) {
		t.Fatalf("total = %v", list["total"])
	}
	items := list["items"].([]any)
	item := items[0].(map[string]any)
	for _, k := range []string{"id", "type", "title", "description", "tags", "source", "thumb", "kinds", "created_at"} {
		if _, ok := item[k]; !ok {
			t.Errorf("list item missing %q", k)
		}
	}
	if item["id"] != "r_1" || item["type"] != "component" || item["title"] != "Pricing card" {
		t.Errorf("item = %v", item)
	}
	if item["thumb"] != "/assets/a_1" {
		t.Errorf("thumb = %v", item["thumb"])
	}
	if _, ok := item["behavior"]; ok {
		t.Error("list item must not carry behavior")
	}
	gotKinds := item["kinds"].([]any)
	if len(gotKinds) != 2 || gotKinds[0] != "image" || gotKinds[1] != "demo" {
		t.Errorf("kinds = %v", gotKinds)
	}
	src := item["source"].(map[string]any)
	if src["url"] != "https://example.com/pricing" || src["kind"] != "web" {
		t.Errorf("source = %v", src)
	}

	detail := getJSON(t, srv.URL+"/api/references/r_1", 200)
	if detail["behavior"] != "peels from the top-left on click" || detail["locator"] != "#hero" {
		t.Errorf("detail = %v", detail)
	}
	assets := detail["assets"].([]any)
	a0 := assets[0].(map[string]any)
	if a0["id"] != "a_1" || a0["mime"] != "image/png" || a0["url"] != "/assets/a_1" {
		t.Errorf("asset 0 = %v", a0)
	}
	if a0["viewport"] != "1440x900" || a0["caption"] != nil {
		t.Errorf("asset 0 viewport/caption = %v %v", a0["viewport"], a0["caption"])
	}
	a1 := assets[1].(map[string]any)
	if a1["kind"] != "demo" || a1["url"] != "/demo/a_2" {
		t.Errorf("asset 1 = %v", a1)
	}

	// Bare and prefixed ids both resolve; missing ones 404.
	getJSON(t, srv.URL+"/api/references/1", 200)
	getJSON(t, srv.URL+"/api/references/r_404", 404)
}

func TestListFiltersAndSearch(t *testing.T) {
	srv, st := testServer(t)
	addRef(t, st, store.NewReference{Type: store.TypeComponent, Title: "Button", Description: "d",
		Tags: []string{"button", "click"}, Assets: []store.AssetInput{{Kind: store.KindImage, Data: png1x1}}})
	addRef(t, st, store.NewReference{Type: store.TypeAnimation, Title: "Peel", Description: "d",
		Tags: []string{"click"}, Assets: []store.AssetInput{{Kind: store.KindDemo, Data: []byte("<p/>")}}})
	addRef(t, st, store.NewReference{Type: store.TypeComponent, Title: "Hero", Description: "d",
		Assets: []store.AssetInput{{Kind: store.KindImage, Data: png1x1}}})

	addRef(t, st, store.NewReference{Type: store.TypeResource, Title: "Icons", Description: "d"})
	v := getJSON(t, srv.URL+"/api/references?type=resource", 200)
	if v["total"] != float64(1) || v["items"].([]any)[0].(map[string]any)["type"] != "resource" {
		t.Errorf("resource filter = %v", v)
	}
	v = getJSON(t, srv.URL+"/api/references?type=component", 200)
	if v["total"] != float64(2) {
		t.Errorf("type filter total = %v", v["total"])
	}
	v = getJSON(t, srv.URL+"/api/references?tag=click&tag=button", 200)
	if v["total"] != float64(1) || v["items"].([]any)[0].(map[string]any)["title"] != "Button" {
		t.Errorf("tag filter = %v", v)
	}
	v = getJSON(t, srv.URL+"/api/references?q=peel", 200)
	if v["total"] != float64(1) || v["items"].([]any)[0].(map[string]any)["title"] != "Peel" {
		t.Errorf("search = %v", v)
	}
	v = getJSON(t, srv.URL+"/api/references?q=peel&type=component", 200)
	if v["total"] != float64(0) {
		t.Errorf("search+filter = %v", v)
	}
	getJSON(t, srv.URL+"/api/references?limit=0", 400)
	getJSON(t, srv.URL+"/api/references?type=nope", 400)
}

func TestDeleteOverAPI(t *testing.T) {
	srv, st := testServer(t)
	r := addRef(t, st, store.NewReference{Type: store.TypeComponent, Title: "T", Description: "d",
		Assets: []store.AssetInput{{Kind: store.KindImage, Data: png1x1}}})

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/references/r_"+strconv.FormatInt(r.ID, 10), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 204 {
		t.Fatalf("delete: %v %v", resp, err)
	}
	resp.Body.Close()

	resp, err = http.Get(srv.URL + "/api/references")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v map[string]any
	json.NewDecoder(resp.Body).Decode(&v)
	if v["total"] != float64(0) {
		t.Errorf("still listed: %v", v)
	}
	getJSON(t, srv.URL+"/api/references/r_1", 404)

	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/references/r_1", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("second delete = %d", resp.StatusCode)
	}
}

func TestTagsEndpoint(t *testing.T) {
	srv, st := testServer(t)
	addRef(t, st, store.NewReference{Type: store.TypeComponent, Title: "A", Description: "d",
		Tags: []string{"click"}, Assets: []store.AssetInput{{Kind: store.KindImage, Data: png1x1}}})
	addRef(t, st, store.NewReference{Type: store.TypeComponent, Title: "B", Description: "d",
		Tags: []string{"click", "button"}, Assets: []store.AssetInput{{Kind: store.KindImage, Data: png1x1}}})

	v := getJSON(t, srv.URL+"/api/tags", 200)
	tags := v["tags"].([]any)
	if len(tags) != 2 {
		t.Fatalf("tags = %v", tags)
	}
	first := tags[0].(map[string]any)
	if first["name"] != "click" || first["count"] != float64(2) {
		t.Errorf("most used first: %v", first)
	}
	v = getJSON(t, srv.URL+"/api/tags?type=animation", 200)
	if len(v["tags"].([]any)) != 0 {
		t.Errorf("typed tags = %v", v)
	}
}

func TestAssetServingAndRange(t *testing.T) {
	srv, st := testServer(t)
	r := addRef(t, st, store.NewReference{Type: store.TypeAnimation, Title: "T", Description: "d",
		Assets: []store.AssetInput{{Kind: store.KindClip, Data: mp4Clip}}})
	asset := r.Assets[0]
	url := srv.URL + "/assets/a_" + itoa(asset.ID)

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("asset: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !bytes.Equal(body, mp4Clip) {
		t.Error("asset body mismatch")
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Errorf("no Accept-Ranges on clips")
	}

	// Range request, as the detail view needs for clip scrubbing.
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Range", "bytes=0-7")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || len(body) != 8 {
		t.Fatalf("range: %d len=%d", resp.StatusCode, len(body))
	}
	if !bytes.HasPrefix(body, mp4Clip[:8]) {
		t.Error("range body mismatch")
	}

	resp, _ = http.Get(srv.URL + "/assets/a_999")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("missing asset = %d", resp.StatusCode)
	}
}

func TestDemoRouteCSPAndContent(t *testing.T) {
	srv, st := testServer(t)
	demoHTML := "<!doctype html><p>peel</p>"
	r := addRef(t, st, store.NewReference{Type: store.TypeAnimation, Title: "T", Description: "d",
		Assets: []store.AssetInput{{Kind: store.KindDemo, Data: []byte(demoHTML)}}})
	asset := r.Assets[0]

	// The exact header from the design: a sandbox without allow-same-origin.
	want := "sandbox allow-scripts allow-pointer-lock; default-src 'none'; " +
		"script-src 'unsafe-inline' 'unsafe-eval' https: blob:; style-src 'unsafe-inline' https:; " +
		"img-src data: blob: https:; font-src data: https:; media-src data: blob: https:; " +
		"connect-src https: data: blob:; worker-src blob:; form-action 'none'"
	resp, err := http.Get(srv.URL + "/demo/a_" + itoa(asset.ID))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	csp := resp.Header.Get("Content-Security-Policy")
	if csp != want {
		t.Fatalf("CSP = %q", csp)
	}
	if !strings.Contains(csp, "sandbox") || strings.Contains(csp, "allow-same-origin") {
		t.Errorf("sandbox must stay and allow-same-origin must not appear: %q", csp)
	}
	if resp.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("content type = %q", resp.Header.Get("Content-Type"))
	}
	if string(body) != demoHTML {
		t.Errorf("demo body = %q", body)
	}

	// A non-demo asset id does not serve a demo.
	img := addRef(t, st, store.NewReference{Type: store.TypeComponent, Title: "I", Description: "d",
		Assets: []store.AssetInput{{Kind: store.KindImage, Data: png1x1}}})
	resp, _ = http.Get(srv.URL + "/demo/a_" + itoa(img.Assets[0].ID))
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("image at /demo = %d", resp.StatusCode)
	}

	// Demo files are never served raw on the app origin: /assets redirects to
	// the sandboxed route.
	resp, _ = http.Get(srv.URL + "/assets/a_" + itoa(asset.ID))
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Request.URL.Path != "/demo/a_"+itoa(asset.ID) {
		t.Errorf("demo via /assets: %d %s", resp.StatusCode, resp.Request.URL.Path)
	}
}

func TestDeleteRemovesFilesFromDisk(t *testing.T) {
	_, st := testServer(t)
	r := addRef(t, st, store.NewReference{Type: store.TypeComponent, Title: "T", Description: "d",
		Assets: []store.AssetInput{{Kind: store.KindImage, Data: png1x1}}})

	assetPath := filepath.Join(st.Dir(), "assets", itoa(r.ID))
	if _, err := os.Stat(assetPath); err != nil {
		t.Fatalf("asset dir: %v", err)
	}
	if err := st.Delete(context.Background(), r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(assetPath); !os.IsNotExist(err) {
		t.Errorf("files still on disk")
	}
}

func TestErrorBody(t *testing.T) {
	srv, _ := testServer(t)
	resp, err := http.Get(srv.URL + "/api/references/r_1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("error content type = %q", ct)
	}
	var v map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	if _, ok := v["error"]; !ok {
		t.Errorf("error body = %v", v)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestRefLinkRedirect(t *testing.T) {
	srv, _ := testServer(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	for _, tc := range []struct{ path, want string }{
		{"/r/r_1", "/#/r/r_1"},
		{"/r/a b&c", "/#/r/a%20b&c"},
	} {
		resp, err := client.Get(srv.URL + strings.ReplaceAll(tc.path, " ", "%20"))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			t.Errorf("GET %s: status %d want %d", tc.path, resp.StatusCode, http.StatusFound)
		}
		if got := resp.Header.Get("Location"); got != tc.want {
			t.Errorf("GET %s: Location %q want %q", tc.path, got, tc.want)
		}
	}
}
