// Package health turns recent probe rounds into the card status dot.
package health

const (
	StateOK       = "ok"
	StateDegraded = "degraded"
	StateOffline  = "offline"
	StateUnknown  = "unknown"
)

// Round is the minimum a status decision needs from one round.
type Round struct {
	Ts    int64
	Sent  int
	Recv  int
	AvgUs int64
}

// Evaluate implements the rules from the plan:
//   - no samples, or the newest sample is stale -> unknown
//   - the last three rounds received nothing      -> offline
//   - last round loss >= 20%, or last RTT > 3x the
//     24h average                                 -> degraded
//   - otherwise                                   -> ok
func Evaluate(recent []Round, avg24Us float64, hasAvg bool, now int64, intervalS int) string {
	if len(recent) == 0 {
		return StateUnknown
	}
	last := recent[0]
	staleAfter := int64(3 * intervalS)
	if staleAfter < 120 {
		staleAfter = 120
	}
	if last.Ts <= 0 || now-last.Ts > staleAfter {
		return StateUnknown
	}
	silent := 0
	for _, r := range recent {
		if r.Recv == 0 {
			silent++
		} else {
			break
		}
	}
	if silent >= 3 {
		return StateOffline
	}
	if last.Sent > 0 && float64(last.Sent-last.Recv)/float64(last.Sent) >= 0.2 {
		return StateDegraded
	}
	if hasAvg && avg24Us > 0 && float64(last.AvgUs) > 3*avg24Us {
		return StateDegraded
	}
	return StateOK
}
