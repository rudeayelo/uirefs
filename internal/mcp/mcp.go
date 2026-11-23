// Package mcp serves the MCP streamable-HTTP endpoint: JSON-RPC 2.0 over
// plain HTTP POST, implemented directly against the wire format (no SDK
// dependency; the official Go SDK would add a module tree for ~300 lines
// of dispatch that are all this server needs).
package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/uesteibar/uirefs/internal/store"
)

// ProtocolVersion is the MCP revision this server speaks.
const ProtocolVersion = "2025-06-18"

// Instructions is the short text carried by the initialize result.
const Instructions = "uirefs is a private library of UI references: components, sites, " +
	"animations, identities and resources, each with images, an inline HTML demo and observable behavior notes. " +
	"Search it with `search` before you design or build UI; use `get` to fetch everything needed to " +
	"implement and verify a match. Save references you find with `add` — call the `guide` tool before " +
	"your first `add`; it explains ingestion, the five types and the tag vocabulary. " +
	"Every component and animation should carry an interactive demo; the guide says how to get or reproduce it. " +
	"A resource carries a demo when it can show itself (an icon grid, a type specimen, an illustration gallery). " +
	"Queries, descriptions and behavior notes are in English."

type Server struct {
	store   *store.Store
	guide   string
	version string
	// fetch is used for url assets; overridable in tests.
	fetch func(ctx context.Context, url string) ([]byte, error)
}

func New(st *store.Store, guide, version string) *Server {
	return &Server{store: st, guide: guide, version: version, fetch: httpFetch}
}

// Register mounts the MCP endpoint on the mux.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /mcp", s.handle)
	mux.HandleFunc("GET /mcp", func(w http.ResponseWriter, _ *http.Request) {
		// Stateless single-response server: no SSE stream to open.
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
}

// ---- wire format ----

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// maxBody caps one JSON-RPC request, base64 assets included.
const maxBody = 256 << 20

// maxFetch caps one asset fetched from a URL.
const maxFetch = 256 << 20

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	// A web page the owner visits can send a cross-site POST to this port.
	// Browsers cannot send application/json cross-site without a preflight,
	// which this server never allows, and a browser always adds Origin.
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct != "application/json" {
		http.Error(w, "content-type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	if o := r.Header.Get("Origin"); o != "" {
		if u, err := url.Parse(o); err != nil || u.Host != r.Host {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPC(w, nil, rpcError{-32700, "parse error"})
		return
	}
	if strings.HasPrefix(req.Method, "notifications/") {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if len(req.ID) == 0 {
		// A request without id is malformed JSON-RPC, not a notification.
		writeRPC(w, nil, rpcError{-32600, "missing id"})
		return
	}

	result, rerr := s.dispatch(r, &req)
	if rerr != nil {
		writeRPC(w, req.ID, *rerr)
		return
	}
	writeRPC(w, req.ID, result)
}

func writeRPC(w http.ResponseWriter, id json.RawMessage, v any) {
	type envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id,omitempty"`
		Result  any             `json:"result,omitempty"`
		Error   *rpcError       `json:"error,omitempty"`
	}
	env := envelope{JSONRPC: "2.0", ID: id}
	if err, ok := v.(rpcError); ok {
		env.Error = &err
	} else {
		env.Result = v
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(env)
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) dispatch(r *http.Request, req *request) (any, *rpcError) {
	base := "http://" + r.Host
	switch req.Method {
	case "initialize":
		return s.initialize(req.Params)
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefs()}, nil
	case "tools/call":
		return s.callTool(r.Context(), base, req.Params)
	case "resources/list":
		return map[string]any{"resources": []map[string]any{{
			"uri":      "uirefs://guide",
			"name":     "uirefs usage guide",
			"mimeType": "text/markdown",
		}}}, nil
	case "resources/read":
		return s.readResource(req.Params)
	default:
		return nil, &rpcError{-32601, "unknown method " + req.Method}
	}
}

func (s *Server) initialize(params json.RawMessage) (any, *rpcError) {
	version := ProtocolVersion
	var in struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 {
		json.Unmarshal(params, &in)
	}
	if in.ProtocolVersion != "" {
		version = in.ProtocolVersion // echo: we accept any revision we are asked for
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities": map[string]any{
			"tools":     map[string]any{"listChanged": false},
			"resources": map[string]any{},
		},
		"serverInfo":   map[string]any{"name": "uirefs", "version": s.version},
		"instructions": Instructions,
	}, nil
}

func (s *Server) readResource(params json.RawMessage) (any, *rpcError) {
	var in struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &in); err != nil || in.URI != "uirefs://guide" {
		return nil, &rpcError{-32602, "unknown resource; only uirefs://guide exists"}
	}
	return map[string]any{"contents": []map[string]any{{
		"uri":      "uirefs://guide",
		"mimeType": "text/markdown",
		"text":     s.guide,
	}}}, nil
}

