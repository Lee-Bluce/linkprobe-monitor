package rangeplan

import (
	"testing"
	"time"
)

func TestPresets(t *testing.T) {
	now := int64(1_800_000_000)
	want := map[string]struct {
		span  int64
		res   int64
		layer string
	}{
		"1h":  {3600, Res30s, "raw"},
		"6h":  {6 * 3600, Res1m, "raw"},
		"24h": {86400, Res5m, "5m"},
		"3d":  {3 * 86400, Res15m, "5m"},
		"7d":  {7 * 86400, Res30m, "5m"},
		"30d": {30 * 86400, Res2h, "1h"},
		"90d": {90 * 86400, Res6h, "1h"},
		"1y":  {365 * 86400, Res1d, "1h"},
	}
	if len(RangeKeys) != len(want) {
		t.Fatalf("range key count %d", len(RangeKeys))
	}
	for _, key := range RangeKeys {
		p, err := Resolve(key, now)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if p.From != now-want[key].span || p.To != now {
			t.Errorf("%s: window %d..%d", key, p.From, p.To)
		}
		if p.Res != want[key].res || p.Layer != want[key].layer {
			t.Errorf("%s: res=%d layer=%s", key, p.Res, p.Layer)
		}
		if p.Points() < 24 || p.Points() > 400 {
			t.Errorf("%s: %d points outside 24..400", key, p.Points())
		}
	}
	if _, err := Resolve("2h", now); err == nil {
		t.Fatal("unknown range accepted")
	}
}

func TestCustom(t *testing.T) {
	ret := Retention{
		Raw:     7 * 24 * time.Hour,
		Rollup5: 90 * 24 * time.Hour,
		Rollup1: 730 * 24 * time.Hour,
	}
	now := int64(1_800_000_000)
	cases := []struct {
		span  int64
		res   int64
		layer string
	}{
		{2 * 3600, Res30s, "raw"},
		{8 * 3600, Res5m, "raw"},
		{2 * 86400, Res15m, "5m"},
		{26 * 3600, Res5m, "5m"},
		{8 * 86400, Res30m, "5m"},
		{40 * 86400, Res6h, "1h"},
		{200 * 86400, Res1d, "1h"},
	}
	for _, c := range cases {
		p, err := Custom(now-c.span, now, ret)
		if err != nil {
			t.Fatalf("span %d: %v", c.span, err)
		}
		if p.Res != c.res || p.Layer != c.layer {
			t.Errorf("span %d: res=%d layer=%s want %d/%s", c.span, p.Res, p.Layer, c.res, c.layer)
		}
		if p.Points() > 400 && p.Res != Res1d {
			t.Errorf("span %d: %d points over cap", c.span, p.Points())
		}
	}
	if _, err := Custom(now, now, ret); err == nil {
		t.Fatal("empty range accepted")
	}
	if _, err := Custom(now-30, now, ret); err == nil {
		t.Fatal("too short range accepted")
	}
	if _, err := Custom(now-3*365*86400, now, ret); err == nil {
		t.Fatal("too long range accepted")
	}
}

func TestCustomWithShortRetention(t *testing.T) {
	ret := Retention{
		Raw:     3 * 24 * time.Hour,
		Rollup5: 30 * 24 * time.Hour,
		Rollup1: 365 * 24 * time.Hour,
	}
	now := int64(1_800_000_000)
	p, err := Custom(now-5*86400, now, ret)
	if err != nil {
		t.Fatal(err)
	}
	if p.Layer != "5m" {
		t.Fatalf("5 day span with 3 day raw retention should use 5m, got %s", p.Layer)
	}
}
