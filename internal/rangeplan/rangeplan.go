// Package rangeplan maps a requested time range onto a bucket width and a
// storage layer. Keeping it separate makes the mapping table-testable.
package rangeplan

import (
	"fmt"
	"time"
)

const (
	Res30s = 30
	Res1m  = 60
	Res5m  = 300
	Res15m = 900
	Res30m = 1800
	Res2h  = 7200
	Res6h  = 21600
	Res1d  = 86400
)

// Retention tells the resolver how far back each layer reaches.
type Retention struct {
	Raw     time.Duration
	Rollup5 time.Duration
	Rollup1 time.Duration
}

type Plan struct {
	Key   string
	From  int64
	To    int64
	Res   int64
	Layer string
}

type preset struct {
	span  time.Duration
	res   int64
	layer string
}

// Presets are the fixed steps offered in the interface. They are deliberately
// explicit: each range has a chosen layer, not a computed one.
var Presets = map[string]preset{
	"1h":  {1 * time.Hour, Res30s, "raw"},
	"6h":  {6 * time.Hour, Res1m, "raw"},
	"24h": {24 * time.Hour, Res5m, "5m"},
	"3d":  {3 * 24 * time.Hour, Res15m, "5m"},
	"7d":  {7 * 24 * time.Hour, Res30m, "5m"},
	"30d": {30 * 24 * time.Hour, Res2h, "1h"},
	"90d": {90 * 24 * time.Hour, Res6h, "1h"},
	"1y":  {365 * 24 * time.Hour, Res1d, "1h"},
}

// RangeKeys is the display order used by the front end.
var RangeKeys = []string{"1h", "6h", "24h", "3d", "7d", "30d", "90d", "1y"}

// BucketWidths is the candidate ladder for custom ranges.
var BucketWidths = []int64{Res30s, Res1m, Res5m, Res15m, Res30m, Res2h, Res6h, Res1d}

const maxPoints = 400

func Resolve(key string, now int64) (Plan, error) {
	p, ok := Presets[key]
	if !ok {
		return Plan{}, fmt.Errorf("unknown range %q", key)
	}
	return Plan{
		Key:   key,
		From:  now - int64(p.span.Seconds()),
		To:    now,
		Res:   p.res,
		Layer: p.layer,
	}, nil
}

// Custom picks the smallest bucket width that keeps the point count under
// maxPoints, then the finest layer that covers the span.
func Custom(from, to int64, ret Retention) (Plan, error) {
	if to <= from {
		return Plan{}, fmt.Errorf("to must be after from")
	}
	span := to - from
	if span < 60 {
		return Plan{}, fmt.Errorf("range must be at least one minute")
	}
	if span > int64((2 * 365 * 24 * time.Hour).Seconds()) {
		return Plan{}, fmt.Errorf("range must be at most two years")
	}
	res := BucketWidths[len(BucketWidths)-1]
	for _, w := range BucketWidths {
		if ceilDiv(span, w) <= maxPoints {
			res = w
			break
		}
	}
	return Plan{Key: "custom", From: from, To: to, Res: res, Layer: layerFor(span, res, ret)}, nil
}

// layerFor keeps queries cheap: raw data is only scanned for short spans.
func layerFor(span, res int64, ret Retention) string {
	raw := int64(ret.Raw.Seconds())
	r5 := int64(ret.Rollup5.Seconds())
	if span <= 24*3600 && (raw <= 0 || span <= raw) {
		return "raw"
	}
	if (r5 <= 0 || span <= r5) && res <= 3600 {
		return "5m"
	}
	return "1h"
}

func ceilDiv(a, b int64) int64 {
	if b <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

// Points returns the number of buckets a plan produces.
func (p Plan) Points() int64 { return ceilDiv(p.To-p.From, p.Res) }
