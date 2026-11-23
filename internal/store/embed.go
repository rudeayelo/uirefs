package store

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"time"
)

// Embedder turns texts into vectors. The default implementation calls an
// OpenAI-compatible /v1/embeddings endpoint.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type HTTPEmbedder struct {
	URL    string
	Model  string
	Client *http.Client
}

const DefaultEmbedModel = "Xenova/all-MiniLM-L6-v2"

// NewHTTPEmbedder accepts either a full endpoint URL (path containing
// "embeddings") or a base URL, to which /v1/embeddings is appended.
func NewHTTPEmbedder(endpoint, model string) *HTTPEmbedder {
	endpoint = strings.TrimRight(endpoint, "/")
	if !strings.Contains(endpoint, "embeddings") {
		endpoint += "/v1/embeddings"
	}
	if model == "" {
		model = DefaultEmbedModel
	}
	return &HTTPEmbedder{URL: endpoint, Model: model, Client: &http.Client{Timeout: 60 * time.Second}}
}

func (e *HTTPEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"model": e.Model, "input": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("embeddings endpoint: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embeddings endpoint: %w", err)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings endpoint: got %d vectors for %d inputs", len(out.Data), len(texts))
	}
	vecs := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vecs) || len(d.Embedding) == 0 {
			return nil, fmt.Errorf("embeddings endpoint: malformed response")
		}
		vecs[d.Index] = d.Embedding
	}
	return vecs, nil
}

func embedText(r *Reference) string {
	return strings.Join([]string{r.Title, r.Description, r.Behavior, strings.Join(r.Tags, " ")}, "\n")
}

func encodeVec(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}

func decodeVec(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return math.Inf(-1)
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return math.Inf(-1)
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// embedRef stores the vector of one reference. It is best effort: a down
// endpoint must not block add or update, and `uirefs reindex` repairs it.
func (s *Store) embedRef(ctx context.Context, id int64) {
	if s.emb == nil {
		return
	}
	ref, err := s.Get(ctx, id)
	if err != nil {
		return
	}
	vecs, err := s.emb.Embed(ctx, []string{embedText(ref)})
	if err != nil {
		log.Printf("embed reference %d: %v (run `uirefs reindex` later)", id, err)
		return
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE refs SET embedding = ? WHERE id = ?`, encodeVec(vecs[0]), id); err != nil {
		log.Printf("store embedding %d: %v", id, err)
	}
}

const reindexBatch = 16

// Reindex embeds every reference again and returns how many it updated.
func (s *Store) Reindex(ctx context.Context) (int, error) {
	if s.emb == nil {
		return 0, fmt.Errorf("embeddings are off: set UIREFS_EMBED_URL")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM refs ORDER BY id`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := closeRows(rows); err != nil {
		return 0, err
	}
	done := 0
	for start := 0; start < len(ids); start += reindexBatch {
		end := min(start+reindexBatch, len(ids))
		var refs []*Reference
		var texts []string
		for _, id := range ids[start:end] {
			r, err := s.Get(ctx, id)
			if err != nil {
				return done, err
			}
			refs = append(refs, r)
			texts = append(texts, embedText(r))
		}
		vecs, err := s.emb.Embed(ctx, texts)
		if err != nil {
			return done, err
		}
		for i, r := range refs {
			if _, err := s.db.ExecContext(ctx, `UPDATE refs SET embedding = ? WHERE id = ?`, encodeVec(vecs[i]), r.ID); err != nil {
				return done, err
			}
			done++
		}
	}
	return done, nil
}
