// Package api serves the JSON API for the web UI (docs/api.md), the asset
// files and the sandboxed demo route.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/uesteibar/uirefs/internal/store"
)

// DemoCSP is the exact Content-Security-Policy of the /demo route (design
// section 5). The sandbox without allow-same-origin is the security boundary:
// the demo gets an opaque origin and cannot read or call the app. The https
// sources keep real demos working.
const DemoCSP = "sandbox allow-scripts allow-pointer-lock; default-src 'none'; " +
	"script-src 'unsafe-inline' 'unsafe-eval' https: blob:; style-src 'unsafe-inline' https:; " +
	"img-src data: blob: https:; font-src data: https:; media-src data: blob: https:; " +
	"connect-src https: data: blob:; worker-src blob:; form-action 'none'"

type Server struct {
	store *store.Store
}

func New(s *store.Store) *Server { return &Server{store: s} }

// Register mounts all JSON API routes on the mux.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/references", s.handleList)
	mux.HandleFunc("GET /api/references/{id}", s.handleGet)
	mux.HandleFunc("DELETE /api/references/{id}", s.handleDelete)
	mux.HandleFunc("GET /api/tags", s.handleTags)
	mux.HandleFunc("GET /assets/{id}", s.handleAsset)
	mux.HandleFunc("GET /demo/{id}", s.handleDemo)
	mux.HandleFunc("GET /r/{id}", s.handleRefLink)
}

// handleRefLink answers the share link: /r/<id> redirects to the hash route the
// web UI already understands. The id is not looked up; an unknown one lands on
// the UI's "Not found" panel.
func (s *Server) handleRefLink(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/#/r/"+url.PathEscape(r.PathValue("id")), http.StatusFound)
}

// ---- JSON shapes (docs/api.md) ----

type sourceJSON struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
}

type assetJSON struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	Role     string  `json:"role"`
	Mime     string  `json:"mime"`
	URL      string  `json:"url"`
	Viewport *string `json:"viewport"`
	Caption  *string `json:"caption"`
}