// ---- tool results ----

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type imageContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

type callResult struct {
	Content []any `json:"content"`
	IsError bool  `json:"isError,omitempty"`
}

func okResult(parts ...any) callResult { return callResult{Content: parts} }
func errResult(msg string) callResult {
	return callResult{Content: []any{textContent{Type: "text", Text: msg}}, IsError: true}
}

// ---- tools ----

func (s *Server) callTool(ctx context.Context, base string, params json.RawMessage) (any, *rpcError) {
	var in struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, &rpcError{-32602, "invalid tools/call params"}
	}

	var res callResult
	switch in.Name {
	case "search":
		res = s.toolSearch(ctx, base, in.Arguments)
	case "get":
		res = s.toolGet(ctx, base, in.Arguments)
	case "add":
		res = s.toolAdd(ctx, in.Arguments)
	case "update":
		res = s.toolUpdate(ctx, in.Arguments)
	case "delete":
		res = s.toolDelete(ctx, in.Arguments)
	case "list_tags":
		res = s.toolListTags(ctx, in.Arguments)
	case "guide":
		res = okResult(textContent{Type: "text", Text: s.guide})
	default:
		return nil, &rpcError{-32602, "unknown tool " + in.Name}
	}
	return res, nil
}

func decodeArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

// marshalJSON encodes without HTML escaping: asset HTML and source text must
// survive to the agent verbatim.
func marshalJSON(v any) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
	return b.String()
}

func refIDString(id int64) string   { return "r_" + strconv.FormatInt(id, 10) }
func assetIDString(id int64) string { return "a_" + strconv.FormatInt(id, 10) }

func parseRefID(raw string) (int64, error)   { return parseID(raw, "reference") }
func parseAssetID(raw string) (int64, error) { return parseID(raw, "asset") }

// parseID accepts both "r_12"/"a_12" and a bare number.
func parseID(raw, kind string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if i := strings.IndexByte(raw, '_'); i >= 0 {
		raw = raw[i+1:]
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, errors.New("bad " + kind + " id")
	}
	return n, nil
}

func sourceMap(r *store.Reference) map[string]any {
	if r.Source == nil {
		return nil
	}
	return map[string]any{
		"url":   r.Source.URL,
		"kind":  r.Source.Kind,
		"title": r.Source.Title,
	}
}

func primaryImage(r *store.Reference) *store.Asset {
	var pick *store.Asset
	for i := range r.Assets {
		a := &r.Assets[i]
		if a.Kind != store.KindImage {
			continue
		}
		if a.Role == "primary" {
			return a
		}
		if pick == nil {
			pick = a
		}
	}
	return pick
}

