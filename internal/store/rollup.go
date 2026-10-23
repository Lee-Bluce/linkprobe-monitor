package store

import (
	"context"
	"database/sql"
)

// AlignDown returns the start of the bucket that contains ts.
func AlignDown(ts, width int64) int64 {
	if width <= 0 {
		return ts
	}
	return ts - ts%width
}

// Agg5m recomputes 5 minute buckets from raw samples for [from, to).
// from and to must be bucket aligned; the caller is responsible for that so a
// partially covered bucket is never written.
func (s *Store) Agg5m(ctx context.Context, from, to int64) (int64, error) {
	if to <= from {
		return 0, nil
	}
	return s.aggregate(ctx, "rollup_5m", "samples_raw", "SUM(rtt_avg_us*recv)", 300, from, to)
}

// Agg1h recomputes 1 hour buckets from the 5 minute rollup for [from, to).
func (s *Store) Agg1h(ctx context.Context, from, to int64) (int64, error) {
	if to <= from {
		return 0, nil
	}
	return s.aggregate(ctx, "rollup_1h", "rollup_5m", "SUM(rtt_sum_us)", 3600, from, to)
}

func (s *Store) aggregate(ctx context.Context, dst, src, sumExpr string, width, from, to int64) (int64, error) {
	var n int64
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO `+dst+`
(node_id, ts, sent, recv, rtt_sum_us, rtt_min_us, rtt_max_us)
SELECT node_id, (ts/?)*?, SUM(sent), SUM(recv), `+sumExpr+`,
  COALESCE(MIN(CASE WHEN recv>0 THEN rtt_min_us END),0),
  COALESCE(MAX(CASE WHEN recv>0 THEN rtt_max_us END),0)
FROM `+src+` WHERE ts>=? AND ts<?
GROUP BY node_id, (ts/?)*?
ON CONFLICT(node_id, ts) DO UPDATE SET
  sent=excluded.sent, recv=excluded.recv, rtt_sum_us=excluded.rtt_sum_us,
  rtt_min_us=excluded.rtt_min_us, rtt_max_us=excluded.rtt_max_us`,
			width, width, from, to, width, width)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n, err
}

// MaxBucketTS returns the newest bucket timestamp in a rollup table.
func (s *Store) MaxBucketTS(ctx context.Context, table string) (int64, bool, error) {
	var ts sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(ts) FROM `+table).Scan(&ts); err != nil {
		return 0, false, err
	}
	return ts.Int64, ts.Valid, nil
}

// TrimBefore deletes rows older than cutoff in small timestamp windows so no
// single transaction grows with the amount of history.
func (s *Store) TrimBefore(ctx context.Context, table string, cutoff, chunk int64) (int64, error) {
	if cutoff <= 0 || chunk <= 0 {
		return 0, nil
	}
	var oldest sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(ts) FROM `+table).Scan(&oldest); err != nil {
		return 0, err
	}
	if !oldest.Valid || oldest.Int64 >= cutoff {
		return 0, nil
	}
	var total int64
	cursor := AlignDown(oldest.Int64, chunk)
	for cursor < cutoff {
		hi := cursor + chunk
		if hi > cutoff {
			hi = cutoff
		}
		res, err := s.db.ExecContext(ctx, `DELETE FROM `+table+` WHERE ts>=? AND ts<?`, cursor, hi)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		cursor = hi
		if n == 0 {
			// Skip empty gaps quickly: jump forward one retention-sized step
			// only when the window was completely empty.
			continue
		}
	}
	return total, nil
}
