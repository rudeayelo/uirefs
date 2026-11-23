package store

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

type AssetInput struct {
	Kind     AssetKind
	Role     string
	Viewport string
	Caption  string
	Data     []byte
}

type NewReference struct {
	Type        Type
	Title       string
	Description string
	Behavior    string
	Tags        []string
	SourceURL   string
	Locator     string
	Assets      []AssetInput
}

// Update changes only the fields that are non-nil. AddAssets appends assets to
// the reference and RemoveAssets deletes those asset ids from it. The whole
// change is atomic: if an asset is rejected, nothing is written.
type Update struct {
	Type         *Type
	Title        *string
	Description  *string
	Behavior     *string
	Tags         *[]string
	SourceURL    *string
	Locator      *string
	AddAssets    []AssetInput
	RemoveAssets []int64
}

var allowedMimes = map[AssetKind][]string{
	KindImage: {"image/png", "image/jpeg", "image/gif", "image/webp"},
	KindClip:  {"video/mp4", "video/webm", "image/gif"},
	KindDemo:  {"text/html"},
}

var mimeExt = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp",
	"video/mp4": ".mp4", "video/webm": ".webm", "text/html": ".html",
}

func extFor(mime string) string { return mimeExt[mime] }

type preparedAsset struct {
	AssetInput
	mime string
}

func prepareAssets(in []AssetInput) ([]preparedAsset, error) {
	out := make([]preparedAsset, 0, len(in))
	for i, a := range in {
		if _, ok := allowedMimes[a.Kind]; !ok {
			return nil, invalid("asset %d: kind must be image, demo or clip", i)
		}
		if len(a.Data) == 0 {
			return nil, invalid("asset %d (%s): empty file", i, a.Kind)
		}
		if a.Role == "" {
			a.Role = "primary"
		}
		mime := sniff(a)
		allowed := false
		for _, m := range allowedMimes[a.Kind] {
			allowed = allowed || m == mime
		}
		if !allowed {
			return nil, invalid("asset %d: a %s cannot be %q (allowed: %s)", i, a.Kind, mime, strings.Join(allowedMimes[a.Kind], ", "))
		}
		out = append(out, preparedAsset{AssetInput: a, mime: mime})
	}
	return out, nil
}

func sniff(a AssetInput) string {
	if a.Kind == KindDemo {
		if utf8.Valid(a.Data) {
			return "text/html"
		}
		return "application/octet-stream"
	}
	if a.Kind == KindClip && len(a.Data) >= 8 && string(a.Data[4:8]) == "ftyp" {
		// Go's sniffing table has no mp4 entry, and mp4 is a legal clip.
		return "video/mp4"
	}
	m := http.DetectContentType(a.Data)
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = m[:i]
	}
	return m
}

func sourceKind(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", invalid("source_url must be an http(s) URL")
	}
	switch strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") {
	case "x.com", "twitter.com", "mobile.twitter.com", "fxtwitter.com":
		return SourceTweet, nil
	}
	return SourceWeb, nil
}

// upsertSource returns the id of the source row for rawURL, creating it when needed.
func upsertSource(ctx context.Context, tx *sql.Tx, rawURL, title string) (int64, error) {
	kind, err := sourceKind(rawURL)
	if err != nil {
		return 0, err
	}
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM sources WHERE url = ?`, rawURL).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO sources(url, kind, title, added_at) VALUES (?,?,?,?)`,
		rawURL, kind, title, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func pruneSource(ctx context.Context, tx *sql.Tx, id int64) error {
	_, err := tx.ExecContext(ctx,
		`DELETE FROM sources WHERE id = ? AND NOT EXISTS (SELECT 1 FROM refs WHERE source_id = ?)`, id, id)
	return err
}

func writeIndex(ctx context.Context, tx *sql.Tx, id int64, title, desc, behavior string, tags []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM ref_tags WHERE ref_id = ?`, id); err != nil {
		return err
	}
	for _, t := range tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO ref_tags(ref_id, tag) VALUES (?,?)`, id, t); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM refs_fts WHERE rowid = ?`, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO refs_fts(rowid, title, description, behavior, tags) VALUES (?,?,?,?,?)`,
		id, title, desc, behavior, strings.Join(tags, " "))
	return err
}