type refJSON struct {
	ID          string      `json:"id"`
	Type        string      `json:"type"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Tags        []string    `json:"tags"`
	Source      *sourceJSON `json:"source"`
	Thumb       *string     `json:"thumb"`
	Kinds       []string    `json:"kinds"`
	CreatedAt   string      `json:"created_at"`
	// Detail-only fields.
	Behavior *string     `json:"behavior,omitempty"`
	Locator  *string     `json:"locator,omitempty"`
	Assets   []assetJSON `json:"assets,omitempty"`
}

func refID(id int64) string   { return fmt.Sprintf("r_%d", id) }
func assetID(id int64) string { return fmt.Sprintf("a_%d", id) }
func assetURL(a store.Asset) string {
	if a.Kind == store.KindDemo {
		return "/demo/" + assetID(a.ID)
	}
	return "/assets/" + assetID(a.ID)
}

// parseID accepts the prefixed form used by the API and MCP ("r_12") and the
// bare number.
func parseID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if i := strings.IndexByte(raw, '_'); i >= 0 {
		raw = raw[i+1:]
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("bad id %q", raw)
	}
	return n, nil
}

// thumb returns the URL of the primary image, or nil.
func thumb(r *store.Reference) *string {
	var pick *store.Asset
	for i := range r.Assets {
		a := &r.Assets[i]
		if a.Kind != store.KindImage {
			continue
		}
		if a.Role == "primary" {
			u := "/assets/" + assetID(a.ID)
			return &u
		}
		if pick == nil {
			pick = a
		}
	}
	if pick == nil {
		return nil
	}
	u := "/assets/" + assetID(pick.ID)
	return &u
}

func kinds(r *store.Reference) []string {
	var out []string
	for _, k := range []store.AssetKind{store.KindImage, store.KindDemo, store.KindClip} {
		for _, a := range r.Assets {
			if a.Kind == k {
				out = append(out, string(k))
				break
			}
		}
	}
	return out
}

func sourceJSONOf(r *store.Reference) *sourceJSON {
	if r.Source == nil {
		return nil
	}
	return &sourceJSON{ID: fmt.Sprintf("s_%d", r.Source.ID), URL: r.Source.URL,
		Kind: r.Source.Kind, Title: r.Source.Title}
}

func listItem(r *store.Reference) refJSON {
	t := thumb(r)
	return refJSON{
		ID: refID(r.ID), Type: string(r.Type), Title: r.Title, Description: r.Description,
		Tags: r.Tags, Source: sourceJSONOf(r), Thumb: t, Kinds: kinds(r),
		CreatedAt: r.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

func detail(r *store.Reference) refJSON {
	j := listItem(r)
	j.Behavior = &r.Behavior
	j.Locator = &r.Locator
	j.Assets = []assetJSON{}
	for _, a := range r.Assets {
		asset := assetJSON{
			ID: assetID(a.ID), Kind: string(a.Kind), Role: a.Role, Mime: a.Mime,
			URL: assetURL(a),
		}
		if a.Viewport != "" {
			v := a.Viewport
			asset.Viewport = &v
		}
		if a.Caption != "" {
			c := a.Caption
			asset.Caption = &c
		}
		j.Assets = append(j.Assets, asset)
	}
	return j
}

// ---- handlers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	t := store.Type(q.Get("type"))
	if t != "" && !t.Valid() {
		writeErr(w, http.StatusBadRequest, "unknown type")
		return
	}
	var tags []string
	for _, tag := range q["tag"] {
		if strings.TrimSpace(tag) != "" {
			tags = append(tags, tag)
		}
	}
	limit := 60
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			writeErr(w, http.StatusBadRequest, "limit must be between 1 and 200")
			return
		}
		limit = n
	}
	offset := 0
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeErr(w, http.StatusBadRequest, "offset must be at least 0")
			return
		}
		offset = n
	}

	var refs []*store.Reference
	var total int
	if text := strings.TrimSpace(q.Get("q")); text != "" {
		hits, n, err := s.store.Search(r.Context(), store.SearchQuery{
			Q: text, Type: t, Tags: tags, Limit: limit, Offset: offset,
		})
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		total = n
		for _, h := range hits {
			refs = append(refs, h.Ref)
		}
	} else {
		var err error
		refs, total, err = s.store.List(r.Context(), store.ListQuery{
			Type: t, Tags: tags, Limit: limit, Offset: offset,
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	items := make([]refJSON, 0, len(refs))
	for _, ref := range refs {
		items = append(items, listItem(ref))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ref, err := s.store.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "no such reference")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail(ref))
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	switch err := s.store.Delete(r.Context(), id); {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "no such reference")
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	t := store.Type(r.URL.Query().Get("type"))
	if t != "" && !t.Valid() {
		writeErr(w, http.StatusBadRequest, "unknown type")
		return
	}
	counts, err := s.store.ListTags(r.Context(), t)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	type tagJSON struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	tags := make([]tagJSON, 0, len(counts))
	for _, c := range counts {
		tags = append(tags, tagJSON{Name: c.Tag, Count: c.Count})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	asset, file, err := s.store.OpenAsset(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "no such asset")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer file.Close()

	if asset.Kind == store.KindDemo {
		// Demo HTML is untrusted: it is only ever served under the sandbox
		// CSP of the /demo route, never raw on the app origin.
		http.Redirect(w, r, "/demo/"+assetID(asset.ID), http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", asset.Mime)
	http.ServeContent(w, r, assetID(asset.ID), time.Time{}, file)
}

func (s *Server) handleDemo(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	asset, file, err := s.store.OpenAsset(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "no such demo")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer file.Close()
	if asset.Kind != store.KindDemo {
		writeErr(w, http.StatusNotFound, "no such demo")
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Security-Policy", DemoCSP)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
