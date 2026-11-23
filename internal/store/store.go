// Package store holds the uirefs data model: references, sources, assets on
// disk, the five types, search and permanent delete.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Type string

const (
	TypeComponent Type = "component"
	TypeSite      Type = "site"
	TypeAnimation Type = "animation"
	TypeIdentity  Type = "identity"
	TypeResource  Type = "resource"
)

type AssetKind string

const (
	KindImage AssetKind = "image"
	KindDemo  AssetKind = "demo"
	KindClip  AssetKind = "clip"
)

const (
	SourceWeb   = "web"
	SourceTweet = "tweet"
)

const (
	dbFile    = "uirefs.db"
	assetsDir = "assets"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrInvalid wraps every rejection caused by the caller's input.
	ErrInvalid = errors.New("invalid")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

type Source struct {
	ID      int64     `json:"id"`
	URL     string    `json:"url"`
	Kind    string    `json:"kind"`
	Title   string    `json:"title"`
	AddedAt time.Time `json:"added_at"`
}

type Asset struct {
	ID       int64
	RefID    int64
	Kind     AssetKind
	Role     string
	Viewport string
	Caption  string
	Mime     string
	Size     int64
}

type Reference struct {
	ID          int64
	Type        Type
	Title       string
	Description string
	Behavior    string
	Tags        []string
	Source      *Source
	Locator     string
	CreatedAt   time.Time
	Assets      []Asset
}

// AssetOf returns the first asset of a kind.
func (r *Reference) AssetOf(kind AssetKind) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Kind == kind {
			return a, true
		}
	}
	return Asset{}, false
}

// Valid reports whether t is one of the five reference types.
func (t Type) Valid() bool {
	switch t {
	case TypeComponent, TypeSite, TypeAnimation, TypeIdentity, TypeResource:
		return true
	}
	return false
}

type Store struct {
	db  *sql.DB
	dir string
	emb Embedder
}

const schema = `
CREATE TABLE IF NOT EXISTS sources (
  id INTEGER PRIMARY KEY,
  url TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  added_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS refs (
  id INTEGER PRIMARY KEY,
  type TEXT NOT NULL,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  behavior TEXT NOT NULL DEFAULT '',
  source_id INTEGER REFERENCES sources(id),
  locator TEXT NOT NULL DEFAULT '',
  embedding BLOB,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS ref_tags (
  ref_id INTEGER NOT NULL REFERENCES refs(id) ON DELETE CASCADE,
  tag TEXT NOT NULL,
  PRIMARY KEY (ref_id, tag)
);
CREATE INDEX IF NOT EXISTS ref_tags_tag ON ref_tags(tag);
CREATE TABLE IF NOT EXISTS assets (
  id INTEGER PRIMARY KEY,
  ref_id INTEGER NOT NULL REFERENCES refs(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  role TEXT NOT NULL,
  viewport TEXT NOT NULL DEFAULT '',
  caption TEXT NOT NULL DEFAULT '',
  mime TEXT NOT NULL,
  size INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS assets_ref ON assets(ref_id);
CREATE VIRTUAL TABLE IF NOT EXISTS refs_fts USING fts5(title, description, behavior, tags, tokenize='porter unicode61');
`

// Open opens (and creates when needed) the store in dir. emb may be nil.
func Open(dir string, emb Embedder) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, assetsDir), 0o700); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.Join(dir, dbFile) +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection: writes are rare and this avoids SQLITE_BUSY between handlers.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db, dir: dir, emb: emb}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Dir returns the data directory (for diagnostics and tests).
func (s *Store) Dir() string { return s.dir }

type queryer interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

func (s *Store) Get(ctx context.Context, id int64) (*Reference, error) {
	return getRef(ctx, s.db, id)
}

func getRef(ctx context.Context, q queryer, id int64) (*Reference, error) {
	var (
		r                      Reference
		created                int64
		srcID, srcAdded        sql.NullInt64
		srcURL, srcKind, srcTi sql.NullString
	)
	err := q.QueryRowContext(ctx, `
SELECT r.id, r.type, r.title, r.description, r.behavior, r.locator, r.created_at,
       s.id, s.url, s.kind, s.title, s.added_at
FROM refs r LEFT JOIN sources s ON s.id = r.source_id WHERE r.id = ?`, id).
		Scan(&r.ID, &r.Type, &r.Title, &r.Description, &r.Behavior, &r.Locator, &created,
			&srcID, &srcURL, &srcKind, &srcTi, &srcAdded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.CreatedAt = time.Unix(created, 0).UTC()
	if srcID.Valid {
		r.Source = &Source{ID: srcID.Int64, URL: srcURL.String, Kind: srcKind.String,
			Title: srcTi.String, AddedAt: time.Unix(srcAdded.Int64, 0).UTC()}
	}

	rows, err := q.QueryContext(ctx, `SELECT tag FROM ref_tags WHERE ref_id = ? ORDER BY tag`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return nil, err
		}
		r.Tags = append(r.Tags, t)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}

	rows, err = q.QueryContext(ctx, `
SELECT id, ref_id, kind, role, viewport, caption, mime, size
FROM assets WHERE ref_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a Asset
		if err := rows.Scan(&a.ID, &a.RefID, &a.Kind, &a.Role, &a.Viewport, &a.Caption, &a.Mime, &a.Size); err != nil {
			rows.Close()
			return nil, err
		}
		r.Assets = append(r.Assets, a)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	return &r, nil
}

func closeRows(rows *sql.Rows) error {
	err := rows.Err()
	if cerr := rows.Close(); err == nil {
		err = cerr
	}
	return err
}

// OpenAsset opens the stored file of an asset.
func (s *Store) OpenAsset(ctx context.Context, id int64) (Asset, *os.File, error) {
	var a Asset
	err := s.db.QueryRowContext(ctx,
		`SELECT id, ref_id, kind, role, viewport, caption, mime, size FROM assets WHERE id = ?`, id).
		Scan(&a.ID, &a.RefID, &a.Kind, &a.Role, &a.Viewport, &a.Caption, &a.Mime, &a.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return a, nil, ErrNotFound
	}
	if err != nil {
		return a, nil, err
	}
	f, err := os.Open(s.assetPath(a))
	if err != nil {
		return a, nil, err
	}
	return a, f, nil
}

func (s *Store) assetPath(a Asset) string {
	return filepath.Join(s.dir, assetsDir, fmt.Sprint(a.RefID), fmt.Sprint(a.ID)+extFor(a.Mime))
}

func (s *Store) refDir(refID int64) string {
	return filepath.Join(s.dir, assetsDir, fmt.Sprint(refID))
}

type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// ListTags returns the tag vocabulary in use with counts, optionally for one type.
func (s *Store) ListTags(ctx context.Context, t Type) ([]TagCount, error) {
	q := `SELECT t.tag, COUNT(*) FROM ref_tags t JOIN refs r ON r.id = t.ref_id`
	var args []any
	if t != "" {
		q += ` WHERE r.type = ?`
		args = append(args, t)
	}
	q += ` GROUP BY t.tag ORDER BY COUNT(*) DESC, t.tag`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	out := []TagCount{}
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Tag, &tc.Count); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, tc)
	}
	return out, closeRows(rows)
}

func normTags(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}
