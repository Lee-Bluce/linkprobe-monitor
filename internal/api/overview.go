package api

import (
	"context"
	"net/http"
	"time"

	"linkprobe/internal/health"
	"linkprobe/internal/rangeplan"
	"linkprobe/internal/store"
)

type ovNode struct {
	store.Node
	Status    string        `json:"status"`
	LastTS    int64         `json:"last_ts"`
	LastRttMs *float64      `json:"last_rtt_ms"`
	LastLoss  float64       `json:"last_loss"`
	Summary   Summary       `json:"summary"`
	Spark     []SeriesPoint `json:"spark"`
}

type ovGroup struct {
	ID    int64    `json:"id"`
	Name  string   `json:"name"`
	Sort  int      `json:"sort"`
	Nodes []ovNode `json:"nodes"`
}

type overviewTotals struct {
	Nodes    int `json:"nodes"`
	Online   int `json:"online"`
	Degraded int `json:"degraded"`
	Offline  int `json:"offline"`
	Unknown  int `json:"unknown"`
	Paused   int `json:"paused"`
}

// sparkPlan coarsens the display bucket so the overview stays around 48
// points per node regardless of the selected range.
func sparkPlan(p rangeplan.Plan) rangeplan.Plan {
	const target = int64(48)
	want := (p.To - p.From) / target
	if want < p.Res {
		want = p.Res
	}
	res := ((want + p.Res - 1) / p.Res) * p.Res
	if res < 1 {
		res = p.Res
	}
	p.Res = res
	return p
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().Unix()
	plan, err := s.planFromQuery(r, now)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_range", "时间范围无效")
		return
	}
	sp := sparkPlan(plan)
	groups, err := s.st.Groups(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	nodes, err := s.st.Nodes(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	byGroup := make(map[int64][]ovNode)
	ungrouped := []ovNode{}
	totals := overviewTotals{Nodes: len(nodes)}
	for _, n := range nodes {
		item := s.buildOvNode(ctx, n, now, sp)
		switch item.Status {
		case health.StateOK:
			totals.Online++
		case health.StateDegraded:
			totals.Degraded++
		case health.StateOffline:
			totals.Offline++
		case "paused":
			totals.Paused++
		default:
			totals.Unknown++
		}
		if n.GroupID != nil {
			byGroup[*n.GroupID] = append(byGroup[*n.GroupID], item)
		} else {
			ungrouped = append(ungrouped, item)
		}
	}
	outGroups := make([]ovGroup, 0, len(groups))
	for _, g := range groups {
		items := byGroup[g.ID]
		if items == nil {
			items = []ovNode{}
		}
		outGroups = append(outGroups, ovGroup{ID: g.ID, Name: g.Name, Sort: g.Sort, Nodes: items})
	}
	warning := ""
	if len(nodes) > 150 {
		warning = "节点较多，建议把采样间隔调到 60 秒以上以降低负载"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"range":        plan.Key,
		"from":         plan.From,
		"to":           plan.To,
		"res":          sp.Res,
		"layer":        sp.Layer,
		"generated_at": now,
		"groups":       outGroups,
		"ungrouped":    ungrouped,
		"totals":       totals,
		"warning":      warning,
	})
}

func (s *Server) buildOvNode(ctx context.Context, n store.Node, now int64, sp rangeplan.Plan) ovNode {
	item := ovNode{Node: n, Spark: []SeriesPoint{}}
	recent, err := s.st.RecentSamples(ctx, n.ID, 3)
	if err != nil {
		s.log.Error("recent samples failed", "node", n.ID, "err", err)
	}
	if len(recent) > 0 {
		last := recent[0]
		item.LastTS = last.Ts
		if last.Recv > 0 {
			item.LastRttMs = fptr(usToMs(last.RttAvgUs))
		}
		if last.Sent > 0 {
			item.LastLoss = float64(last.Sent-last.Recv) / float64(last.Sent)
		}
	}
	avg24, hasAvg, err := s.st.AvgRTT(ctx, n.ID, now-24*3600, now)
	if err != nil {
		s.log.Error("avg rtt failed", "node", n.ID, "err", err)
	}
	rounds := make([]health.Round, 0, len(recent))
	for _, r := range recent {
		rounds = append(rounds, health.Round{Ts: r.Ts, Sent: r.Sent, Recv: r.Recv, AvgUs: r.RttAvgUs})
	}
	if n.Enabled {
		item.Status = health.Evaluate(rounds, avg24, hasAvg, now, n.IntervalS)
	} else {
		item.Status = "paused"
	}
	pts, err := s.st.Series(ctx, n.ID, sp.From, sp.To, sp.Res, sp.Layer)
	if err != nil {
		s.log.Error("series failed", "node", n.ID, "err", err)
	} else {
		item.Spark = convertPoints(pts)
		item.Summary = buildSummary(pts, sp)
	}
	return item
}

func (s *Server) system(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.st.Nodes(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	groups, err := s.st.Groups(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	now := time.Now().Unix()
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     s.version,
		"build_time":  s.build,
		"started_at":  s.started.Unix(),
		"uptime_s":    now - s.started.Unix(),
		"now":         now,
		"node_count":  len(nodes),
		"group_count": len(groups),
		"db_size":     s.st.DiskSize(),
		"queue":       s.writer.Stats(),
		"probe":       s.sched.Stats(),
		"clock_ok":    s.clockOK(),
		"ranges":      rangeplan.RangeKeys,
		"retention": map[string]int{
			"raw_days":       s.cfg.Retention.RawDays,
			"rollup_5m_days": s.cfg.Retention.Rollup5mDays,
			"rollup_1h_days": s.cfg.Retention.Rollup1hDays,
		},
	})
}
