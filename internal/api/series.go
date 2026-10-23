package api

import (
	"net/http"
	"strconv"
	"time"

	"linkprobe/internal/health"
	"linkprobe/internal/rangeplan"
	"linkprobe/internal/store"
)

type SeriesPoint struct {
	Ts       int64    `json:"ts"`
	Sent     int64    `json:"sent"`
	Recv     int64    `json:"recv"`
	Loss     float64  `json:"loss"`
	RttMinMs *float64 `json:"rtt_min_ms"`
	RttAvgMs *float64 `json:"rtt_avg_ms"`
	RttMaxMs *float64 `json:"rtt_max_ms"`
}

type Summary struct {
	Sent        int64    `json:"sent"`
	Recv        int64    `json:"recv"`
	Loss        float64  `json:"loss"`
	AvgMs       *float64 `json:"avg_ms"`
	MinMs       *float64 `json:"min_ms"`
	MaxMs       *float64 `json:"max_ms"`
	WorstLoss   float64  `json:"worst_loss"`
	WorstLossTS int64    `json:"worst_loss_ts"`
	Coverage    float64  `json:"coverage"`
	Buckets     int      `json:"buckets"`
	Expected    int64    `json:"expected_buckets"`
}

func fptr(v float64) *float64 { return &v }

func convertPoints(pts []store.Point) []SeriesPoint {
	out := make([]SeriesPoint, 0, len(pts))
	for _, p := range pts {
		sp := SeriesPoint{Ts: p.Ts, Sent: p.Sent, Recv: p.Recv}
		if p.Sent > 0 {
			sp.Loss = 1 - float64(p.Recv)/float64(p.Sent)
		}
		if p.Recv > 0 {
			sp.RttMinMs = fptr(usToMs(p.RttMinUs))
			sp.RttMaxMs = fptr(usToMs(p.RttMaxUs))
			sp.RttAvgMs = fptr(usToMs(p.RttSumUs / p.Recv))
		}
		out = append(out, sp)
	}
	return out
}

// buildSummary weights latency by received packets, so it is exact regardless
// of the bucket width used for display.
func buildSummary(pts []store.Point, plan rangeplan.Plan) Summary {
	sum := Summary{Buckets: len(pts)}
	expected := plan.Points()
	if expected < 1 {
		expected = 1
	}
	sum.Expected = expected
	var rttSum int64
	var minUs, maxUs int64
	var haveMin, haveMax bool
	for _, p := range pts {
		sum.Sent += p.Sent
		sum.Recv += p.Recv
		rttSum += p.RttSumUs
		if p.Recv > 0 {
			if !haveMin || p.RttMinUs < minUs {
				minUs, haveMin = p.RttMinUs, true
			}
			if !haveMax || p.RttMaxUs > maxUs {
				maxUs, haveMax = p.RttMaxUs, true
			}
		}
		var loss float64
		if p.Sent > 0 {
			loss = 1 - float64(p.Recv)/float64(p.Sent)
		}
		if loss >= sum.WorstLoss {
			sum.WorstLoss = loss
			sum.WorstLossTS = p.Ts
		}
	}
	if sum.Sent > 0 {
		sum.Loss = 1 - float64(sum.Recv)/float64(sum.Sent)
	}
	if sum.Recv > 0 {
		sum.AvgMs = fptr(usToMs(rttSum / sum.Recv))
	}
	if haveMin {
		sum.MinMs = fptr(usToMs(minUs))
	}
	if haveMax {
		sum.MaxMs = fptr(usToMs(maxUs))
	}
	sum.Coverage = float64(len(pts)) / float64(expected)
	if sum.Coverage > 1 {
		sum.Coverage = 1
	}
	return sum
}

func (s *Server) retention() rangeplan.Retention {
	return rangeplan.Retention{
		Raw:     time.Duration(s.cfg.Retention.RawDays) * 24 * time.Hour,
		Rollup5: time.Duration(s.cfg.Retention.Rollup5mDays) * 24 * time.Hour,
		Rollup1: time.Duration(s.cfg.Retention.Rollup1hDays) * 24 * time.Hour,
	}
}

func (s *Server) planFromQuery(r *http.Request, now int64) (rangeplan.Plan, error) {
	q := r.URL.Query()
	fromStr, toStr := q.Get("from"), q.Get("to")
	if fromStr != "" || toStr != "" {
		from, err1 := strconv.ParseInt(fromStr, 10, 64)
		to, err2 := strconv.ParseInt(toStr, 10, 64)
		if err1 != nil || err2 != nil {
			return rangeplan.Plan{}, errBadRange
		}
		return rangeplan.Custom(from, to, s.retention())
	}
	key := q.Get("range")
	if key == "" {
		key = "24h"
	}
	return rangeplan.Resolve(key, now)
}

type badRangeError struct{}

func (badRangeError) Error() string { return "时间范围无效" }

var errBadRange = badRangeError{}

func (s *Server) series(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	node, err := s.st.Node(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	now := time.Now().Unix()
	plan, err := s.planFromQuery(r, now)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_range", "时间范围无效")
		return
	}
	pts, err := s.st.Series(r.Context(), id, plan.From, plan.To, plan.Res, plan.Layer)
	if err != nil {
		s.fail(w, err)
		return
	}
	status, lastTS := s.nodeStatus(r, node, now)
	writeJSON(w, http.StatusOK, map[string]any{
		"node":         node,
		"status":       status,
		"last_ts":      lastTS,
		"range":        plan.Key,
		"from":         plan.From,
		"to":           plan.To,
		"res":          plan.Res,
		"layer":        plan.Layer,
		"points":       convertPoints(pts),
		"summary":      buildSummary(pts, plan),
		"generated_at": now,
	})
}

// nodeStatus reuses the same rules as the overview card.
func (s *Server) nodeStatus(r *http.Request, node store.Node, now int64) (string, int64) {
	if !node.Enabled {
		return "paused", 0
	}
	ctx := r.Context()
	recent, err := s.st.RecentSamples(ctx, node.ID, 3)
	if err != nil {
		s.log.Error("recent samples failed", "node", node.ID, "err", err)
	}
	rounds := make([]health.Round, 0, len(recent))
	var lastTS int64
	for i, x := range recent {
		if i == 0 {
			lastTS = x.Ts
		}
		rounds = append(rounds, health.Round{Ts: x.Ts, Sent: x.Sent, Recv: x.Recv, AvgUs: x.RttAvgUs})
	}
	avg24, hasAvg, err := s.st.AvgRTT(ctx, node.ID, now-86400, now)
	if err != nil {
		s.log.Error("avg rtt failed", "node", node.ID, "err", err)
	}
	return health.Evaluate(rounds, avg24, hasAvg, now, node.IntervalS), lastTS
}
