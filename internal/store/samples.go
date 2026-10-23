package store

import (
	"context"
	"database/sql"
)

// Sample is one probe round.
type Sample struct {
	NodeID   int64
	Ts       int64
	Sent     int
	Recv     int
	RttMinUs int64
	RttAvgUs int64
	RttMaxUs int64
}

// InsertSamples writes a batch in one transaction. Duplicate (node_id, ts)
// rows are replaced so a retry or a clock correction cannot double count.
func (s *Store) InsertSamples(ctx context.Context, batch []Sample) error {
	if len(batch) == 0 {
		return nil
	}
	return s.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO samples_raw
(node_id, ts, sent, recv, rtt_min_us, rtt_avg_us, rtt_max_us)
VALUES(?,?,?,?,?,?,?)
ON CONFLICT(node_id, ts) DO UPDATE SET
  sent=excluded.sent, recv=excluded.recv,
  rtt_min_us=excluded.rtt_min_us, rtt_avg_us=excluded.rtt_avg_us, rtt_max_us=excluded.rtt_max_us`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, sp := range batch {
			if _, err := stmt.ExecContext(ctx, sp.NodeID, sp.Ts, sp.Sent, sp.Recv,
				sp.RttMinUs, sp.RttAvgUs, sp.RttMaxUs); err != nil {
				return err
			}
		}
		return nil
	})
}

// Point is an aggregated bucket returned by the series query.
type Point struct {
	Ts       int64 `json:"ts"`
	Sent     int64 `json:"sent"`
	Recv     int64 `json:"recv"`
	RttMinUs int64 `json:"rtt_min_us"`
	RttMaxUs int64 `json:"rtt_max_us"`
	RttSumUs int64 `json:"rtt_sum_us"`
}

const (
	LayerRaw = "raw"
	Layer5m  = "5m"
	Layer1h  = "1h"
)

// Series returns buckets of width res for [from, to), aggregated from the
// requested layer. Buckets without samples are simply absent; callers fill
// them with nulls so "no data" stays distinguishable from "100% loss".
func (s *Store) Series(ctx context.Context, nodeID, from, to, res int64, layer string) ([]Point, error) {
	if res <= 0 {
		res = 60
	}
	var table, minExpr, maxExpr, sumExpr string
	switch layer {
	case Layer5m, Layer1h:
		table = "rollup_" + layer
		minExpr = "MIN(CASE WHEN recv>0 THEN rtt_min_us END)"
		maxExpr = "MAX(CASE WHEN recv>0 THEN rtt_max_us END)"
		sumExpr = "SUM(rtt_sum_us)"
	default:
		table = "samples_raw"
		minExpr = "MIN(CASE WHEN recv>0 THEN rtt_min_us END)"
		maxExpr = "MAX(CASE WHEN recv>0 THEN rtt_max_us END)"
		sumExpr = "SUM(rtt_avg_us*recv)"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT (ts/?)*? AS bucket, SUM(sent), SUM(recv),
COALESCE(`+minExpr+`,0), COALESCE(`+maxExpr+`,0), COALESCE(`+sumExpr+`,0)
FROM `+table+` WHERE node_id=? AND ts>=? AND ts<?
GROUP BY bucket ORDER BY bucket`, res, res, nodeID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Point{}
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.Ts, &p.Sent, &p.Recv, &p.RttMinUs, &p.RttMaxUs, &p.RttSumUs); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RecentSamples returns the newest rounds for one node, newest first.
func (s *Store) RecentSamples(ctx context.Context, nodeID int64, limit int) ([]Sample, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id, ts, sent, recv, rtt_min_us, rtt_avg_us, rtt_max_us
FROM samples_raw WHERE node_id=? ORDER BY ts DESC LIMIT ?`, nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Sample, 0, limit)
	for rows.Next() {
		var sp Sample
		if err := rows.Scan(&sp.NodeID, &sp.Ts, &sp.Sent, &sp.Recv, &sp.RttMinUs, &sp.RttAvgUs, &sp.RttMaxUs); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// AvgRTT returns the receive-weighted mean RTT (microseconds) over
// [from, to). ok is false when no packet was received in that window.
func (s *Store) AvgRTT(ctx context.Context, nodeID, from, to int64) (avgUs float64, ok bool, err error) {
	var sum, recv sql.NullInt64
	err = s.db.QueryRowContext(ctx, `SELECT SUM(rtt_sum_us), SUM(recv) FROM rollup_5m
WHERE node_id=? AND ts>=? AND ts<?`, nodeID, from, to).Scan(&sum, &recv)
	if err != nil {
		return 0, false, err
	}
	if !recv.Valid || recv.Int64 == 0 {
		return 0, false, nil
	}
	return float64(sum.Int64) / float64(recv.Int64), true, nil
}

// CountSamples returns how many raw rows exist for a node.
func (s *Store) CountSamples(ctx context.Context, nodeID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM samples_raw WHERE node_id=?`, nodeID).Scan(&n)
	return n, err
}
