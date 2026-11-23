package store

import (
	"context"
	"log"
	"sort"
	"strings"
)

// SearchQuery is one keyword search. Q must not be empty; Type and Tags are
// optional filters (all tags must match).
type SearchQuery struct {
	Q      string
	Type   Type
	Tags   []string
	Limit  int
	Offset int
}

// Hit is one ranked search result.
type Hit struct {
	Ref   *Reference
	Score float64
}

// ListQuery lists references newest first without search text.
type ListQuery struct {
	Type   Type
	Tags   []string
	Limit  int
	Offset int
}

const (
	// searchCap bounds how many candidates each ranking method contributes.
	searchCap = 500
	// rrfK is the reciprocal-rank-fusion constant.
	rrfK = 60.0
)

// stopwords are the English function words dropped from keyword queries:
// they carry no signal for ranking, and none of them collides with the tag
// vocabulary (which contains real words like "form", "type" or "text").
var stopwords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true,
	"of": true, "to": true, "in": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "it": true,
	"its": true, "this": true, "that": true, "these": true, "those": true,
	"i": true, "you": true, "your": true, "he": true, "she": true,
	"we": true, "they": true, "as": true, "at": true, "by": true,
	"for": true, "on": true, "with": true, "from": true, "into": true,
	"like": true, "something": true, "when": true, "where": true, "which": true,
	"who": true, "whose": true, "what": true, "how": true, "why": true,
	"do": true, "does": true, "did": true, "can": true, "could": true,
	"should": true, "would": true, "will": true, "shall": true, "may": true,
	"might": true, "must": true, "has": true, "had": true, "not": true,
	"so": true, "than": true, "then": true, "there": true, "here": true,
	"between": true, "off": true, "over": true, "under": true, "up": true,
}

// ftsTerms tokenizes a free-text query: lower-cased words without the
// stop-word noise.
func ftsTerms(q string) []string {
	var out []string
	for _, f := range strings.Fields(strings.ToLower(q)) {
		f = strings.Trim(f, ",.!?;:()[]'`")
		if f == "" || stopwords[f] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// ftsQuery turns free text into a safe FTS5 query: each remaining word is a
// quoted phrase, OR-ed so that sentences with no exact word overlap still
// rank; bm25 puts references that match more (and rarer, better-weighted)
// words first. The table's porter tokenizer makes peels/peel line up.
func ftsQuery(q string) string {
	terms := ftsTerms(q)
	parts := make([]string, 0, len(terms))
	for _, t := range terms {
		parts = append(parts, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	return strings.Join(parts, " OR ")
}

// Search ranks references by FTS5 and, when an embedder is configured, by
// cosine similarity, fused with reciprocal rank fusion. It returns one page
// of hits and the total number of matching references.
func (s *Store) Search(ctx context.Context, sq SearchQuery) ([]Hit, int, error) {
	match := ftsQuery(sq.Q)
	if match == "" {
		return nil, 0, invalid("search query is empty")
	}
	if sq.Limit <= 0 {
		sq.Limit = 10
	}
	tags := normTags(sq.Tags)

	refs := map[int64]*Reference{}
	load := func(ids []int64) error {
		for _, id := range ids {
			if _, ok := refs[id]; ok {
				continue
			}
			r, err := getRef(ctx, s.db, id)
			if err != nil {
				return err
			}
			refs[id] = r
		}
		return nil
	}

	ftsRanked, err := s.rankedFTS(ctx, sq.Type, match)
	if err != nil {
		return nil, 0, err
	}
	if err := load(ftsRanked); err != nil {
		return nil, 0, err
	}
	ftsRanked = filterTags(ftsRanked, refs, tags)

	semRanked, err := s.rankedSemantic(ctx, sq.Type, sq.Q)
	if err != nil {
		return nil, 0, err
	}
	if semRanked != nil {
		if err := load(semRanked); err != nil {
			return nil, 0, err
		}
		semRanked = filterTags(semRanked, refs, tags)
	}

	// Reciprocal rank fusion over the contributing lists.
	scores := map[int64]float64{}
	fuse := func(ids []int64) {
		for i, id := range ids {
			scores[id] += 1 / (rrfK + float64(i+1))
		}
	}
	fuse(ftsRanked)
	fuse(semRanked)

	ids := make([]int64, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if scores[ids[i]] != scores[ids[j]] {
			return scores[ids[i]] > scores[ids[j]]
		}
		return ids[i] < ids[j]
	})

	total := len(ids)
	start := min(sq.Offset, total)
	end := min(start+sq.Limit, total)
	hits := make([]Hit, 0, end-start)
	for _, id := range ids[start:end] {
		hits = append(hits, Hit{Ref: refs[id], Score: scores[id]})
	}
	return hits, total, nil
}

// rankedFTS returns the ids matching the FTS5 query, best first, capped at
// searchCap. Type is applied in SQL; tags are filtered by the caller.
func (s *Store) rankedFTS(ctx context.Context, t Type, match string) ([]int64, error) {
	q := `SELECT f.rowid FROM refs_fts f JOIN refs r ON r.id = f.rowid WHERE refs_fts MATCH ?`
	args := []any{match}
	if t != "" {
		q += ` AND r.type = ?`
		args = append(args, t)
	}
	q += ` ORDER BY bm25(refs_fts, 8.0, 2.0, 2.0, 4.0) LIMIT ?`
	args = append(args, searchCap)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, closeRows(rows)
}

// rankedSemantic returns the ids with embeddings, most cosine-similar to the
// query first, capped at searchCap. It returns (nil, nil) when no embedder is
// configured, and also when the endpoint fails: keyword search must still
// answer. nil therefore means "no semantic list"; an empty list means the
// embedder answered but nothing matched.
func (s *Store) rankedSemantic(ctx context.Context, t Type, query string) ([]int64, error) {
	if s.emb == nil {
		return nil, nil
	}
	vecs, err := s.emb.Embed(ctx, []string{query})
	if err != nil {
		log.Printf("embed query: %v (keyword-only results)", err)
		return nil, nil
	}
	qvec := vecs[0]

	q := `SELECT id, embedding FROM refs WHERE embedding IS NOT NULL`
	args := []any{}
	if t != "" {
		q += ` AND type = ?`
		args = append(args, t)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	type scored struct {
		id int64
		s  float64
	}
	var list []scored
	for rows.Next() {
		var id int64
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, scored{id, cosine(qvec, decodeVec(blob))})
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].s != list[j].s {
			return list[i].s > list[j].s
		}
		return list[i].id < list[j].id
	})
	ids := make([]int64, 0, len(list))
	for _, x := range list {
		ids = append(ids, x.id)
	}
	if len(ids) > searchCap {
		ids = ids[:searchCap]
	}
	return ids, nil
}

// filterTags keeps the ids whose reference carries all the given tags,
// preserving order.
func filterTags(ids []int64, refs map[int64]*Reference, tags []string) []int64 {
	if len(tags) == 0 {
		return ids
	}
	out := ids[:0]
	for _, id := range ids {
		have := map[string]bool{}
		for _, t := range refs[id].Tags {
			have[t] = true
		}
		ok := true
		for _, t := range tags {
			if !have[t] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, id)
		}
	}
	return out
}

