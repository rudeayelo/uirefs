package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// png1x1 is a minimal valid 1x1 transparent PNG.
var png1x1 = mustDecode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")

func mustDecode(s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

var (
	mp4Clip  = append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, bytes.Repeat([]byte{7}, 16)...)
	webmClip = append([]byte{0x1a, 0x45, 0xdf, 0xa3, 1, 2, 3, 4}, bytes.Repeat([]byte{7}, 16)...)
)

func openStore(t *testing.T, emb Embedder) *Store {
	t.Helper()
	s, err := Open(t.TempDir(), emb)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func img(data ...byte) AssetInput {
	return AssetInput{Kind: KindImage, Data: data}
}

func demo(html string) AssetInput {
	return AssetInput{Kind: KindDemo, Data: []byte(html)}
}

func add(t *testing.T, s *Store, in NewReference) *Reference {
	t.Helper()
	r, err := s.Add(context.Background(), in)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	return r
}

func TestAddValidation(t *testing.T) {
	s := openStore(t, nil)
	ctx := context.Background()

	big := append(append([]byte{}, png1x1...), make([]byte, 6<<20)...)
	cases := []struct {
		name string
		in   NewReference
		err  string
	}{
		{
			name: "unknown type is rejected",
			in:   NewReference{Type: "widget", Title: "T", Description: "D", Assets: []AssetInput{img(png1x1...)}},
			err:  "type must be component, site, animation, identity or resource",
		},
		{
			name: "resource with no image, no demo and no source_url succeeds",
			in:   NewReference{Type: TypeResource, Title: "T", Description: "D"},
		},
		{
			name: "title and description are required",
			in:   NewReference{Type: TypeComponent, Title: " ", Description: "D", Assets: []AssetInput{img(png1x1...)}},
			err:  "title and description are required",
		},
		{
			name: "a png cannot be a clip",
			in:   NewReference{Type: TypeAnimation, Title: "T", Description: "D", Assets: []AssetInput{{Kind: KindClip, Data: png1x1}}},
			err:  "cannot be",
		},
		{
			name: "bad source_url is rejected",
			in:   NewReference{Type: TypeSite, Title: "T", Description: "D", SourceURL: "not a url", Assets: []AssetInput{img(png1x1...)}},
			err:  "source_url must be an http(s) URL",
		},
		{
			name: "component with no image succeeds",
			in:   NewReference{Type: TypeComponent, Title: "T", Description: "D", Assets: []AssetInput{demo("<p>x</p>")}},
		},
		{
			name: "site without a source_url succeeds",
			in:   NewReference{Type: TypeSite, Title: "T", Description: "D", Assets: []AssetInput{img(png1x1...)}},
		},
		{
			name: "animation with only an image succeeds",
			in:   NewReference{Type: TypeAnimation, Title: "T", Description: "D", Assets: []AssetInput{img(png1x1...)}},
		},
		{
			name: "identity with no image succeeds",
			in:   NewReference{Type: TypeIdentity, Title: "T", Description: "D", Assets: []AssetInput{demo("<p>x</p>")}},
		},
		{
			name: "any role is accepted",
			in: NewReference{Type: TypeComponent, Title: "T", Description: "D",
				Assets: []AssetInput{{Kind: KindImage, Role: "secondary", Data: png1x1}}},
		},
		{
			name: "several demos and clips are accepted",
			in: NewReference{Type: TypeAnimation, Title: "T", Description: "D",
				Assets: []AssetInput{demo("<p>a</p>"), demo("<p>b</p>"),
					{Kind: KindClip, Data: mp4Clip}, {Kind: KindClip, Data: webmClip}}},
		},
		{
			name: "a 6 MB file is accepted",
			in: NewReference{Type: TypeIdentity, Title: "T", Description: "D",
				Assets: []AssetInput{{Kind: KindImage, Data: big}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Add(ctx, tc.in)
			if tc.err == "" {
				if err != nil {
					t.Fatalf("expected success, got %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.err)
			}
		})
	}
}

func TestSourceKind(t *testing.T) {
	for _, tc := range []struct {
		url, kind string
	}{
		{"https://x.com/user/status/123", SourceTweet},
		{"https://twitter.com/user/status/123", SourceTweet},
		{"https://example.com/page", SourceWeb},
	} {
		k, err := sourceKind(tc.url)
		if err != nil {
			t.Fatalf("%s: %v", tc.url, err)
		}
		if k != tc.kind {
			t.Errorf("%s: got %q want %q", tc.url, k, tc.kind)
		}
	}
}

func TestAddGetRoundTrip(t *testing.T) {
	s := openStore(t, nil)
	ctx := context.Background()

	r := add(t, s, NewReference{
		Type:        TypeComponent,
		Title:       "  Pricing card ",
		Description: "Annual toggle",
		Behavior:    "peels from the top-left",
		Tags:        []string{" Click ", "click", "concept:pricing"},
		SourceURL:   "https://example.com/pricing",
		Locator:     " #hero ",
		Assets: []AssetInput{
			{Kind: KindImage, Role: "primary", Viewport: "1440x900", Caption: "Default", Data: png1x1},
			{Kind: KindImage, Role: "state", Caption: "Hover", Data: png1x1},
			demo("<p>demo</p>"),
		},
	})

	if r.Title != "Pricing card" {
		t.Errorf("title = %q", r.Title)
	}
	if r.Locator != "#hero" {
		t.Errorf("locator = %q", r.Locator)
	}
	if len(r.Tags) != 2 || r.Tags[0] != "click" || r.Tags[1] != "concept:pricing" {
		t.Errorf("tags = %v", r.Tags)
	}
	if r.Source == nil || r.Source.URL != "https://example.com/pricing" || r.Source.Kind != SourceWeb {
		t.Errorf("source = %+v", r.Source)
	}
	if len(r.Assets) != 3 {
		t.Fatalf("assets = %d", len(r.Assets))
	}
	if r.Assets[0].Mime != "image/png" || r.Assets[0].Role != "primary" {
		t.Errorf("asset 0 = %+v", r.Assets[0])
	}
	if _, ok := r.AssetOf(KindDemo); !ok {
		t.Error("no demo asset")
	}

	got, err := s.Get(ctx, r.ID)
	if err != nil || got.Title != r.Title {
		t.Fatalf("get: %v %v", got, err)
	}

	// The files are on disk and readable through OpenAsset.
	_, f, err := s.OpenAsset(ctx, r.Assets[0].ID)
	if err != nil {
		t.Fatalf("open asset: %v", err)
	}
	f.Close()

	if _, err := s.Get(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("get missing: %v", err)
	}
}

func TestUpdateTypeAndIndexes(t *testing.T) {
	s := openStore(t, nil)
	ctx := context.Background()

	r := add(t, s, NewReference{
		Type: TypeComponent, Title: "Old title", Description: "d",
		Assets: []AssetInput{img(png1x1...)},
	})

	// Only the five types are accepted.
	_, err := s.Update(ctx, r.ID, Update{Type: ptr(Type("widget"))})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "type must be") {
		t.Fatalf("unknown type: %v", err)
	}
	// Any of the five works, whatever the assets.
	if _, err := s.Update(ctx, r.ID, Update{Type: ptr(TypeAnimation)}); err != nil {
		t.Fatalf("type change to animation: %v", err)
	}

	title := "New title"
	tags := []string{"button"}
	if _, err := s.Update(ctx, r.ID, Update{Title: &title, Tags: &tags}); err != nil {
		t.Fatal(err)
	}
	hits, _, err := s.Search(ctx, SearchQuery{Q: "New title"})
	if err != nil || len(hits) != 1 || hits[0].Ref.ID != r.ID {
		t.Fatalf("search after update: %v %v", hits, err)
	}
	hits, _, err = s.Search(ctx, SearchQuery{Q: "Old"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("old title still indexed: %v", hitTitles(hits))
	}
	// Tags are part of the FTS index.
	hits, _, _ = s.Search(ctx, SearchQuery{Q: "button"})
	if len(hits) != 1 || hits[0].Ref.ID != r.ID {
		t.Errorf("tag not indexed: %v", hits)
	}
}

func ptr[T any](v T) *T { return &v }

func TestUpdateAssets(t *testing.T) {
	s := openStore(t, nil)
	ctx := context.Background()

	r := add(t, s, NewReference{
		Type: TypeAnimation, Title: "Peel", Description: "d",
		Assets: []AssetInput{{Kind: KindClip, Data: mp4Clip}},
	})
	clip, ok := r.AssetOf(KindClip)
	if !ok {
		t.Fatal("no clip")
	}

	// A demo can be attached to a reference that already exists.
	got, err := s.Update(ctx, r.ID, Update{AddAssets: []AssetInput{demo("<p>peel v1</p>")}})
	if err != nil {
		t.Fatalf("attach demo: %v", err)
	}
	first, ok := got.AssetOf(KindDemo)
	if !ok {
		t.Fatal("no demo after update")
	}
	if _, ok := got.AssetOf(KindClip); !ok {
		t.Error("clip lost")
	}
	if data := assetFile(t, s, got.ID, first.ID, ".html"); string(data) != "<p>peel v1</p>" {
		t.Errorf("demo file = %q", data)
	}

	// A second demo is appended, not replaced: the first one stays.
	got, err = s.Update(ctx, r.ID, Update{AddAssets: []AssetInput{demo("<p>peel v2</p>")}})
	if err != nil {
		t.Fatalf("add second demo: %v", err)
	}
	var demos []Asset
	for _, a := range got.Assets {
		if a.Kind == KindDemo {
			demos = append(demos, a)
		}
	}
	if len(demos) != 2 {
		t.Fatalf("demos = %d", len(demos))
	}
	if demos[0].ID != first.ID {
		t.Error("the first demo was replaced")
	}
	if data := assetFile(t, s, got.ID, demos[0].ID, ".html"); string(data) != "<p>peel v1</p>" {
		t.Errorf("first demo file = %q", data)
	}
	if data := assetFile(t, s, got.ID, demos[1].ID, ".html"); string(data) != "<p>peel v2</p>" {
		t.Errorf("second demo file = %q", data)
	}

	// Removing the clip is allowed.
	got, err = s.Update(ctx, r.ID, Update{RemoveAssets: []int64{clip.ID}})
	if err != nil {
		t.Fatalf("remove clip: %v", err)
	}
	if _, ok := got.AssetOf(KindClip); ok {
		t.Error("clip not removed")
	}
	if _, err := os.Stat(assetPath(s, got.ID, clip.ID, ".mp4")); !os.IsNotExist(err) {
		t.Error("clip file still on disk")
	}

	// An animation may lose its last media.
	got, err = s.Update(ctx, r.ID, Update{RemoveAssets: []int64{demos[0].ID, demos[1].ID}})
	if err != nil {
		t.Fatalf("remove all demos: %v", err)
	}
	if len(got.Assets) != 0 {
		t.Errorf("assets left = %+v", got.Assets)
	}
	if _, err := os.Stat(assetPath(s, got.ID, demos[1].ID, ".html")); !os.IsNotExist(err) {
		t.Error("demo file still on disk")
	}

	// An asset id from another reference is rejected.
	other := add(t, s, NewReference{Type: TypeComponent, Title: "Other", Description: "d",
		Assets: []AssetInput{img(png1x1...)}})
	_, err = s.Update(ctx, r.ID, Update{RemoveAssets: []int64{other.Assets[0].ID}})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "not on this reference") {
		t.Fatalf("foreign asset id: %v", err)
	}
}

func TestUpdateAssetsIsAtomic(t *testing.T) {
	s := openStore(t, nil)
	ctx := context.Background()

	r := add(t, s, NewReference{Type: TypeComponent, Title: "Card", Description: "d",
		Assets: []AssetInput{img(png1x1...)}})
	title := "Renamed"

	// A rejected asset rolls back the fields, rows and files of the same call.
	if _, err := s.Update(ctx, r.ID, Update{Title: &title,
		AddAssets:    []AssetInput{demo("<p>x</p>"), {Kind: KindClip, Data: png1x1}},
		RemoveAssets: []int64{r.Assets[0].ID}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a png as a clip: %v", err)
	}
	got, err := s.Get(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Card" || len(got.Assets) != 1 || got.Assets[0].Kind != KindImage {
		t.Errorf("update was not rolled back: %+v", got)
	}
	entries, err := os.ReadDir(filepath.Join(s.dir, assetsDir, fmt.Sprint(r.ID)))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("files = %v", entries)
	}
}

func assetPath(s *Store, refID, assetID int64, ext string) string {
	return filepath.Join(s.dir, assetsDir, fmt.Sprint(refID), fmt.Sprint(assetID)+ext)
}

func assetFile(t *testing.T, s *Store, refID, assetID int64, ext string) []byte {
	t.Helper()
	data, err := os.ReadFile(assetPath(s, refID, assetID, ext))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDeleteIsPermanent(t *testing.T) {
	s := openStore(t, nil)
	ctx := context.Background()

	url := "https://x.com/u/status/1"
	r1 := add(t, s, NewReference{Type: TypeComponent, Title: "Shared source A", Description: "d",
		SourceURL: url, Assets: []AssetInput{img(png1x1...)}})
	r2 := add(t, s, NewReference{Type: TypeComponent, Title: "Shared source B", Description: "d",
		SourceURL: url, Assets: []AssetInput{img(png1x1...)}})
	refDir := filepath.Join(s.dir, assetsDir, fmt.Sprint(r1.ID))

	if _, err := os.Stat(refDir); err != nil {
		t.Fatalf("asset dir: %v", err)
	}

	if err := s.Delete(ctx, r1.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(ctx, r1.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("still gettable: %v", err)
	}
	if _, err := os.Stat(refDir); !os.IsNotExist(err) {
		t.Errorf("files still on disk")
	}
	if _, _, err := s.OpenAsset(ctx, r1.Assets[0].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("asset still openable: %v", err)
	}
	hits, _, _ := s.Search(ctx, SearchQuery{Q: "Shared"})
	if len(hits) != 1 || hits[0].Ref.ID != r2.ID {
		t.Fatalf("search after delete: %v", hits)
	}
	// The source survives while another reference uses it.
	r2b, _ := s.Get(ctx, r2.ID)
	if r2b.Source == nil {
		t.Fatal("source pruned too early")
	}

	if err := s.Delete(ctx, r2.ID); err != nil {
		t.Fatalf("delete 2: %v", err)
	}
	var n int
	s.db.QueryRow("SELECT COUNT(*) FROM sources").Scan(&n)
	if n != 0 {
		t.Errorf("source not pruned: %d left", n)
	}

	if err := s.Delete(ctx, r2.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
}

func TestSearchRankingFTS(t *testing.T) {
	s := openStore(t, nil)
	ctx := context.Background()

	add(t, s, NewReference{Type: TypeComponent, Title: "Pricing with annual toggle",
		Description: "The plan selector", Behavior: "b", Assets: []AssetInput{img(png1x1...)}})
	add(t, s, NewReference{Type: TypeSite, Title: "Studio", Description: "a pricing mention in passing",
		Behavior: "b", SourceURL: "https://studio.example", Assets: []AssetInput{img(png1x1...)}})
	add(t, s, NewReference{Type: TypeComponent, Title: "Nav bar", Description: "links",
		Behavior: "b", Assets: []AssetInput{img(png1x1...)}})

	// Terms are OR-ed; a title match outranks a description-only match.
	hits, total, err := s.Search(ctx, SearchQuery{Q: "pricing"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(hits) != 2 {
		t.Fatalf("total=%d hits=%d", total, len(hits))
	}
	if hits[0].Ref.Title != "Pricing with annual toggle" {
		t.Errorf("top hit = %q", hits[0].Ref.Title)
	}
	if hits[1].Ref.Title != "Studio" {
		t.Errorf("second hit = %q", hits[1].Ref.Title)
	}

	// A reference matching both words ranks above one matching only "pricing".
	hits, _, _ = s.Search(ctx, SearchQuery{Q: "pricing toggle"})
	if len(hits) != 2 || hits[0].Ref.Title != "Pricing with annual toggle" || hits[1].Ref.Title != "Studio" {
		t.Errorf("multi-word ranking: %v", hitTitles(hits))
	}

	// Filters.
	hits, _, _ = s.Search(ctx, SearchQuery{Q: "pricing", Type: TypeSite})
	if len(hits) != 1 || hits[0].Ref.Type != TypeSite {
		t.Errorf("type filter: %v", hits)
	}
	hits, _, _ = s.Search(ctx, SearchQuery{Q: "nav"})
	if len(hits) != 1 || hits[0].Ref.Title != "Nav bar" {
		t.Errorf("single term: %v", hits)
	}

	if _, _, err := s.Search(ctx, SearchQuery{Q: "  "}); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty query: %v", err)
	}
}

func hitTitles(hits []Hit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Ref.Title)
	}
	return out
}

func TestNaturalLanguageSearch(t *testing.T) {
	s := openStore(t, nil)
	ctx := context.Background()

	// References the way an agent would write them.
	add(t, s, NewReference{Type: TypeComponent, Title: "Pricing with annual toggle",
		Description: "Plan selector with annual billing toggle", Behavior: "b",
		Tags: []string{"pricing", "toggle"}, Assets: []AssetInput{img(png1x1...)}})
	add(t, s, NewReference{Type: TypeSite, Title: "Console site",
		Description: "Personal website styled as a game console", Behavior: "b",
		SourceURL: "https://console.example", Assets: []AssetInput{img(png1x1...)}})
	add(t, s, NewReference{Type: TypeAnimation, Title: "Sticker peel",
		Description: "Icon sticker that peels off from the corner on click", Behavior: "b",
		Tags: []string{"click", "sticker"}, Assets: []AssetInput{demo("<p>peel</p>")}})

	cases := []struct {
		query string
		want  string
	}{
		{"pricing toggle", "Pricing with annual toggle"},
		{"switch between yearly and monthly pricing", "Pricing with annual toggle"},
		{"a site that looks like a game console", "Console site"},
		{"something that peels off when you click it", "Sticker peel"},
	}
	for _, tc := range cases {
		hits, _, err := s.Search(ctx, SearchQuery{Q: tc.query})
		if err != nil {
			t.Fatalf("%q: %v", tc.query, err)
		}
		if len(hits) == 0 {
			t.Errorf("%q: no results", tc.query)
			continue
		}
		if hits[0].Ref.Title != tc.want {
			t.Errorf("%q: top = %q, want %q (all: %v)", tc.query, hits[0].Ref.Title, tc.want, hitTitles(hits))
		}
	}

	// A sentence with nothing in common with the library stays empty.
	hits, total, err := s.Search(ctx, SearchQuery{Q: "keyboard shortcut for spreadsheet cells"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(hits) != 0 {
		t.Errorf("no-match query returned: %v", hitTitles(hits))
	}

	// A query that is only stop words is empty, not an error-less scan.
	if _, _, err := s.Search(ctx, SearchQuery{Q: "the and of"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("stop-word-only query: %v", err)
	}
}

func TestTagMatchRanksHigh(t *testing.T) {
	s := openStore(t, nil)
	ctx := context.Background()

	// "click" appears only as a tag on the first, only in the description
	// of the second: the tag (weight 4) must outrank the description
	// (weight 2).
	add(t, s, NewReference{Type: TypeComponent, Title: "Menu", Description: "menu links",
		Tags: []string{"click"}, Assets: []AssetInput{img(png1x1...)}})
	add(t, s, NewReference{Type: TypeComponent, Title: "Button", Description: "activated with a click",
		Behavior: "b", Assets: []AssetInput{img(png1x1...)}})

	hits, _, err := s.Search(ctx, SearchQuery{Q: "click"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].Ref.Title != "Menu" {
		t.Fatalf("tag ranking: %v", hitTitles(hits))
	}
}

func TestSearchTagFilter(t *testing.T) {
	s := openStore(t, nil)
	ctx := context.Background()

	add(t, s, NewReference{Type: TypeAnimation, Title: "Peel", Description: "d",
		Tags: []string{"click", "sticker"}, Assets: []AssetInput{demo("<p/>")}})
	add(t, s, NewReference{Type: TypeAnimation, Title: "Scroll shader", Description: "d",
		Tags: []string{"scroll", "shader"}, Assets: []AssetInput{demo("<p/>")}})

	hits, _, err := s.Search(ctx, SearchQuery{Q: "peel", Tags: []string{"Click", "STICKER"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Ref.Title != "Peel" {
		t.Fatalf("tag filter (all must match): %v", hits)
	}
}

// fakeEmbedder returns canned vectors by exact text.
type fakeEmbedder struct{ vectors map[string][]float32 }

func (f fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, ok := f.vectors[t]
		if !ok {
			return nil, fmt.Errorf("no vector for %q", t)
		}
		out[i] = v
	}
	return out, nil
}

func TestSearchFusionChangesRanking(t *testing.T) {
	// Exact texts embedText produces for the three references.
	vecs := map[string][]float32{
		"Toggle switch\npricing states\nb\n":       {1, 0},
		"Pricing card toggle\nannual billing\nb\n": {0, 1},
		"Pricing table\nsimple\nb\n":               {0.7, 0.7},
	}
	s := openStore(t, fakeEmbedder{vectors: vecs})
	ctx := context.Background()

	r1 := add(t, s, NewReference{Type: TypeComponent, Title: "Toggle switch", Description: "pricing states",
		Behavior: "b", Assets: []AssetInput{img(png1x1...)}})
	r2 := add(t, s, NewReference{Type: TypeComponent, Title: "Pricing card toggle", Description: "annual billing",
		Behavior: "b", Assets: []AssetInput{img(png1x1...)}})
	add(t, s, NewReference{Type: TypeComponent, Title: "Pricing table", Description: "simple",
		Behavior: "b", Assets: []AssetInput{img(png1x1...)}})
	query := "pricing toggle"
	vecs[query] = []float32{1, 0} // semantically closest to r1 only

	// FTS matches all three under OR; r2 leads because both its terms sit
	// in the title.
	s.emb = nil
	hits, _, err := s.Search(ctx, SearchQuery{Q: query})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Fatalf("hits = %d", len(hits))
	}
	if hits[0].Ref.ID != r2.ID {
		t.Fatalf("keyword expectation broken: top = %q", hits[0].Ref.Title)
	}

	// With fusion the reference that both lists rank high wins:
	// r1 = 1/62 (FTS rank 2) + 1/61 (semantic rank 1) beats
	// r2 = 1/61 (FTS rank 1) + 1/63 (semantic rank 3).
	s.emb = fakeEmbedder{vectors: vecs}
	hits, _, err = s.Search(ctx, SearchQuery{Q: query})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Fatalf("hits = %d", len(hits))
	}
	if hits[0].Ref.ID != r1.ID {
		t.Fatalf("fusion did not change the ranking: top = %q", hits[0].Ref.Title)
	}
	if hits[1].Ref.ID != r2.ID {
		t.Fatalf("fusion second = %q", hits[1].Ref.Title)
	}
}

// fakeEndpoint serves an OpenAI-compatible /v1/embeddings that returns
// canned vectors by exact input text.
func fakeEndpoint(t *testing.T, vectors map[string][]float32) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/embeddings" {
			http.NotFound(w, req)
			return
		}
		var in struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		data := make([]struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}, len(in.Input))
		for i, text := range in.Input {
			v, ok := vectors[text]
			if !ok {
				w.WriteHeader(500)
				return
			}
			data[i] = struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{i, v}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHTTPEmbedderAndReindex(t *testing.T) {
	vectors := map[string][]float32{}
	ep := fakeEndpoint(t, vectors)

	// With no embedder: add stores nothing, reindex errors.
	plain := openStore(t, nil)
	ctx := context.Background()
	r := add(t, plain, NewReference{Type: TypeComponent, Title: "Card", Description: "d",
		Behavior: "b", Assets: []AssetInput{img(png1x1...)}})
	if _, err := plain.Reindex(ctx); err == nil {
		t.Error("reindex without embedder should fail")
	}
	var hasVec bool
	plain.db.QueryRow("SELECT embedding IS NOT NULL FROM refs WHERE id=?", r.ID).Scan(&hasVec)
	if hasVec {
		t.Error("embedding stored without embedder")
	}

	// Reopen with the HTTP embedder wired to the fake endpoint.
	got, _ := plain.Get(ctx, r.ID)
	vectors[embedText(got)] = []float32{1, 0}
	vectors["card query"] = []float32{1, 0}
	plain.Close()
	s, err := Open(plain.dir, NewHTTPEmbedder(ep.URL, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	n, err := s.Reindex(ctx)
	if err != nil || n != 1 {
		t.Fatalf("reindex: n=%d err=%v", n, err)
	}
	hits, _, err := s.Search(ctx, SearchQuery{Q: "card query"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Ref.ID != r.ID {
		t.Fatalf("semantic hit after reindex: %+v", hits)
	}

	// A down endpoint degrades to keyword-only results.
	s.emb = NewHTTPEmbedder("http://127.0.0.1:1/v1/embeddings", "")
	if _, _, err := s.Search(ctx, SearchQuery{Q: "card"}); err != nil {
		t.Fatalf("search with down endpoint: %v", err)
	}
}

func TestList(t *testing.T) {
	s := openStore(t, nil)
	ctx := context.Background()

	add(t, s, NewReference{Type: TypeComponent, Title: "First", Description: "d",
		Tags: []string{"button"}, Assets: []AssetInput{img(png1x1...)}})
	add(t, s, NewReference{Type: TypeComponent, Title: "Second", Description: "d",
		Tags: []string{"button", "card"}, Assets: []AssetInput{img(png1x1...)}})
	add(t, s, NewReference{Type: TypeSite, Title: "Third", Description: "d",
		SourceURL: "https://example.com", Assets: []AssetInput{img(png1x1...)}})

	refs, total, err := s.List(ctx, ListQuery{})
	if err != nil || total != 3 || len(refs) != 3 {
		t.Fatalf("list: %d %v", total, err)
	}
	if refs[0].Title != "Third" {
		t.Errorf("not newest first: %v", refs[0].Title)
	}

	refs, total, _ = s.List(ctx, ListQuery{Type: TypeComponent, Tags: []string{"button", "card"}})
	if total != 1 || len(refs) != 1 || refs[0].Title != "Second" {
		t.Fatalf("filtered list: %d %v", total, refs)
	}
	add(t, s, NewReference{Type: TypeResource, Title: "Isometric set", Description: "illustration library"})
	refs, total, _ = s.List(ctx, ListQuery{Type: TypeResource})
	if total != 1 || len(refs) != 1 || refs[0].Title != "Isometric set" {
		t.Fatalf("resource list: %d %v", total, refs)
	}
	hits, _, err := s.Search(ctx, SearchQuery{Q: "illustration", Type: TypeResource})
	if err != nil || len(hits) != 1 {
		t.Fatalf("resource search: %v %v", hits, err)
	}
	if hits, _, _ = s.Search(ctx, SearchQuery{Q: "illustration", Type: TypeSite}); len(hits) != 0 {
		t.Fatalf("search leaked across types: %v", hits)
	}
	s.Delete(ctx, refs[0].ID)
	refs, total, _ = s.List(ctx, ListQuery{Limit: 2, Offset: 1})
	if total != 3 || len(refs) != 2 || refs[0].Title != "Second" {
		t.Fatalf("paged list: %d %v", total, refs)
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := add(t, s, NewReference{Type: TypeComponent, Title: "Persist", Description: "d", Assets: []AssetInput{img(png1x1...)}})
	s.Close()

	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.Get(context.Background(), r.ID)
	if err != nil || got.Title != "Persist" {
		t.Fatalf("after reopen: %+v %v", got, err)
	}
	hits, _, err := s2.Search(context.Background(), SearchQuery{Q: "persist"})
	if err != nil || len(hits) != 1 {
		t.Fatalf("fts after reopen: %v %v", hits, err)
	}
}

func TestClipMimeSniffing(t *testing.T) {
	s := openStore(t, nil)

	// mp4 is not in Go's sniffing table but must be accepted as a clip, and a
	// reference may hold several clips.
	r := add(t, s, NewReference{Type: TypeAnimation, Title: "Clips", Description: "d",
		Assets: []AssetInput{{Kind: KindClip, Data: mp4Clip}, {Kind: KindClip, Data: webmClip}}})
	if r.Assets[0].Mime != "video/mp4" {
		t.Errorf("mp4 mime = %q", r.Assets[0].Mime)
	}
	if r.Assets[1].Mime != "video/webm" {
		t.Errorf("webm mime = %q", r.Assets[1].Mime)
	}
}
