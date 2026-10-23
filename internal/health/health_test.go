package health

import "testing"

func rounds(recv ...int) []Round {
	out := make([]Round, 0, len(recv))
	for i, r := range recv {
		out = append(out, Round{Ts: int64(1000 - i*30), Sent: 5, Recv: r, AvgUs: 2000})
	}
	return out
}

func TestEvaluate(t *testing.T) {
	now := int64(1000)
	cases := []struct {
		name   string
		recent []Round
		avg24  float64
		hasAvg bool
		want   string
	}{
		{"no data", nil, 0, false, StateUnknown},
		{"three silent rounds", rounds(0, 0, 0), 0, false, StateOffline},
		{"two silent then alive", rounds(0, 0, 5), 0, false, StateDegraded},
		{"healthy", rounds(5, 5, 5), 2000, true, StateOK},
		{"20 percent loss", rounds(4, 5, 5), 2000, true, StateDegraded},
		{"19 percent loss", []Round{{Ts: now, Sent: 100, Recv: 81, AvgUs: 2000}}, 2000, true, StateOK},
		{"latency spike", rounds(5, 5, 5), 500, true, StateDegraded},
		{"first round silent", rounds(0), 0, false, StateDegraded},
		{"stale sample", []Round{{Ts: 0, Sent: 5, Recv: 5}}, 0, false, StateUnknown},
	}
	for _, c := range cases {
		if got := Evaluate(c.recent, c.avg24, c.hasAvg, now, 30); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestStaleUsesInterval(t *testing.T) {
	now := int64(10_000)
	recent := []Round{{Ts: now - 1000, Sent: 5, Recv: 5, AvgUs: 2000}}
	if got := Evaluate(recent, 0, false, now, 300); got != StateUnknown {
		t.Fatalf("want unknown for stale data at 300s interval, got %s", got)
	}
	recent[0].Ts = now - 200
	if got := Evaluate(recent, 0, false, now, 300); got != StateOK {
		t.Fatalf("want ok inside 3 intervals, got %s", got)
	}
}