// Add validates and stores a reference with its assets.
func (s *Store) Add(ctx context.Context, in NewReference) (*Reference, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	if in.Title == "" || in.Description == "" {
		return nil, invalid("title and description are required")
	}
	if !in.Type.Valid() {
		return nil, invalid("type must be component, site, animation, identity or resource")
	}
	assets, err := prepareAssets(in.Assets)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var srcID sql.NullInt64
	if in.SourceURL != "" {
		id, err := upsertSource(ctx, tx, in.SourceURL, in.Title)
		if err != nil {
			return nil, err
		}
		srcID = sql.NullInt64{Int64: id, Valid: true}
	}
	tags := normTags(in.Tags)
	res, err := tx.ExecContext(ctx,
		`INSERT INTO refs(type, title, description, behavior, source_id, locator, created_at) VALUES (?,?,?,?,?,?,?)`,
		in.Type, in.Title, in.Description, strings.TrimSpace(in.Behavior), srcID, strings.TrimSpace(in.Locator), time.Now().Unix())
	if err != nil {
		return nil, err
	}
	refID, _ := res.LastInsertId()
	if err := writeIndex(ctx, tx, refID, in.Title, in.Description, strings.TrimSpace(in.Behavior), tags); err != nil {
		return nil, err
	}

	if len(assets) > 0 {
		if err := os.MkdirAll(s.refDir(refID), 0o700); err != nil {
			return nil, err
		}
	}
	fail := func(err error) (*Reference, error) {
		os.RemoveAll(s.refDir(refID))
		return nil, err
	}
	for _, a := range assets {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO assets(ref_id, kind, role, viewport, caption, mime, size) VALUES (?,?,?,?,?,?,?)`,
			refID, a.Kind, a.Role, a.Viewport, a.Caption, a.mime, len(a.Data))
		if err != nil {
			return fail(err)
		}
		aid, _ := res.LastInsertId()
		stored := Asset{ID: aid, RefID: refID, Mime: a.mime}
		if err := os.WriteFile(s.assetPath(stored), a.Data, 0o600); err != nil {
			return fail(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fail(err)
	}
	s.embedRef(ctx, refID)
	return s.Get(ctx, refID)
}

// Update applies the changed fields and assets. The whole call is atomic: a
// rejected asset changes nothing on disk or in the database.
func (s *Store) Update(ctx context.Context, id int64, u Update) (*Reference, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	cur, err := getRef(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if u.Type != nil {
		cur.Type = *u.Type
	}
	if u.Title != nil {
		cur.Title = strings.TrimSpace(*u.Title)
	}
	if u.Description != nil {
		cur.Description = strings.TrimSpace(*u.Description)
	}
	if u.Behavior != nil {
		cur.Behavior = strings.TrimSpace(*u.Behavior)
	}
	if u.Locator != nil {
		cur.Locator = strings.TrimSpace(*u.Locator)
	}
	if u.Tags != nil {
		cur.Tags = normTags(*u.Tags)
	}
	if cur.Title == "" || cur.Description == "" {
		return nil, invalid("title and description are required")
	}
	if !cur.Type.Valid() {
		return nil, invalid("type must be component, site, animation, identity or resource")
	}

	oldSrc := sql.NullInt64{}
	newSrc := sql.NullInt64{}
	if cur.Source != nil {
		oldSrc = sql.NullInt64{Int64: cur.Source.ID, Valid: true}
		newSrc = oldSrc
	}
	if u.SourceURL != nil {
		newSrc = sql.NullInt64{}
		if *u.SourceURL != "" {
			sid, err := upsertSource(ctx, tx, *u.SourceURL, cur.Title)
			if err != nil {
				return nil, err
			}
			newSrc = sql.NullInt64{Int64: sid, Valid: true}
		}
	}

	// Resolve asset changes against the current assets.
	newAssets, err := prepareAssets(u.AddAssets)
	if err != nil {
		return nil, err
	}
	removed := map[int64]Asset{}
	for i, aid := range u.RemoveAssets {
		found := false
		for _, a := range cur.Assets {
			if a.ID == aid {
				removed[aid] = a
				found = true
				break
			}
		}
		if !found {
			return nil, invalid("remove_assets %d: asset %d is not on this reference", i, aid)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE refs SET type=?, title=?, description=?, behavior=?, locator=?, source_id=? WHERE id=?`,
		cur.Type, cur.Title, cur.Description, cur.Behavior, cur.Locator, newSrc, id); err != nil {
		return nil, err
	}
	if err := writeIndex(ctx, tx, id, cur.Title, cur.Description, cur.Behavior, cur.Tags); err != nil {
		return nil, err
	}
	if oldSrc.Valid && oldSrc != newSrc {
		if err := pruneSource(ctx, tx, oldSrc.Int64); err != nil {
			return nil, err
		}
	}
	if len(newAssets) > 0 {
		if err := os.MkdirAll(s.refDir(id), 0o700); err != nil {
			return nil, err
		}
	}
	// Insert the new assets, then delete the removed ones, so a failure can
	// remove only the files this call wrote.
	written := make([]string, 0, len(newAssets))
	for _, a := range newAssets {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO assets(ref_id, kind, role, viewport, caption, mime, size) VALUES (?,?,?,?,?,?,?)`,
			id, a.Kind, a.Role, a.Viewport, a.Caption, a.mime, len(a.Data))
		if err != nil {
			removeFiles(written)
			return nil, err
		}
		aid, _ := res.LastInsertId()
		path := s.assetPath(Asset{ID: aid, RefID: id, Mime: a.mime})
		if err := os.WriteFile(path, a.Data, 0o600); err != nil {
			removeFiles(written)
			return nil, err
		}
		written = append(written, path)
	}
	for _, a := range removed {
		if _, err := tx.ExecContext(ctx, `DELETE FROM assets WHERE id = ? AND ref_id = ?`, a.ID, id); err != nil {
			removeFiles(written)
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		removeFiles(written)
		return nil, err
	}
	for _, a := range removed {
		os.Remove(s.assetPath(a))
	}
	s.embedRef(ctx, id)
	return s.Get(ctx, id)
}

func removeFiles(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}

// Delete removes a reference permanently: its rows, its files, and its source
// when no other reference uses it.
func (s *Store) Delete(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var src sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT source_id FROM refs WHERE id = ?`, id).Scan(&src); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM refs_fts WHERE rowid = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM refs WHERE id = ?`, id); err != nil {
		return err
	}
	if src.Valid {
		if err := pruneSource(ctx, tx, src.Int64); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return os.RemoveAll(s.refDir(id))
}
