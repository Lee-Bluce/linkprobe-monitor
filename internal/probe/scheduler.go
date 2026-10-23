package probe

import (
	"context"
	"hash/fnv"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"linkprobe/internal/store"
)

// Scheduler keeps one goroutine per enabled node, staggered by node id so the
// whole fleet does not fire at the same instant.
type Scheduler struct {
	engine  *Engine
	out     chan<- store.Sample
	log     *slog.Logger
	sem     chan struct{}
	gap     time.Duration
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	runners map[int64]*runner

	rounds     atomic.Int64
	lastRound  atomic.Int64
	lastMillis atomic.Int64
	inFlight   atomic.Int64
}

type runner struct {
	node   store.Node
	cancel context.CancelFunc
}

func NewScheduler(engine *Engine, out chan<- store.Sample, maxConcurrent int, gap time.Duration, log *slog.Logger) *Scheduler {
	if maxConcurrent < 1 {
		maxConcurrent = 32
	}
	if gap <= 0 {
		gap = 400 * time.Millisecond
	}
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{
		engine:  engine,
		out:     out,
		log:     log,
		sem:     make(chan struct{}, maxConcurrent),
		gap:     gap,
		runners: make(map[int64]*runner),
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	s.ctx, s.cancel = context.WithCancel(ctx)
}

// Reload reconciles running goroutines with the current node list.
func (s *Scheduler) Reload(nodes []store.Node) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx == nil {
		return
	}
	seen := make(map[int64]bool, len(nodes))
	for _, n := range nodes {
		seen[n.ID] = true
		cur, ok := s.runners[n.ID]
		if ok && cur.node == n {
			continue
		}
		if ok {
			cur.cancel()
			delete(s.runners, n.ID)
		}
		if !n.Enabled {
			continue
		}
		ctx, cancel := context.WithCancel(s.ctx)
		s.runners[n.ID] = &runner{node: n, cancel: cancel}
		go s.loop(ctx, n)
	}
	for id, r := range s.runners {
		if !seen[id] {
			r.cancel()
			delete(s.runners, id)
		}
	}
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	for id, r := range s.runners {
		r.cancel()
		delete(s.runners, id)
	}
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Scheduler) loop(ctx context.Context, node store.Node) {
	interval := time.Duration(node.IntervalS) * time.Second
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	offset := offsetFor(node.ID, interval)
	timer := time.NewTimer(time.Until(nextRun(time.Now(), interval, offset)))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.round(ctx, node)
		timer.Reset(time.Until(nextRun(time.Now(), interval, offset)))
	}
}

func nextRun(now time.Time, interval, offset time.Duration) time.Time {
	base := now.Truncate(interval).Add(offset)
	if !base.After(now) {
		base = base.Add(interval)
	}
	return base
}

func offsetFor(id int64, interval time.Duration) time.Duration {
	h := fnv.New64a()
	var b [8]byte
	for i := 0; i < 8; i++ {
		b[i] = byte(id >> (8 * i))
	}
	h.Write(b[:])
	ms := interval.Milliseconds()
	if ms <= 0 {
		return 0
	}
	return time.Duration(h.Sum64()%uint64(ms)) * time.Millisecond
}

func (s *Scheduler) round(parent context.Context, node store.Node) {
	start := time.Now()
	s.rounds.Add(1)
	s.lastRound.Store(start.Unix())

	select {
	case s.sem <- struct{}{}:
	case <-parent.Done():
		return
	}
	s.inFlight.Add(1)
	defer func() {
		<-s.sem
		s.inFlight.Add(-1)
	}()

	t := Target{
		NodeID:    node.ID,
		Host:      node.Host,
		Port:      node.Port,
		Type:      node.Type,
		Count:     node.Count,
		Timeout:   time.Duration(node.TimeoutMs) * time.Millisecond,
		Family:    node.Family,
		PacketGap: s.gap,
	}
	budget := time.Duration(node.Count)*(t.Timeout+s.gap) + 3*time.Second
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()

	res := s.engine.Probe(ctx, t)
	elapsed := time.Since(start)
	s.lastMillis.Store(elapsed.Milliseconds())

	sp := store.Sample{
		NodeID:   node.ID,
		Ts:       start.Unix(),
		Sent:     res.Sent,
		Recv:     res.Recv,
		RttMinUs: res.RttMinUs,
		RttAvgUs: res.RttAvgUs,
		RttMaxUs: res.RttMaxUs,
	}
	select {
	case s.out <- sp:
	case <-parent.Done():
	}
	if res.Reason != "" && res.Reason != "timeout" {
		s.log.Debug("probe round failed", "node", node.ID, "host", node.Host, "reason", res.Reason)
	}
}

// ProbeNow runs one round outside the schedule, for the "test" button.
func (s *Scheduler) ProbeNow(ctx context.Context, node store.Node) (Result, time.Duration) {
	t := Target{
		NodeID:    node.ID,
		Host:      node.Host,
		Port:      node.Port,
		Type:      node.Type,
		Count:     node.Count,
		Timeout:   time.Duration(node.TimeoutMs) * time.Millisecond,
		Family:    node.Family,
		PacketGap: s.gap,
	}
	budget := time.Duration(node.Count)*(t.Timeout+s.gap) + 3*time.Second
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	start := time.Now()
	res := s.engine.Probe(ctx, t)
	return res, time.Since(start)
}

type SchedulerStats struct {
	Rounds      int64 `json:"rounds"`
	LastRoundTS int64 `json:"last_round_ts"`
	LastRoundMs int64 `json:"last_round_ms"`
	InFlight    int64 `json:"in_flight"`
}

func (s *Scheduler) Stats() SchedulerStats {
	return SchedulerStats{
		Rounds:      s.rounds.Load(),
		LastRoundTS: s.lastRound.Load(),
		LastRoundMs: s.lastMillis.Load(),
		InFlight:    s.inFlight.Load(),
	}
}
