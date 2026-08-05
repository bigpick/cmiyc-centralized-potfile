// Package store is the data layer for the pool. It owns the only PostgreSQL
// connection in the system (the server), so the CLI never touches the database
// directly; it goes through the HTTP API instead.
//
// The correctness properties discussed in design all live here:
//   - Submit is idempotent: uploading the same lines any number of times, in
//     any order, whole or fragmented, converges to the same unique set.
//   - Pending / All never mutate state. Only MarkSubmitted does.
//   - MarkSubmitted is SET-EXACT: it marks precisely the ids handed to it and
//     is guarded by "submitted_at IS NULL", so a crack that raced in after a
//     pull's snapshot can never be falsely marked.
package store

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zilla/cmiyc-pool/internal/potfile"
)

//go:embed schema.sql
var schemaSQL string

// chunkSize bounds how many lines go into one INSERT statement. The client
// already de-duplicates the whole potfile before upload, so chunking never
// splits a duplicate across statements.
const chunkSize = 5000

// Store wraps a pgx connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// New connects to PostgreSQL, retries until reachable (Railway has no
// depends_on, so the DB may lag the app on cold start), then applies the schema
// idempotently.
func New(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}

	var pool *pgxpool.Pool
	deadline := time.Now().Add(60 * time.Second)
	for {
		pool, err = pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			if pingErr := pool.Ping(ctx); pingErr == nil {
				break
			} else {
				err = pingErr
				pool.Close()
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("database not reachable within 60s: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

// SubmitResult summarizes one upload.
type SubmitResult struct {
	NewCracks    int64 `json:"new_cracks"`     // lines never seen by the team before
	BatchUnique  int64 `json:"batch_unique"`   // distinct lines in this upload
	NewForAuthor int64 `json:"new_for_author"` // lines this author had not contributed before
	TotalUnique  int64 `json:"total_unique"`   // team-wide unique cracks after this upload
	Pending      int64 `json:"pending"`        // not yet sent to KoreLogic
}

// Submit upserts a batch of cracked lines attributed to author. It records the
// first-cracker on new rows and registers author as a contributor to every
// line in the batch (new or pre-existing), which is what makes post-competition
// overlap analysis possible.
func (s *Store) Submit(ctx context.Context, author string, lines []potfile.Line) (SubmitResult, error) {
	var res SubmitResult
	if author == "" {
		return res, errors.New("author is required")
	}

	for start := 0; start < len(lines); start += chunkSize {
		end := start + chunkSize
		if end > len(lines) {
			end = len(lines)
		}
		chunk := lines[start:end]

		hashes := make([][]byte, len(chunk))
		texts := make([]string, len(chunk))
		for i, l := range chunk {
			h := l.Hash // copy so the slice does not alias the loop variable
			hashes[i] = h[:]
			texts[i] = l.Text
		}

		var newCracks, batchUnique, newForAuthor int64
		err := s.pool.QueryRow(ctx, submitSQL, hashes, texts, author).
			Scan(&newCracks, &batchUnique, &newForAuthor)
		if err != nil {
			return res, fmt.Errorf("submit chunk [%d:%d]: %w", start, end, err)
		}
		res.NewCracks += newCracks
		res.BatchUnique += batchUnique
		res.NewForAuthor += newForAuthor
	}

	if err := s.pool.QueryRow(ctx, totalsSQL).Scan(&res.TotalUnique, &res.Pending); err != nil {
		return res, fmt.Errorf("read totals: %w", err)
	}
	return res, nil
}

const submitSQL = `
WITH incoming(line_hash, line) AS (
    SELECT * FROM unnest($1::bytea[], $2::text[])
),
dedup AS (
    SELECT DISTINCT ON (line_hash) line_hash, line FROM incoming
),
ins AS (
    INSERT INTO cracks (line_hash, line, first_by)
    SELECT line_hash, line, $3 FROM dedup
    ON CONFLICT (line_hash) DO NOTHING
    RETURNING id, line_hash
),
existing AS (
    -- Rows already present before this statement (data-modifying CTEs are not
    -- visible to sibling reads, so new rows are captured via ins above).
    SELECT c.id
    FROM cracks c
    JOIN dedup d ON d.line_hash = c.line_hash
    WHERE c.line_hash NOT IN (SELECT line_hash FROM ins)
),
all_ids AS (
    SELECT id FROM ins
    UNION ALL
    SELECT id FROM existing
),
contrib AS (
    INSERT INTO crack_contributors (crack_id, author)
    SELECT id, $3 FROM all_ids
    ON CONFLICT DO NOTHING
    RETURNING crack_id
)
SELECT
    (SELECT count(*) FROM ins)     AS new_cracks,
    (SELECT count(*) FROM dedup)   AS batch_unique,
    (SELECT count(*) FROM contrib) AS new_for_author
`

const totalsSQL = `
SELECT
    count(*)                                        AS total_unique,
    count(*) FILTER (WHERE submitted_at IS NULL)    AS pending
FROM cracks
`

// CrackLine is one row returned to a pull.
type CrackLine struct {
	ID         int64  `json:"id"`
	Line       string `json:"line"`
	WasPending bool   `json:"was_pending"`
}

// Pending returns every crack not yet sent to KoreLogic. This is what a plain
// pull bundles; every line it returns has WasPending == true.
func (s *Store) Pending(ctx context.Context) ([]CrackLine, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, line FROM cracks WHERE submitted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CrackLine
	for rows.Next() {
		l := CrackLine{WasPending: true}
		if err := rows.Scan(&l.ID, &l.Line); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// All returns every crack, each tagged with whether it was pending at read
// time. This backs `pull --full`: the whole set is re-shipped to KoreLogic, but
// only the was_pending == true subset is eligible to be marked on send.
func (s *Store) All(ctx context.Context) ([]CrackLine, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, line, (submitted_at IS NULL) AS was_pending FROM cracks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CrackLine
	for rows.Next() {
		var l CrackLine
		if err := rows.Scan(&l.ID, &l.Line, &l.WasPending); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// MarkSubmitted stamps submitted_at on exactly the given ids, but only where
// still pending. Returns how many rows actually transitioned. Ids that were
// already submitted, or that never existed, are silently ignored, which is what
// makes re-submitting a superset harmless.
func (s *Store) MarkSubmitted(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE cracks SET submitted_at = now()
		 WHERE id = ANY($1::bigint[]) AND submitted_at IS NULL`, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Stats is the internal, post-competition analytics surface.
type Stats struct {
	TotalUnique     int64            `json:"total_unique"`
	Submitted       int64            `json:"submitted"`
	Pending         int64            `json:"pending"`
	OverlapCracks   int64            `json:"overlap_cracks"`   // cracked independently by >1 teammate
	FirstByAuthor   map[string]int64 `json:"first_by_author"`  // race-winning first cracks per teammate
	ContribByAuthor map[string]int64 `json:"contrib_by_author"` // total cracks each teammate has ever held
}

// Stats computes team distribution numbers in a handful of cheap aggregates.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var st Stats
	st.FirstByAuthor = map[string]int64{}
	st.ContribByAuthor = map[string]int64{}

	err := s.pool.QueryRow(ctx, `
		SELECT
		    count(*),
		    count(*) FILTER (WHERE submitted_at IS NOT NULL),
		    count(*) FILTER (WHERE submitted_at IS NULL)
		FROM cracks`).Scan(&st.TotalUnique, &st.Submitted, &st.Pending)
	if err != nil {
		return st, err
	}

	err = s.pool.QueryRow(ctx, `
		SELECT count(*) FROM (
		    SELECT crack_id FROM crack_contributors
		    GROUP BY crack_id HAVING count(*) > 1
		) t`).Scan(&st.OverlapCracks)
	if err != nil {
		return st, err
	}

	if err := scanCounts(ctx, s.pool,
		`SELECT first_by, count(*) FROM cracks GROUP BY first_by`, st.FirstByAuthor); err != nil {
		return st, err
	}
	if err := scanCounts(ctx, s.pool,
		`SELECT author, count(*) FROM crack_contributors GROUP BY author`, st.ContribByAuthor); err != nil {
		return st, err
	}
	return st, nil
}

func scanCounts(ctx context.Context, pool *pgxpool.Pool, sql string, dst map[string]int64) error {
	rows, err := pool.Query(ctx, sql)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var v int64
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		dst[k] = v
	}
	return rows.Err()
}
