// Package pipeline batches probe results into the database and keeps the
// rollup tables current.
package pipeline

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"linkprobe/internal/store"
)

// Writer drains probe results and commits them in batches.
type Writer struct {
	store   *store.Store
	in      <-chan store.Sample
	period  time.Duration
	log     *slog.Logger
	pending atomic.Int64
	written atomic.Int64
	lastTS  atomic.Int64
	errs    atomic.Int64
}

func NewWriter(st *store.Store, in <-chan store.Sample, period time.Duration, log *slog.Logger) *Writer {
	if period <= 0 {
		period = 30 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	return &Writer{store: st, in: in, period: period, log: log}
}

func (w *Writer) Run(ctx context.Context) {
	ticker := time.NewTicker(w.period)
	defer ticker.Stop()
	buf := make([]store.Sample, 0, 1024)
	for {
		select {
		case sp := <-w.in:
			buf = append(buf, sp)
			w.pending.Add(1)
			if len(buf) >= 4096 {
				w.flush(&buf)
			}
		case <-ticker.C:
			w.flush(&buf)
		case <-ctx.Done():
			// Last chance: take whatever is already queued, then one flush.
			for {
				select {
				case sp := <-w.in:
					buf = append(buf, sp)
					continue
				default:
				}
				break
			}
			w.flush(&buf)
			return
		}
	}
}

func (w *Writer) flush(buf *[]store.Sample) {
	if len(*buf) == 0 {
		return
	}
	batch := *buf
	*buf = (*buf)[:0]
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	start := time.Now()
	if err := w.store.InsertSamples(ctx, batch); err != nil {
		w.errs.Add(1)
		w.log.Error("write samples failed", "rows", len(batch), "err", err)
		return
	}
	w.pending.Add(-int64(len(batch)))
	w.written.Add(int64(len(batch)))
	w.lastTS.Store(time.Now().Unix())
	w.log.Debug("samples written", "rows", len(batch), "took", time.Since(start))
}

type WriterStats struct {
	Queued  int64 `json:"queued"`
	Written int64 `json:"written"`
	LastTS  int64 `json:"last_write_ts"`
	Errors  int64 `json:"errors"`
}

func (w *Writer) Stats() WriterStats {
	return WriterStats{
		Queued:  w.pending.Load(),
		Written: w.written.Load(),
		LastTS:  w.lastTS.Load(),
		Errors:  w.errs.Load(),
	}
}

// Aggregator maintains rollup_5m and rollup_1h.
type Aggregator struct {
	store *store.Store
	raw   time.Duration
	r5m   time.Duration
	log   *slog.Logger
}

func NewAggregator(st *store.Store, rawRetention, rollup5mRetention time.Duration, log *slog.Logger) *Aggregator {
	if log == nil {
		log = slog.Default()
	}
	return &Aggregator{store: st, raw: rawRetention, r5m: rollup5mRetention, log: log}
}

// Backfill closes gaps left by downtime. It is safe to run repeatedly.
func (a *Aggregator) Backfill(ctx context.Context) error {
	now := time.Now().Unix()
	rawFrom := now - int64(a.raw.Seconds())
	if rawFrom < 0 {
		rawFrom = 0
	}
	start := rawFrom
	if last, ok, err := a.store.MaxBucketTS(ctx, "rollup_5m"); err != nil {
		return err
	} else if ok && last > start {
		start = last
	}
	start = store.AlignDown(start, 300)
	for cur := start; cur < now; cur += 6 * 3600 {
		end := cur + 6*3600
		if end > now+300 {
			end = now + 300
		}
		if _, err := a.store.Agg5m(ctx, cur, end); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	a.log.Info("rollup 5m backfill done", "from", start, "to", now)

	start1h := now - int64(a.r5m.Seconds())
	if start1h < 0 {
		start1h = 0
	}
	if last, ok, err := a.store.MaxBucketTS(ctx, "rollup_1h"); err != nil {
		return err
	} else if ok && last > start1h {
		start1h = last
	}
	start1h = store.AlignDown(start1h, 3600)
	for cur := start1h; cur < now; cur += 24 * 3600 {
		end := cur + 24*3600
		if end > now+3600 {
			end = now + 3600
		}
		if _, err := a.store.Agg1h(ctx, cur, end); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	a.log.Info("rollup 1h backfill done", "from", start1h, "to", now)
	return nil
}

// Run keeps the newest buckets fresh, including the current open bucket.
func (a *Aggregator) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.refresh(ctx)
		}
	}
}

func (a *Aggregator) refresh(ctx context.Context) {
	now := time.Now().Unix()
	from5m := store.AlignDown(now-3*3600, 300)
	to5m := store.AlignDown(now, 300) + 300
	if n, err := a.store.Agg5m(ctx, from5m, to5m); err != nil {
		a.log.Error("rollup 5m failed", "err", err)
	} else if n > 0 {
		a.log.Debug("rollup 5m refreshed", "buckets", n)
	}
	from1h := store.AlignDown(now-3*3600, 3600)
	to1h := store.AlignDown(now, 3600) + 3600
	if n, err := a.store.Agg1h(ctx, from1h, to1h); err != nil {
		a.log.Error("rollup 1h failed", "err", err)
	} else if n > 0 {
		a.log.Debug("rollup 1h refreshed", "buckets", n)
	}
}