// List returns references newest first and the total count, with optional
// type and tag filters (all tags must match).
func (s *Store) List(ctx context.Context, lq ListQuery) ([]*Reference, int, error) {
	if lq.Limit <= 0 {
		lq.Limit = 60
	}
	tags := normTags(lq.Tags)

	where, args := filterSQL(lq.Type, tags)
	total, err := s.countRefs(ctx, where, args)
	if err != nil {
		return nil, 0, err
	}
	q := `SELECT r.id FROM refs r ` + where + ` ORDER BY r.created_at DESC, r.id DESC LIMIT ? OFFSET ?`
	args = append(args, lq.Limit, max(lq.Offset, 0))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	if err := closeRows(rows); err != nil {
		return nil, 0, err
	}
	out := make([]*Reference, 0, len(ids))
	for _, id := range ids {
		r, err := getRef(ctx, s.db, id)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, nil
}

// filterSQL builds the WHERE clause (with leading "WHERE", or "" for no
// filters) shared by List and countRefs.
func filterSQL(t Type, tags []string) (string, []any) {
	var conds []string
	var args []any
	if t != "" {
		conds = append(conds, `r.type = ?`)
		args = append(args, t)
	}
	if len(tags) > 0 {
		ph := make([]string, len(tags))
		for i, tag := range tags {
			ph[i] = "?"
			args = append(args, tag)
		}
		conds = append(conds,
			`(SELECT COUNT(*) FROM ref_tags t WHERE t.ref_id = r.id AND t.tag IN (`+strings.Join(ph, ",")+`)) = ?`)
		args = append(args, len(tags))
	}
	if len(conds) == 0 {
		return "", nil
	}
	return ` WHERE ` + strings.Join(conds, " AND "), args
}

func (s *Store) countRefs(ctx context.Context, where string, args []any) (int, error) {
	var total int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refs r`+where, args...).Scan(&total)
	return total, err
}
