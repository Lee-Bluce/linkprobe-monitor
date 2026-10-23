package store

import (
	"context"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func seed(t *testing.T, st *Store, nodeID int64, start int64, rounds int) []Sample {
	t.Helper()
	ctx := context.Background()
	batch := make([]Sample, 0, rounds)
	for i := 0; i < rounds; i++ {
		ts := start + int64(i)*30
		sent := 5
		recv := 5
		if i%7 == 3 {
			recv = 0
		} else if i%11 == 5 {
			recv = 3
		}
		minUs := int64(1000 + i)
		maxUs := int64(4000 + i*2)
		avgUs := int64(2000 + i)
		sm := Sample{NodeID: nodeID, Ts: ts, Sent: sent, Recv: recv}
		if recv > 0 {
			sm.RttMinUs, sm.RttAvgUs, sm.RttMaxUs = minUs, avgUs, maxUs
		}
		batch = append(batch, sm)
	}
	if err := st.InsertSamples(ctx, batch); err != nil {
		t.Fatalf("insert: %v", err)
	}
	return batch
}

func TestAggregationMatchesRaw(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	start := AlignDown(1_700_000_000, 3600)
	seed(t, st, 1, start, 6*120) // six hours of 30s rounds

	from := start
	to := start + 6*3600
	if _, err := st.Agg5m(ctx, from, to); err != nil {
		t.Fatalf("agg5m: %v", err)
	}
	if _, err := st.Agg1h(ctx, from, to); err != nil {
		t.Fatalf("agg1h: %v", err)
	}

	raw, err := st.Series(ctx, 1, from, to, 300, LayerRaw)
	if err != nil {
		t.Fatal(err)
	}
	roll, err := st.Series(ctx, 1, from, to, 300, Layer5m)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != len(roll) || len(raw) != 72 {
		t.Fatalf("bucket count raw=%d roll=%d want 72", len(raw), len(roll))
	}
	for i := range raw {
		if raw[i] != roll[i] {
			t.Fatalf("bucket %d differs\nraw=%+v\n5m =%+v", i, raw[i], roll[i])
		}
	}

	hourRaw, err := st.Series(ctx, 1, from, to, 3600, LayerRaw)
	if err != nil {
		t.Fatal(err)
	}
	hour1h, err := st.Series(ctx, 1, from, to, 3600, Layer1h)
	if err != nil {
		t.Fatal(err)
	}
	if len(hourRaw) != 6 || len(hour1h) != 6 {
		t.Fatalf("hourly buckets raw=%d 1h=%d", len(hourRaw), len(hour1h))
	}
	for i := range hourRaw {
		if hourRaw[i] != hour1h[i] {
			t.Fatalf("hour %d differs\nraw=%+v\n1h =%+v", i, hourRaw[i], hour1h[i])
		}
	}
}

func TestAggregationIsIdempotent(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	start := AlignDown(1_700_000_000, 3600)
	seed(t, st, 7, start, 360)
	if _, err := st.Agg5m(ctx, start, start+3600); err != nil {
		t.Fatal(err)
	}
	first, err := st.Series(ctx, 7, start, start+3600, 300, Layer5m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Agg5m(ctx, start, start+3600); err != nil {
		t.Fatal(err)
	}
	second, err := st.Series(ctx, 7, start, start+3600, 300, Layer5m)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 12 || len(second) != 12 {
		t.Fatalf("unexpected bucket count %d/%d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("not idempotent at %d: %+v vs %+v", i, first[i], second[i])
		}
	}
}

func TestUpsertOverwritesPartialBucket(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	start := AlignDown(1_700_000_000, 300)
	seed(t, st, 3, start, 4) // 2 minutes inside one 5m bucket
	if _, err := st.Agg5m(ctx, start, start+300); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Series(ctx, 3, start, start+300, 300, Layer5m)
	if len(before) != 1 || before[0].Sent != 20 {
		t.Fatalf("unexpected first aggregation: %+v", before)
	}
	seed(t, st, 3, start+120, 4) // more rounds, same bucket
	if _, err := st.Agg5m(ctx, start, start+300); err != nil {
		t.Fatal(err)
	}
	after, _ := st.Series(ctx, 3, start, start+300, 300, Layer5m)
	if len(after) != 1 || after[0].Sent != 40 {
		t.Fatalf("bucket was not recomputed: %+v", after)
	}
}

func TestTrimBefore(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	start := AlignDown(1_700_000_000, 3600)
	seed(t, st, 1, start, 240) // 2 hours
	cutoff := start + 3600
	if _, err := st.TrimBefore(ctx, "samples_raw", cutoff, 600); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := st.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	n, err := st.CountSamples(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 120 {
		t.Fatalf("want 120 rows after trim, got %d", n)
	}
}

func TestDeleteNodeRemovesSamples(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	n, err := st.CreateNode(ctx, Node{
		Name: "tmp", Type: "icmp", Host: "127.0.0.1",
		IntervalS: 30, Count: 5, TimeoutMs: 2000, Family: "auto", Enabled: true,
	}, 100)
	if err != nil {
		t.Fatal(err)
	}
	start := AlignDown(1_700_000_000, 300)
	seed(t, st, n.ID, start, 20)
	if err := st.DeleteNode(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	c, err := st.CountSamples(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c != 0 {
		t.Fatalf("samples left behind: %d", c)
	}
}

func TestGroupsAndNodes(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	g, err := st.CreateGroup(ctx, "机房", 100)
	if err != nil {
		t.Fatal(err)
	}
	n, err := st.CreateNode(ctx, Node{
		GroupID: &g.ID, Name: "gw", Type: "icmp", Host: "192.168.10.1",
		IntervalS: 30, Count: 5, TimeoutMs: 2000, Family: "auto", Enabled: true,
	}, 100)
	if err != nil {
		t.Fatal(err)
	}
	id := g.ID
	updated, err := st.UpdateNode(ctx, n.ID, func(x *Node) error {
		x.GroupID = nil
		x.Name = "gateway"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GroupID != nil || updated.Name != "gateway" {
		t.Fatalf("update not applied: %+v", updated)
	}
	if err := st.DeleteGroup(ctx, id); err != nil {
		t.Fatal(err)
	}
	list, err := st.Nodes(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("nodes: %v %v", list, err)
	}
}