func (s *Server) toolSearch(ctx context.Context, base string, raw json.RawMessage) callResult {
	var in struct {
		Query string   `json:"query"`
		Type  string   `json:"type"`
		Tags  []string `json:"tags"`
		Limit int      `json:"limit"`
	}
	if err := decodeArgs(raw, &in); err != nil {
		return errResult(err.Error())
	}
	if strings.TrimSpace(in.Query) == "" {
		return errResult("query is required")
	}
	if in.Limit <= 0 {
		in.Limit = 10
	}
	if in.Limit > 50 {
		in.Limit = 50
	}
	hits, _, err := s.store.Search(ctx, store.SearchQuery{
		Q: in.Query, Type: store.Type(in.Type), Tags: in.Tags, Limit: in.Limit,
	})
	if err != nil {
		return errResult(err.Error())
	}

	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		item := map[string]any{
			"id":          refIDString(h.Ref.ID),
			"type":        string(h.Ref.Type),
			"title":       h.Ref.Title,
			"description": h.Ref.Description,
			"tags":        h.Ref.Tags,
			"source_url":  "",
		}
		if h.Ref.Source != nil {
			item["source_url"] = h.Ref.Source.URL
		}
		if a := primaryImage(h.Ref); a != nil {
			item["thumb"] = base + "/assets/" + assetIDString(a.ID)
		}
		out = append(out, item)
	}
	compact := marshalJSON(out)

	parts := []any{textContent{Type: "text", Text: string(compact)}}
	for _, h := range hits {
		if a := primaryImage(h.Ref); a != nil {
			if img, err := s.assetImage(ctx, *a); err == nil {
				parts = append(parts, img)
			}
		}
	}
	return callResult{Content: parts}
}

