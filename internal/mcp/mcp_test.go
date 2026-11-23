package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

const testGuide = "# uirefs guide\n\nSearch, get, add, verify.\n"

func testServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	st, err := store.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	mux := http.NewServeMux()
	s := New(st, testGuide, "test")
	s.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, s
}

// call sends one JSON-RPC request and returns the decoded result or error.
func call(t *testing.T, srv *httptest.Server, method string, params any) (map[string]any, map[string]any) {
	t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		body["params"] = params
	}
	raw, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/mcp", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type = %q", ct)
	}
	var env struct {
		Result map[string]any  `json:"result"`
		Error  *map[string]any `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if env.Error != nil {
		return nil, *env.Error
	}
	return env.Result, nil
}

func toolCall(t *testing.T, srv *httptest.Server, name string, args any) (string, bool, []map[string]any) {
	t.Helper()
	res, rerr := call(t, srv, "tools/call", map[string]any{"name": name, "arguments": args})
	if rerr != nil {
		t.Fatalf("tools/call %s: %v", name, rerr)
	}
	isErr, _ := res["isError"].(bool)
	content, _ := res["content"].([]any)
	var blocks []map[string]any
	var text string
	for _, c := range content {
		b := c.(map[string]any)
		if b["type"] == "text" {
			text, _ = b["text"].(string)
		} else {
			blocks = append(blocks, b)
		}
	}
	return text, isErr, blocks
}

func TestInitialize(t *testing.T) {
	srv, _ := testServer(t)
	res, rerr := call(t, srv, "initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "test", "version": "0"},
	})
	if rerr != nil {
		t.Fatal(rerr)
	}
	if res["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v", res["protocolVersion"])
	}
	ins, _ := res["instructions"].(string)
	if !strings.Contains(ins, "guide") || !strings.Contains(ins, "add") {
		t.Errorf("instructions = %q", ins)
	}
	if !strings.Contains(ins, "interactive demo") {
		t.Errorf("instructions do not state the demo rule: %q", ins)
	}
	info := res["serverInfo"].(map[string]any)
	if info["name"] != "uirefs" {
		t.Errorf("serverInfo = %v", info)
	}
	if _, ok := res["capabilities"].(map[string]any)["tools"]; !ok {
		t.Error("no tools capability")
	}
}

func TestToolsList(t *testing.T) {
	srv, _ := testServer(t)
	res, rerr := call(t, srv, "tools/list", nil)
	if rerr != nil {
		t.Fatal(rerr)
	}
	tools := res["tools"].([]any)
	if len(tools) != 7 {
		t.Fatalf("tools = %d", len(tools))
	}
	want := map[string]bool{}
	for _, tl := range tools {
		tool := tl.(map[string]any)
		want[tool["name"].(string)] = true
		if _, ok := tool["inputSchema"]; !ok {
			t.Errorf("tool %v has no inputSchema", tool["name"])
		}
	}
	for _, n := range []string{"search", "get", "add", "update", "delete", "list_tags", "guide"} {
		if !want[n] {
			t.Errorf("missing tool %q", n)
		}
	}
	// add and update repeat the demo rule and point at guide.
	byName := map[string]map[string]any{}
	for _, tl := range tools {
		tool := tl.(map[string]any)
		byName[tool["name"].(string)] = tool
	}
	for _, name := range []string{"add", "update"} {
		desc := byName[name]["description"].(string)
		if !strings.Contains(desc, "demo") || !strings.Contains(desc, "guide") {
			t.Errorf("%s description does not state the demo rule and point at guide: %q", name, desc)
		}
	}
}

func TestGuideToolAndResource(t *testing.T) {
	srv, _ := testServer(t)
	text, isErr, _ := toolCall(t, srv, "guide", map[string]any{})
	if isErr || text != testGuide {
		t.Fatalf("guide tool: isErr=%v text=%q", isErr, text)
	}
	res, rerr := call(t, srv, "resources/list", nil)
	if rerr != nil {
		t.Fatal(rerr)
	}
	resources := res["resources"].([]any)
	if len(resources) != 1 || resources[0].(map[string]any)["uri"] != "uirefs://guide" {
		t.Fatalf("resources = %v", resources)
	}
	res, rerr = call(t, srv, "resources/read", map[string]any{"uri": "uirefs://guide"})
	if rerr != nil {
		t.Fatal(rerr)
	}
	contents := res["contents"].([]any)
	text, _ = contents[0].(map[string]any)["text"].(string)
	if text != testGuide {
		t.Errorf("resource text = %q", text)
	}
}

func TestAddSearchGetUpdateDeleteFlow(t *testing.T) {
	srv, _ := testServer(t)

	// add with a base64 image and a base64 demo.
	added, isErr, _ := toolCall(t, srv, "add", map[string]any{
		"type":        "component",
		"title":       "Pricing card",
		"description": "Annual billing toggle done right",
		"behavior":    "the toggle slides in about 300 ms",
		"tags":        []string{"pricing", "toggle"},
		"source_url":  "https://example.com/pricing",
		"assets": []map[string]any{
			{"kind": "image", "role": "primary", "viewport": "1440x900",
				"base64": base64.StdEncoding.EncodeToString(png1x1)},
			{"kind": "demo", "base64": base64.StdEncoding.EncodeToString([]byte("<p>demo</p>"))},
		},
	})
	if isErr || !strings.Contains(added, "r_1") {
		t.Fatalf("add: isErr=%v text=%q", isErr, added)
	}

	// search finds it, with an inline thumbnail.
	text, isErr, blocks := toolCall(t, srv, "search", map[string]any{"query": "annual pricing toggle"})
	if isErr {
		t.Fatalf("search: %s", text)
	}
	if !strings.Contains(text, `"title":"Pricing card"`) || !strings.Contains(text, `"thumb"`) {
		t.Fatalf("search text = %q", text)
	}
	if len(blocks) != 1 || blocks[0]["type"] != "image" {
		t.Fatalf("no inline thumbnail: %+v", blocks)
	}
	if blocks[0]["mimeType"] != "image/png" {
		t.Errorf("thumb mime = %v", blocks[0]["mimeType"])
	}
	if data, _ := blocks[0]["data"].(string); data == "" {
		t.Error("empty thumbnail data")
	}

	// get returns behavior, demo_url, demo_html and inline images.
	text, isErr, blocks = toolCall(t, srv, "get", map[string]any{"id": "r_1"})
	if isErr {
		t.Fatalf("get: %s", text)
	}
	for _, want := range []string{`"behavior":"the toggle slides in about 300 ms"`,
		`"demo_url"`, `"demo_html":"<p>demo</p>"`, `"clip"`, `"source_url"`} {
		if want == `"clip"` {
			if strings.Contains(text, "clip_url") {
				t.Error("clip_url on a reference without a clip")
			}
			continue
		}
		if !strings.Contains(text, want) {
			t.Errorf("get text missing %s", want)
		}
	}
	if len(blocks) != 1 || blocks[0]["type"] != "image" {
		t.Errorf("get images: %+v", blocks)
	}

	// update accepts any of the five types and rejects anything else.
	bare, isErr, _ := toolCall(t, srv, "add", map[string]any{
		"type": "component", "title": "Bare", "description": "d",
		"assets": []map[string]any{
			{"kind": "image", "base64": base64.StdEncoding.EncodeToString(png1x1)},
		},
	})
	if isErr {
		t.Fatalf("add bare: %s", bare)
	}
	_, isErr, _ = toolCall(t, srv, "update", map[string]any{"id": "r_2", "type": "widget"})
	if !isErr {
		t.Fatal("update to an unknown type must fail")
	}
	if _, isErr, _ = toolCall(t, srv, "update", map[string]any{"id": "r_2", "type": "animation"}); isErr {
		t.Fatal("update to animation must succeed")
	}

	updated, isErr, _ := toolCall(t, srv, "update", map[string]any{"id": "r_1", "behavior": "slides in 200 ms"})
	if isErr || !strings.Contains(updated, "updated r_1") {
		t.Fatalf("update: isErr=%v %q", isErr, updated)
	}
	text, isErr, _ = toolCall(t, srv, "get", map[string]any{"id": "r_1"})
	if isErr || !strings.Contains(text, "slides in 200 ms") {
		t.Errorf("behavior not updated: %q", text)
	}

	// list_tags.
	text, isErr, _ = toolCall(t, srv, "list_tags", map[string]any{"type": "component"})
	if isErr || !strings.Contains(text, `"name":"pricing"`) {
		t.Fatalf("list_tags: %q", text)
	}

	// A resource needs no image, demo or source_url, and list_tags accepts its type.
	if text, isErr, _ = toolCall(t, srv, "add", map[string]any{
		"type": "resource", "title": "Isometric set", "description": "animated illustration library",
	}); isErr {
		t.Fatalf("add resource: %s", text)
	}
	if text, isErr, _ = toolCall(t, srv, "list_tags", map[string]any{"type": "resource"}); isErr {
		t.Fatalf("list_tags resource: %q", text)
	}

	// delete removes it everywhere.
	deleted, isErr, _ := toolCall(t, srv, "delete", map[string]any{"id": "r_1"})
	if isErr || !strings.Contains(deleted, "deleted") {
		t.Fatalf("delete: isErr=%v %q", isErr, deleted)
	}
	text, isErr, _ = toolCall(t, srv, "get", map[string]any{"id": "r_1"})
	if !isErr {
		t.Fatalf("get after delete: %q", text)
	}
	text, isErr, _ = toolCall(t, srv, "search", map[string]any{"query": "pricing toggle"})
	if isErr || !strings.Contains(text, "[]") {
		t.Fatalf("search after delete: %q", text)
	}
}

func TestAddAssetSources(t *testing.T) {
	srv, s := testServer(t)

	// A file on this host.
	dir := t.TempDir()
	clipPath := filepath.Join(dir, "clip.mp4")
	clip := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, make([]byte, 16)...)
	if err := os.WriteFile(clipPath, clip, 0o644); err != nil {
		t.Fatal(err)
	}
	added, isErr, _ := toolCall(t, srv, "add", map[string]any{
		"type": "animation", "title": "Peel", "description": "d",
		"assets": []map[string]any{{"kind": "clip", "path": clipPath}},
	})
	if isErr {
		t.Fatalf("add with path: %s", added)
	}

	// A URL: point the server's fetch at a local file server instead of the
	// network (same code path as httpFetch).
	s.fetch = func(ctx context.Context, url string) ([]byte, error) {
		if !strings.HasPrefix(url, "http://127.0.0.1") {
			return nil, fmt.Errorf("test only fetches local urls")
		}
		return os.ReadFile(clipPath)
	}
	added, isErr, _ = toolCall(t, srv, "add", map[string]any{
		"type": "animation", "title": "Peel2", "description": "d",
		"assets": []map[string]any{{"kind": "clip", "url": srv.URL + "/clip.mp4"}},
	})
	if isErr {
		t.Fatalf("add with url: %s", added)
	}

	// Files of any size are accepted.
	bigPath := filepath.Join(dir, "big.png")
	big := append(append([]byte{}, png1x1...), make([]byte, 6<<20)...)
	if err := os.WriteFile(bigPath, big, 0o644); err != nil {
		t.Fatal(err)
	}
	added, isErr, _ = toolCall(t, srv, "add", map[string]any{
		"type": "identity", "title": "Big", "description": "d",
		"assets": []map[string]any{{"kind": "image", "path": bigPath}},
	})
	if isErr {
		t.Fatalf("add 6 MB file: %s", added)
	}

	// An animation with only an image is accepted too.
	added, isErr, _ = toolCall(t, srv, "add", map[string]any{
		"type": "animation", "title": "Still", "description": "d",
		"assets": []map[string]any{{"kind": "image", "base64": base64.StdEncoding.EncodeToString(png1x1)}},
	})
	if isErr {
		t.Fatalf("animation with only an image: %s", added)
	}

	// An asset with two sources is rejected.
	msg, isErr, _ := toolCall(t, srv, "add", map[string]any{
		"type": "component", "title": "Bad2", "description": "d",
		"assets": []map[string]any{{"kind": "image", "path": clipPath, "base64": "AAAA"}},
	})
	if !isErr || !strings.Contains(msg, "exactly one of path, url or base64") {
		t.Fatalf("two sources: isErr=%v %q", isErr, msg)
	}
}

func TestUpdateAttachesAndRemovesAssets(t *testing.T) {
	srv, _ := testServer(t)
	clip := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, make([]byte, 16)...)

	added, isErr, _ := toolCall(t, srv, "add", map[string]any{
		"type": "animation", "title": "Peel", "description": "d",
		"assets": []map[string]any{{"kind": "clip", "base64": base64.StdEncoding.EncodeToString(clip)}},
	})
	if isErr {
		t.Fatalf("add: %s", added)
	}

	// update attaches a demo to the reference that already exists.
	msg, isErr, _ := toolCall(t, srv, "update", map[string]any{"id": "r_1", "assets": []map[string]any{
		{"kind": "demo", "base64": base64.StdEncoding.EncodeToString([]byte("<p>v1</p>"))},
	}})
	if isErr || !strings.Contains(msg, "updated r_1") {
		t.Fatalf("update with demo: isErr=%v %q", isErr, msg)
	}
	got := refDetail(t, srv, "r_1")
	if got["demo_html"] != "<p>v1</p>" || got["demo_url"] == "" {
		t.Fatalf("demo after update: %v %v", got["demo_html"], got["demo_url"])
	}
	if assetIDOf(t, got, "clip") == "" {
		t.Fatal("clip lost when the demo was attached")
	}

	// A second demo is appended. get shows the first one; the assets list
	// carries both.
	_, isErr, _ = toolCall(t, srv, "update", map[string]any{"id": "r_1", "assets": []map[string]any{
		{"kind": "demo", "base64": base64.StdEncoding.EncodeToString([]byte("<p>v2</p>"))},
	}})
	if isErr {
		t.Fatal("adding a second demo was rejected")
	}
	got = refDetail(t, srv, "r_1")
	if got["demo_html"] != "<p>v1</p>" {
		t.Errorf("demo_html = %v, want the first demo", got["demo_html"])
	}
	var demoIDs []string
	for _, a := range got["assets"].([]any) {
		m := a.(map[string]any)
		if m["kind"] == "demo" {
			demoIDs = append(demoIDs, m["id"].(string))
		}
	}
	if len(demoIDs) != 2 {
		t.Fatalf("demo assets = %d", len(demoIDs))
	}

	// remove_assets drops the clip while the demos remain.
	clipID := assetIDOf(t, got, "clip")
	if clipID == "" {
		t.Fatal("no clip id")
	}
	_, isErr, _ = toolCall(t, srv, "update", map[string]any{"id": "r_1", "remove_assets": []string{clipID}})
	if isErr {
		t.Fatal("removing the clip was rejected")
	}
	got = refDetail(t, srv, "r_1")
	if assetIDOf(t, got, "clip") != "" {
		t.Error("clip still listed")
	}

	// An animation may lose its last media.
	_, isErr, _ = toolCall(t, srv, "update", map[string]any{"id": "r_1", "remove_assets": demoIDs})
	if isErr {
		t.Fatal("removing the last demos was rejected")
	}
	got = refDetail(t, srv, "r_1")
	if n := len(got["assets"].([]any)); n != 0 {
		t.Errorf("assets left = %d", n)
	}

	// An asset id that is not on the reference is rejected.
	bare, isErr, _ := toolCall(t, srv, "add", map[string]any{
		"type": "component", "title": "Bare", "description": "d",
		"assets": []map[string]any{{"kind": "image", "base64": base64.StdEncoding.EncodeToString(png1x1)}},
	})
	if isErr {
		t.Fatalf("add bare: %s", bare)
	}
	msg, isErr, _ = toolCall(t, srv, "update", map[string]any{"id": "r_2", "remove_assets": []string{"a_999"}})
	if !isErr || !strings.Contains(msg, "not on this reference") {
		t.Fatalf("foreign asset id: isErr=%v %q", isErr, msg)
	}
}

func refDetail(t *testing.T, srv *httptest.Server, id string) map[string]any {
	t.Helper()
	text, isErr, _ := toolCall(t, srv, "get", map[string]any{"id": id})
	if isErr {
		t.Fatalf("get %s: %s", id, text)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("get %s json: %v", id, err)
	}
	return out
}

func assetIDOf(t *testing.T, got map[string]any, kind string) string {
	t.Helper()
	assets, _ := got["assets"].([]any)
	for _, a := range assets {
		m, _ := a.(map[string]any)
		if m["kind"] == kind {
			id, _ := m["id"].(string)
			return id
		}
	}
	return ""
}

func TestNotificationsAndUnknowns(t *testing.T) {
	srv, _ := testServer(t)

	body := map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}
	raw, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/mcp", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("notification status = %d", resp.StatusCode)
	}

	_, rerr := call(t, srv, "no/such/method", nil)
	if rerr == nil || rerr["code"] != float64(-32601) {
		t.Errorf("unknown method: %v", rerr)
	}
	_, rerr = call(t, srv, "tools/call", map[string]any{"name": "nope"})
	if rerr == nil || rerr["code"] != float64(-32602) {
		t.Errorf("unknown tool: %v", rerr)
	}

	resp, err = http.Get(srv.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /mcp = %d", resp.StatusCode)
	}
}

func TestBrowserCrossSiteRequestsAreRefused(t *testing.T) {
	srv, _ := testServer(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_tags"}}`
	post := func(contentType, origin string) int {
		req, _ := http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := post("text/plain", "http://evil.example"); got != http.StatusUnsupportedMediaType {
		t.Errorf("simple cross-site POST: got %d, want 415", got)
	}
	if got := post("application/json", "http://evil.example"); got != http.StatusForbidden {
		t.Errorf("cross-origin JSON POST: got %d, want 403", got)
	}
	if got := post("application/json; charset=utf-8", srv.URL); got != http.StatusOK {
		t.Errorf("same-origin POST: got %d, want 200", got)
	}
	if got := post("application/json", ""); got != http.StatusOK {
		t.Errorf("agent POST without Origin: got %d, want 200", got)
	}
}

func TestPathAssetErrorHidesThePath(t *testing.T) {
	srv, _ := testServer(t)
	text, isErr, _ := toolCall(t, srv, "add", map[string]any{
		"type": "component", "title": "t", "description": "d", "behavior": "b",
		"assets": []map[string]any{{"kind": "image", "path": "/nonexistent/dir/secret.png"}},
	})
	if !isErr || strings.Contains(text, "/nonexistent") {
		t.Fatalf("isError=%v text=%q", isErr, text)
	}
}
