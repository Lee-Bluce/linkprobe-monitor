package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type Group struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Sort      int    `json:"sort"`
	CreatedAt int64  `json:"created_at"`
}

type Node struct {
	ID        int64  `json:"id"`
	GroupID   *int64 `json:"group_id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	IntervalS int    `json:"interval_s"`
	Count     int    `json:"count"`
	TimeoutMs int    `json:"timeout_ms"`
	Family    string `json:"family"`
	Enabled   bool   `json:"enabled"`
	Sort      int    `json:"sort"`
	Note      string `json:"note"`
	CreatedAt int64  `json:"created_at"`
}

func scanGroup(sc interface{ Scan(...any) error }) (Group, error) {
	var g Group
	err := sc.Scan(&g.ID, &g.Name, &g.Sort, &g.CreatedAt)
	return g, err
}

func (s *Store) Groups(ctx context.Context) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, sort, created_at FROM groups ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) CreateGroup(ctx context.Context, name string, now int64) (Group, error) {
	var maxSort int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort), -1) FROM groups`).Scan(&maxSort); err != nil {
		return Group{}, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO groups(name, sort, created_at) VALUES(?, ?, ?)`, name, maxSort+1, now)
	if err != nil {
		return Group{}, err
	}
	id, _ := res.LastInsertId()
	return Group{ID: id, Name: name, Sort: maxSort + 1, CreatedAt: now}, nil
}

func (s *Store) RenameGroup(ctx context.Context, id int64, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE groups SET name=? WHERE id=?`, name, id)
	if err != nil {
		return err
	}
	return affected(res)
}

// DeleteGroup removes the group; its nodes become ungrouped (group_id NULL).
func (s *Store) DeleteGroup(ctx context.Context, id int64) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE nodes SET group_id=NULL WHERE group_id=?`, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM groups WHERE id=?`, id)
		if err != nil {
			return err
		}
		return affected(res)
	})
}

func (s *Store) ReorderGroups(ctx context.Context, ids []int64) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE groups SET sort=? WHERE id=?`, i, id); err != nil {
				return err
			}
		}
		return nil
	})
}

const nodeCols = `id, group_id, name, type, host, port, interval_s, count, timeout_ms, family, enabled, sort, note, created_at`

func scanNode(sc interface{ Scan(...any) error }) (Node, error) {
	var n Node
	var groupID sql.NullInt64
	var enabled int
	err := sc.Scan(&n.ID, &groupID, &n.Name, &n.Type, &n.Host, &n.Port, &n.IntervalS,
		&n.Count, &n.TimeoutMs, &n.Family, &enabled, &n.Sort, &n.Note, &n.CreatedAt)
	if groupID.Valid {
		v := groupID.Int64
		n.GroupID = &v
	}
	n.Enabled = enabled != 0
	return n, err
}

func (s *Store) Nodes(ctx context.Context) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+nodeCols+` FROM nodes ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) Node(ctx context.Context, id int64) (Node, error) {
	n, err := scanNode(s.db.QueryRowContext(ctx, `SELECT `+nodeCols+` FROM nodes WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	return n, err
}

func (s *Store) CreateNode(ctx context.Context, n Node, now int64) (Node, error) {
	var maxSort int
	q := `SELECT COALESCE(MAX(sort), -1) FROM nodes`
	args := []any{}
	if n.GroupID != nil {
		q += ` WHERE group_id=?`
		args = append(args, *n.GroupID)
	} else {
		q += ` WHERE group_id IS NULL`
	}
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&maxSort); err != nil {
		return Node{}, err
	}
	enabled := 0
	if n.Enabled {
		enabled = 1
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO nodes
(group_id, name, type, host, port, interval_s, count, timeout_ms, family, enabled, sort, note, created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.GroupID, n.Name, n.Type, n.Host, n.Port, n.IntervalS, n.Count, n.TimeoutMs,
		n.Family, enabled, maxSort+1, n.Note, now)
	if err != nil {
		return Node{}, err
	}
	id, _ := res.LastInsertId()
	return s.Node(ctx, id)
}

// UpdateNode applies mutate to the current row and writes every field back.
func (s *Store) UpdateNode(ctx context.Context, id int64, mutate func(*Node) error) (Node, error) {
	n, err := s.Node(ctx, id)
	if err != nil {
		return n, err
	}
	if err := mutate(&n); err != nil {
		return n, err
	}
	enabled := 0
	if n.Enabled {
		enabled = 1
	}
	_, err = s.db.ExecContext(ctx, `UPDATE nodes SET group_id=?, name=?, type=?, host=?, port=?,
interval_s=?, count=?, timeout_ms=?, family=?, enabled=?, sort=?, note=? WHERE id=?`,
		n.GroupID, n.Name, n.Type, n.Host, n.Port, n.IntervalS, n.Count, n.TimeoutMs,
		n.Family, enabled, n.Sort, n.Note, id)
	if err != nil {
		return n, err
	}
	return s.Node(ctx, id)
}

func (s *Store) DeleteNode(ctx context.Context, id int64) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		for _, table := range []string{"samples_raw", "rollup_5m", "rollup_1h"} {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE node_id=?`, id); err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM nodes WHERE id=?`, id)
		if err != nil {
			return err
		}
		return affected(res)
	})
}

type NodeOrderItem struct {
	ID      int64  `json:"id"`
	GroupID *int64 `json:"group_id"`
	Sort    int    `json:"sort"`
}

// ReorderNodes moves nodes between groups and rewrites their sort keys.
func (s *Store) ReorderNodes(ctx context.Context, items []NodeOrderItem) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		var count int
		for _, it := range items {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM nodes WHERE id=?`, it.ID).Scan(&exists); err != nil {
				return err
			}
			if exists == 0 {
				return fmt.Errorf("node %d: %w", it.ID, ErrNotFound)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE nodes SET group_id=?, sort=? WHERE id=?`,
				it.GroupID, it.Sort, it.ID); err != nil {
				return err
			}
			count++
		}
		_ = count
		return nil
	})
}

func affected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
