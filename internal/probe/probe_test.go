package probe

import (
	"context"
	"testing"
	"time"
)

func TestSummarize(t *testing.T) {
	min, avg, max := summarize([]int64{3000, 1000, 2000})
	if min != 1000 || avg != 2000 || max != 3000 {
		t.Fatalf("got %d %d %d", min, avg, max)
	}
	min, avg, max = summarize(nil)
	if min != 0 || avg != 0 || max != 0 {
		t.Fatal("empty summarize must be zero")
	}
}

func TestResultLoss(t *testing.T) {
	cases := []struct {
		sent, recv int
		want       float64
	}{
		{5, 5, 0},
		{5, 4, 0.2},
		{5, 0, 1},
		{0, 0, 0},
	}
	for _, c := range cases {
		r := Result{Sent: c.sent, Recv: c.recv}
		got := r.Loss()
		if diff := got - c.want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("%d/%d: got %v want %v", c.recv, c.sent, got, c.want)
		}
	}
}

func TestResolverLiteralAndFamily(t *testing.T) {
	r := NewResolver(time.Minute)
	ctx := context.Background()
	ips, err := r.Resolve(ctx, "192.168.10.1", "auto")
	if err != nil || len(ips) != 1 || ips[0].String() != "192.168.10.1" {
		t.Fatalf("literal ipv4: %v %v", ips, err)
	}
	if _, err := r.Resolve(ctx, "192.168.10.1", "6"); err == nil {
		t.Fatal("ipv4 literal accepted for family 6")
	}
	ips, err = r.Resolve(ctx, "::1", "6")
	if err != nil || len(ips) != 1 {
		t.Fatalf("literal ipv6: %v %v", ips, err)
	}
	if _, err := r.Resolve(ctx, "::1", "4"); err == nil {
		t.Fatal("ipv6 literal accepted for family 4")
	}
}

func TestClassifyDialError(t *testing.T) {
	if got := classifyDialError(context.DeadlineExceeded); got != "timeout" {
		t.Fatalf("got %s", got)
	}
}