func (s *Server) assetImage(ctx context.Context, a store.Asset) (imageContent, error) {
	asset, file, err := s.store.OpenAsset(ctx, a.ID)
	if err != nil {
		return imageContent{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return imageContent{}, err
	}
	return imageContent{
		Type:     "image",
		Data:     base64.StdEncoding.EncodeToString(data),
		MimeType: asset.Mime,
	}, nil
}

func (s *Server) toolGet(ctx context.Context, base string, raw json.RawMessage) callResult {
	var in struct {
		ID string `json:"id"`
	}
	if err := decodeArgs(raw, &in); err != nil {
		return errResult(err.Error())
	}
	id, err := parseRefID(in.ID)
	if err != nil {
		return errResult(err.Error())
	}
	ref, err := s.store.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return errResult("no reference " + in.ID)
	}
	if err != nil {
		return errResult(err.Error())
	}

	out := map[string]any{
		"id":          refIDString(ref.ID),
		"type":        string(ref.Type),
		"title":       ref.Title,
		"description": ref.Description,
		"behavior":    ref.Behavior,
		"tags":        ref.Tags,
		"source":      sourceMap(ref),
		"source_url":  "",
		"locator":     ref.Locator,
		"created_at":  ref.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if ref.Source != nil {
		out["source_url"] = ref.Source.URL
	}
	assets := make([]map[string]any, 0, len(ref.Assets))
	parts := []any{}
	for _, a := range ref.Assets {
		url := base + "/assets/" + assetIDString(a.ID)
		if a.Kind == store.KindDemo {
			url = base + "/demo/" + assetIDString(a.ID)
			// The demo is the code of the reference; inline the first one.
			if _, ok := out["demo_url"]; !ok {
				if data, err := s.assetBytes(ctx, a); err == nil {
					out["demo_html"] = string(data)
				}
				out["demo_url"] = url
			}
		}
		if a.Kind == store.KindClip {
			if _, ok := out["clip_url"]; !ok {
				out["clip_url"] = url
			}
		}
		entry := map[string]any{
			"id": assetIDString(a.ID), "kind": string(a.Kind), "role": a.Role,
			"mime": a.Mime, "url": url,
		}
		if a.Viewport != "" {
			entry["viewport"] = a.Viewport
		}
		if a.Caption != "" {
			entry["caption"] = a.Caption
		}
		assets = append(assets, entry)
		if a.Kind == store.KindImage {
			if img, err := s.assetImage(ctx, a); err == nil {
				parts = append(parts, img)
			}
		}
	}
	out["assets"] = assets
	detail := marshalJSON(out)

	parts = append([]any{textContent{Type: "text", Text: string(detail)}}, parts...)
	return callResult{Content: parts}
}

func (s *Server) assetBytes(ctx context.Context, a store.Asset) ([]byte, error) {
	_, file, err := s.store.OpenAsset(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

type assetArgs struct {
	Kind     string `json:"kind"`
	Role     string `json:"role"`
	Viewport string `json:"viewport"`
	Caption  string `json:"caption"`
	Path     string `json:"path"`
	URL      string `json:"url"`
	Base64   string `json:"base64"`
}

// resolveAsset turns one add() asset argument into store input. The media
// type is checked in the store.
func (s *Server) resolveAsset(ctx context.Context, i int, a assetArgs) (store.AssetInput, error) {
	kind := store.AssetKind(a.Kind)
	switch kind {
	case store.KindImage, store.KindDemo, store.KindClip:
	default:
		return store.AssetInput{}, fmt.Errorf("asset %d: kind must be image, demo or clip", i)
	}

	sources := 0
	for _, ok := range []bool{a.Path != "", a.URL != "", a.Base64 != ""} {
		if ok {
			sources++
		}
	}
	if sources != 1 {
		return store.AssetInput{}, fmt.Errorf("asset %d: give exactly one of path, url or base64", i)
	}

	var data []byte
	var err error
	switch {
	case a.Base64 != "":
		b64 := a.Base64
		// Tolerate a data: prefix.
		if i := strings.IndexByte(b64, ','); i >= 0 && strings.HasPrefix(b64, "data:") {
			b64 = b64[i+1:]
		}
		data, err = base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return store.AssetInput{}, fmt.Errorf("asset %d: bad base64: %w", i, err)
		}
	case a.Path != "":
		data, err = os.ReadFile(a.Path)
		if err != nil {
			// Keep the OS error (it carries the path) out of the reply.
			return store.AssetInput{}, fmt.Errorf("asset %d: cannot read that file", i)
		}
	case a.URL != "":
		data, err = s.fetch(ctx, a.URL)
		if err != nil {
			return store.AssetInput{}, fmt.Errorf("asset %d: %w", i, err)
		}
	}
	return store.AssetInput{
		Kind: kind, Role: a.Role, Viewport: a.Viewport, Caption: a.Caption, Data: data,
	}, nil
}

var fetchClient = &http.Client{Timeout: 2 * time.Minute}

func httpFetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := fetchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFetch+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFetch {
		return nil, fmt.Errorf("%s: larger than %d MiB", url, maxFetch>>20)
	}
	return data, nil
}

func (s *Server) toolAdd(ctx context.Context, raw json.RawMessage) callResult {
	var in struct {
		Type        string      `json:"type"`
		Title       string      `json:"title"`
		Description string      `json:"description"`
		Behavior    string      `json:"behavior"`
		Tags        []string    `json:"tags"`
		SourceURL   string      `json:"source_url"`
		Locator     string      `json:"locator"`
		Assets      []assetArgs `json:"assets"`
	}
	if err := decodeArgs(raw, &in); err != nil {
		return errResult(err.Error())
	}
	assets := make([]store.AssetInput, 0, len(in.Assets))
	for i, a := range in.Assets {
		ai, err := s.resolveAsset(ctx, i, a)
		if err != nil {
			return errResult(err.Error())
		}
		assets = append(assets, ai)
	}

	ref, err := s.store.Add(ctx, store.NewReference{
		Type: store.Type(in.Type), Title: in.Title, Description: in.Description,
		Behavior: in.Behavior, Tags: in.Tags, SourceURL: in.SourceURL,
		Locator: in.Locator, Assets: assets,
	})
	if err != nil {
		return errResult(err.Error())
	}
	summary := fmt.Sprintf("added %s %q (%s)", refIDString(ref.ID), ref.Title, ref.Type)
	if demo, ok := ref.AssetOf(store.KindDemo); ok {
		summary += ", demo a_" + strconv.FormatInt(demo.ID, 10)
	}
	return okResult(textContent{Type: "text", Text: summary})
}

func (s *Server) toolUpdate(ctx context.Context, raw json.RawMessage) callResult {
	var in struct {
		ID           string      `json:"id"`
		Type         *string     `json:"type"`
		Title        *string     `json:"title"`
		Description  *string     `json:"description"`
		Behavior     *string     `json:"behavior"`
		Tags         *[]string   `json:"tags"`
		SourceURL    *string     `json:"source_url"`
		Locator      *string     `json:"locator"`
		Assets       []assetArgs `json:"assets"`
		RemoveAssets []string    `json:"remove_assets"`
	}
	if err := decodeArgs(raw, &in); err != nil {
		return errResult(err.Error())
	}
	id, err := parseRefID(in.ID)
	if err != nil {
		return errResult(err.Error())
	}
	var u store.Update
	if in.Type != nil {
		t := store.Type(*in.Type)
		u.Type = &t
	}
	u.Title, u.Description, u.Behavior = in.Title, in.Description, in.Behavior
	u.Tags, u.SourceURL, u.Locator = in.Tags, in.SourceURL, in.Locator
	for i, a := range in.Assets {
		ai, err := s.resolveAsset(ctx, i, a)
		if err != nil {
			return errResult(err.Error())
		}
		u.AddAssets = append(u.AddAssets, ai)
	}
	for i, raw := range in.RemoveAssets {
		aid, err := parseAssetID(raw)
		if err != nil {
			return errResult(fmt.Sprintf("remove_assets %d: %v", i, err))
		}
		u.RemoveAssets = append(u.RemoveAssets, aid)
	}

	ref, err := s.store.Update(ctx, id, u)
	if errors.Is(err, store.ErrNotFound) {
		return errResult("no reference " + in.ID)
	}
	if err != nil {
		return errResult(err.Error())
	}
	return okResult(textContent{Type: "text", Text: fmt.Sprintf("updated %s %q", refIDString(ref.ID), ref.Title)})
}

func (s *Server) toolDelete(ctx context.Context, raw json.RawMessage) callResult {
	var in struct {
		ID string `json:"id"`
	}
	if err := decodeArgs(raw, &in); err != nil {
		return errResult(err.Error())
	}
	id, err := parseRefID(in.ID)
	if err != nil {
		return errResult(err.Error())
	}
	switch err := s.store.Delete(ctx, id); {
	case errors.Is(err, store.ErrNotFound):
		return errResult("no reference " + in.ID)
	case err != nil:
		return errResult(err.Error())
	}
	return okResult(textContent{Type: "text", Text: "deleted " + in.ID})
}

func (s *Server) toolListTags(ctx context.Context, raw json.RawMessage) callResult {
	var in struct {
		Type string `json:"type"`
	}
	if err := decodeArgs(raw, &in); err != nil {
		return errResult(err.Error())
	}
	counts, err := s.store.ListTags(ctx, store.Type(in.Type))
	if err != nil {
		return errResult(err.Error())
	}
	type tag struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	tags := make([]tag, 0, len(counts))
	for _, c := range counts {
		tags = append(tags, tag{Name: c.Tag, Count: c.Count})
	}
	body := marshalJSON(tags)
	return okResult(textContent{Type: "text", Text: string(body)})
}

// ---- tool descriptors ----

func toolDefs() []map[string]any {
	schema := func(s string) json.RawMessage { return json.RawMessage(s) }
	return []map[string]any{
		{
			"name": "search",
			"description": "Search UI references by keyword (English). Ranks by FTS5 over title, " +
				"description, behavior and tags, fused with semantic similarity when embeddings are on. " +
				"Returns id, type, title, description, tags, source URL and inline thumbnails.",
			"inputSchema": schema(`{"type":"object","properties":{
				"query":{"type":"string","description":"English keywords, e.g. 'pricing annual toggle'"},
				"type":{"type":"string","enum":["component","site","animation","identity","resource"]},
				"tags":{"type":"array","items":{"type":"string"}},
				"limit":{"type":"integer","default":10,"maximum":50}
			},"required":["query"]}`),
		},
		{
			"name": "get",
			"description": "Fetch one reference by id: metadata, description, behavior, inline images, " +
				"demo_url with the demo HTML source, clip_url, source URL and locator.",
			"inputSchema": schema(`{"type":"object","properties":{
				"id":{"type":"string","description":"Reference id like r_12"}
			},"required":["id"]}`),
		},
		{
			"name": "add",
			"description": "Save a new reference. Every component and animation should carry an interactive " +
				"demo; the `guide` tool has the procedure to get or reproduce it. Assets come as a file path " +
				"on this host, an http(s) url, or base64. Call `guide` before your first add.",
			"inputSchema": schema(`{"type":"object","properties":{
				"type":{"type":"string","enum":["component","site","animation","identity","resource"]},
				"title":{"type":"string"},
				"description":{"type":"string","description":"What it is and why it is good, in English"},
				"behavior":{"type":"string","description":"Observable facts an implementer can check"},
				"tags":{"type":"array","items":{"type":"string"}},
				"source_url":{"type":"string"},
				"locator":{"type":"string","description":"Section of the page, or timestamp of the clip"},
				"assets":{"type":"array","items":{"type":"object","properties":{
					"kind":{"type":"string","enum":["image","demo","clip"]},
					"role":{"type":"string","description":"Free text, e.g. primary, detail, state or mobile; defaults to primary"},
					"viewport":{"type":"string"},
					"caption":{"type":"string"},
					"path":{"type":"string","description":"File path on this host"},
					"url":{"type":"string","description":"http(s) URL to fetch"},
					"base64":{"type":"string","description":"Inline base64, for small assets"}
				},"required":["kind"]}}
			},"required":["type","title","description"]}`),
		},
		{
			"name": "update",
			"description": "Change the fields of one reference (title, description, behavior, tags, type, " +
				"source_url, locator). `assets` appends assets (a demo is expected; `guide` has the procedure) " +
				"and `remove_assets` deletes asset ids from this reference. The whole call changes nothing if " +
				"an asset is rejected.",
			"inputSchema": schema(`{"type":"object","properties":{
				"id":{"type":"string"},
				"type":{"type":"string","enum":["component","site","animation","identity","resource"]},
				"title":{"type":"string"},
				"description":{"type":"string"},
				"behavior":{"type":"string"},
				"tags":{"type":"array","items":{"type":"string"}},
				"source_url":{"type":"string"},
				"locator":{"type":"string"},
				"assets":{"type":"array","items":{"type":"object","properties":{
					"kind":{"type":"string","enum":["image","demo","clip"]},
					"role":{"type":"string","description":"Free text, e.g. primary, detail, state or mobile; defaults to primary"},
					"viewport":{"type":"string"},
					"caption":{"type":"string"},
					"path":{"type":"string","description":"File path on this host"},
					"url":{"type":"string","description":"http(s) URL to fetch"},
					"base64":{"type":"string","description":"Inline base64, for small assets"}
				},"required":["kind"]}},
				"remove_assets":{"type":"array","items":{"type":"string","description":"Asset id like a_12"}}
			},"required":["id"]}`),
		},
		{
			"name": "delete",
			"description": "Delete one reference permanently: the row, the search entries and the " +
				"files. The source is removed when no reference uses it.",
			"inputSchema": schema(`{"type":"object","properties":{
				"id":{"type":"string"}
			},"required":["id"]}`),
		},
		{
			"name":        "list_tags",
			"description": "The tag vocabulary in use with counts, optionally for one type.",
			"inputSchema": schema(`{"type":"object","properties":{
				"type":{"type":"string","enum":["component","site","animation","identity","resource"]}
			}}`),
		},
		{
			"name": "guide",
			"description": "The full usage guide: when to search, when to save, how to ingest sources, " +
				"the five types, the tag vocabulary and the verification flow.",
			"inputSchema": schema(`{"type":"object","properties":{}}`),
		},
	}
}
