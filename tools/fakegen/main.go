// Command fakegen seeds a demo database with realistic probe history.
//
// It is a development tool: it writes raw samples for the last N days, derives
// the 5 minute and 1 hour rollups through the production aggregation code, then
// extends the hourly layer further back so long range queries can be measured.
// Nothing in the server imports this package.
//
// Example:
//
//	go run ./tools/fakegen -db ./fake.db -nodes 50 -days 30 -history 335 -bench
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"time"

	_ "modernc.org/sqlite"

	"linkprobe/internal/store"
)

var cst = time.FixedZone("CST", 8*3600)

type nodeSpec struct {
	group int
	name  string
	typ   string
	host  string
	port  int
	base  float64 // baseline RTT in milliseconds
	jit   float64 // relative jitter
	loss  float64 // per packet loss probability
	burst float64 // extra loss inside congestion windows
}

var groupNames = []string{
	"家庭内网",
	"运营商链路",
	"公共 DNS",
	"云服务",
	"海外线路",
	"备用与测试",
}

// specs describes 50 nodes with deliberately different shapes: LAN nodes are
// sub-millisecond with no loss, overseas nodes are slow with visible loss.
var specs = []nodeSpec{
	// 家庭内网
	{0, "光猫网关", "icmp", "192.168.10.1", 0, 0.4, 0.12, 0.000, 0},
	{0, "主路由", "icmp", "192.168.10.2", 0, 0.5, 0.10, 0.000, 0},
	{0, "客厅 AP", "icmp", "192.168.10.11", 0, 1.1, 0.18, 0.001, 0},
	{0, "书房 AP", "icmp", "192.168.10.12", 0, 1.3, 0.18, 0.001, 0},
	{0, "卧室 AP", "icmp", "192.168.10.13", 0, 1.6, 0.20, 0.002, 0},
	{0, "NAS", "icmp", "192.168.10.20", 0, 0.8, 0.14, 0.000, 0},
	{0, "电视盒子", "icmp", "192.168.10.31", 0, 2.4, 0.30, 0.002, 0},
	{0, "打印机", "icmp", "192.168.10.32", 0, 3.2, 0.35, 0.003, 0},
	{0, "智能音箱", "icmp", "192.168.10.33", 0, 3.8, 0.40, 0.004, 0},
	{0, "树莓派", "icmp", "192.168.10.40", 0, 1.0, 0.16, 0.001, 0},
	// 运营商链路
	{1, "电信网关", "icmp", "192.168.1.1", 0, 3.1, 0.25, 0.001, 0.05},
	{1, "联通网关", "icmp", "192.168.2.1", 0, 4.6, 0.28, 0.002, 0.05},
	{1, "移动网关", "icmp", "192.168.3.1", 0, 5.2, 0.30, 0.002, 0.05},
	{1, "电信 DNS", "icmp", "218.85.152.99", 0, 6.8, 0.20, 0.001, 0.04},
	{1, "联通 DNS", "icmp", "221.6.4.66", 0, 8.4, 0.22, 0.002, 0.04},
	{1, "移动 DNS", "icmp", "211.136.192.6", 0, 9.1, 0.24, 0.002, 0.04},
	{1, "运营商测速点", "tcp", "speedtest.telecom.cn", 8080, 12.0, 0.35, 0.004, 0.08},
	{1, "城域网出口", "icmp", "202.97.33.1", 0, 11.5, 0.30, 0.003, 0.06},
	// 公共 DNS
	{2, "阿里 DNS", "icmp", "223.5.5.5", 0, 7.2, 0.18, 0.001, 0.03},
	{2, "腾讯 DNS", "icmp", "119.29.29.29", 0, 8.0, 0.20, 0.001, 0.03},
	{2, "百度 DNS", "icmp", "180.76.76.76", 0, 9.4, 0.22, 0.002, 0.04},
	{2, "114 DNS", "icmp", "114.114.114.114", 0, 10.2, 0.22, 0.002, 0.04},
	{2, "CNNIC DNS", "icmp", "1.2.4.8", 0, 11.8, 0.25, 0.003, 0.05},
	{2, "Cloudflare", "icmp", "1.1.1.1", 0, 45.0, 0.30, 0.006, 0.10},
	{2, "Google DNS", "icmp", "8.8.8.8", 0, 52.0, 0.32, 0.008, 0.12},
	{2, "Quad9", "icmp", "9.9.9.9", 0, 68.0, 0.34, 0.010, 0.14},
	// 云服务
	{3, "阿里云-上海", "icmp", "47.100.0.1", 0, 18.5, 0.25, 0.002, 0.05},
	{3, "阿里云-北京", "icmp", "47.94.0.1", 0, 32.0, 0.28, 0.003, 0.06},
	{3, "腾讯云-广州", "icmp", "129.204.0.1", 0, 24.0, 0.26, 0.002, 0.05},
	{3, "腾讯云-上海", "icmp", "111.230.0.1", 0, 21.0, 0.25, 0.002, 0.05},
	{3, "华为云-深圳", "icmp", "121.36.0.1", 0, 26.0, 0.28, 0.003, 0.06},
	{3, "AWS-东京", "tcp", "13.112.0.1", 443, 72.0, 0.30, 0.005, 0.10},
	{3, "AWS-新加坡", "tcp", "13.228.0.1", 443, 88.0, 0.32, 0.006, 0.12},
	{3, "火山引擎", "icmp", "106.75.0.1", 0, 23.0, 0.26, 0.002, 0.05},
	{3, "京东云", "icmp", "116.196.0.1", 0, 27.0, 0.28, 0.003, 0.06},
	{3, "青云", "icmp", "139.198.0.1", 0, 30.0, 0.30, 0.004, 0.07},
	// 海外线路
	{4, "GitHub", "tcp", "140.82.112.3", 443, 165.0, 0.35, 0.010, 0.20},
	{4, "Cloudflare CDN", "tcp", "104.16.0.1", 443, 120.0, 0.32, 0.008, 0.18},
	{4, "Akamai", "tcp", "23.32.0.1", 443, 140.0, 0.34, 0.009, 0.18},
	{4, "Fastly", "tcp", "151.101.0.1", 443, 150.0, 0.35, 0.010, 0.20},
	{4, "DigitalOcean-SG", "icmp", "128.199.0.1", 0, 95.0, 0.32, 0.008, 0.16},
	{4, "Vultr-东京", "icmp", "108.61.0.1", 0, 85.0, 0.30, 0.007, 0.15},
	{4, "Linode-新加坡", "icmp", "139.162.0.1", 0, 110.0, 0.33, 0.009, 0.18},
	{4, "Google", "icmp", "142.250.0.1", 0, 175.0, 0.36, 0.012, 0.22},
	// 备用与测试
	{5, "公司 VPN", "icmp", "10.8.0.1", 0, 38.0, 0.30, 0.004, 0.08},
	{5, "家庭备份服务器", "icmp", "192.168.10.50", 0, 1.2, 0.18, 0.001, 0},
	{5, "旧路由", "icmp", "192.168.10.60", 0, 4.5, 0.45, 0.010, 0.12},
	{5, "4G 备份", "icmp", "192.168.8.1", 0, 55.0, 0.45, 0.015, 0.25},
	{5, "卫星链路", "icmp", "192.168.9.1", 0, 620.0, 0.25, 0.020, 0.30},
	{5, "测试节点", "icmp", "192.0.2.1", 0, 15.0, 0.40, 0.050, 0.35},
}

type window struct{ from, to int64 }

type profile struct {
	nodeSpec
	nodeID  int64
	outages []window
	bursts  []window
}

func main() {
	dbPath := flag.String("db", "fake.db", "database file to create or extend")
	nodeCount := flag.Int("nodes", 50, "number of demo nodes (max 50)")
	days := flag.Int("days", 30, "days of raw samples")
	interval := flag.Int("interval", 30, "raw sample interval in seconds")
	history := flag.Int("history", 335, "extra days of hourly history")
	seed := flag.Int64("seed", 20261023, "random seed")
	reset := flag.Bool("reset", false, "delete the database file first")
	bench := flag.Bool("bench", true, "run range query timings at the end")
	flag.Parse()

	if *nodeCount > len(specs) {
		fmt.Fprintf(os.Stderr, "nodes: at most %d demo profiles are defined\n", len(specs))
		os.Exit(2)
	}
	if *interval < 5 {
		fmt.Fprintln(os.Stderr, "interval must be at least 5 seconds")
		os.Exit(2)
	}
	if *reset {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if err := os.Remove(*dbPath + suffix); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "reset: %v\n", err)
				os.Exit(1)
			}
		}
	}

	ctx := context.Background()
	st, err := store.Open(*dbPath)
	must(err, "open store")
	defer st.Close()

	nodes, err := st.Nodes(ctx)
	must(err, "list nodes")
	if len(nodes) == 0 {
		nodes = seedNodes(ctx, st, *nodeCount)
		fmt.Printf("seeded %d nodes\n", len(nodes))
	} else {
		fmt.Printf("reusing %d existing nodes\n", len(nodes))
	}

	now := time.Now().Unix()
	start := now - int64(*days)*86400
	start -= start % int64(*interval)

	have, err := st.CountSamples(ctx, nodes[0].ID)
	must(err, "count samples")
	if have == 0 {
		fmt.Printf("writing raw samples: %d nodes x %d days @ %ds\n", len(nodes), *days, *interval)
		writeRaw(ctx, st, nodes, start, now, int64(*interval), *seed)
	} else {
		fmt.Printf("raw samples already present (%d for first node), skipping\n", have)
	}

	from5 := store.AlignDown(start, 300)
	to5 := store.AlignDown(now, 300)
	n5, err := st.Agg5m(ctx, from5, to5)
	must(err, "aggregate 5m")
	fmt.Printf("rollup_5m: %d rows refreshed\n", n5)

	from1 := store.AlignDown(start, 3600)
	to1 := store.AlignDown(now, 3600)
	n1, err := st.Agg1h(ctx, from1, to1)
	must(err, "aggregate 1h")
	fmt.Printf("rollup_1h: %d rows refreshed\n", n1)

	if *history > 0 {
		histFrom := store.AlignDown(now-int64(*days+*history)*86400, 3600)
		histTo := from1
		if histTo > histFrom {
			n, err := writeHistory(*dbPath, nodes, histFrom, histTo, *seed)
			must(err, "write hourly history")
			fmt.Printf("rollup_1h: %d synthetic rows back to %s\n", n,
				time.Unix(histFrom, 0).In(cst).Format("2006-01-02"))
		}
	}

	fmt.Printf("database: %s (%.1f MiB)\n", *dbPath, float64(st.DiskSize())/(1<<20))

	if *bench {
		runBench(ctx, st, nodes, now)
	}
}

func must(err error, what string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", what, err)
		os.Exit(1)
	}
}

func seedNodes(ctx context.Context, st *store.Store, n int) []store.Node {
	now := time.Now().Unix()
	groups := make([]store.Group, len(groupNames))
	for i, name := range groupNames {
		g, err := st.CreateGroup(ctx, name, now)
		must(err, "create group")
		groups[i] = g
	}
	out := make([]store.Node, 0, n)
	for i := 0; i < n; i++ {
		sp := specs[i]
		gid := groups[sp.group].ID
		node, err := st.CreateNode(ctx, store.Node{
			GroupID:   &gid,
			Name:      sp.name,
			Type:      sp.typ,
			Host:      sp.host,
			Port:      sp.port,
			IntervalS: 30,
			Count:     5,
			TimeoutMs: 2000,
			Family:    "auto",
			Enabled:   true,
			Note:      "演示数据",
		}, now)
		must(err, "create node")
		out = append(out, node)
	}
	return out
}

// writeRaw generates one sample per node per interval and inserts in batches.
func writeRaw(ctx context.Context, st *store.Store, nodes []store.Node, start, end, interval, seed int64) {
	profiles := makeProfiles(nodes, start, end, seed)
	const batchSize = 2000
	batch := make([]store.Sample, 0, batchSize)
	written := 0
	lastReport := time.Now()

	for _, p := range profiles {
		rng := rand.New(rand.NewSource(seed + p.nodeID*7919))
		for ts := start; ts < end; ts += interval {
			batch = append(batch, sampleAt(p, ts, rng))
			if len(batch) >= batchSize {
				must(st.InsertSamples(ctx, batch), "insert samples")
				written += len(batch)
				batch = batch[:0]
				if time.Since(lastReport) > 5*time.Second {
					fmt.Printf("  %d samples...\n", written)
					lastReport = time.Now()
				}
			}
		}
	}
	if len(batch) > 0 {
		must(st.InsertSamples(ctx, batch), "insert samples")
		written += len(batch)
	}
	fmt.Printf("  wrote %d raw samples\n", written)
}

func makeProfiles(nodes []store.Node, start, end, seed int64) []profile {
	out := make([]profile, 0, len(nodes))
	for _, n := range nodes {
		sp := nodeSpec{name: n.Name, host: n.Host, typ: n.Type, port: n.Port, base: 10, jit: 0.2}
		for _, s := range specs {
			if s.name == n.Name {
				sp = s
				break
			}
		}
		rng := rand.New(rand.NewSource(seed + n.ID*104729))
		out = append(out, profile{
			nodeSpec: sp,
			nodeID:   n.ID,
			outages:  makeWindows(rng, start, end, outageCount(sp)),
			bursts:   makeWindows(rng, start, end, 3+int(sp.loss*200)),
		})
	}
	return out
}

func outageCount(sp nodeSpec) int {
	switch {
	case sp.loss >= 0.01:
		return 2 + int(sp.loss*100) // flaky lines drop more often
	case sp.loss > 0:
		return 1
	default:
		return 0
	}
}

func makeWindows(rng *rand.Rand, start, end int64, n int) []window {
	out := make([]window, 0, n)
	span := end - start
	if span <= 0 {
		return out
	}
	for i := 0; i < n; i++ {
		from := start + rng.Int63n(span)
		length := int64(600 + rng.Intn(2*3600))
		if sp := rng.Float64(); sp < 0.15 {
			length = int64(4*3600 + rng.Intn(4*3600)) // rarer, longer outage
		}
		out = append(out, window{from: from, to: from + length})
	}
	return out
}

func sampleAt(p profile, ts int64, rng *rand.Rand) store.Sample {
	sent := 5
	if inAny(p.outages, ts) {
		return store.Sample{NodeID: p.nodeID, Ts: ts, Sent: sent, Recv: 0}
	}
	loss := p.loss
	if inAny(p.bursts, ts) {
		loss += p.burst
	}
	recv := sent
	for i := 0; i < sent; i++ {
		if rng.Float64() < loss {
			recv--
		}
	}
	if recv <= 0 {
		return store.Sample{NodeID: p.nodeID, Ts: ts, Sent: sent, Recv: 0}
	}

	base := p.base
	if base <= 0 {
		base = 10
	}
	rtt := base * diurnal(ts) * (1 + p.jit*rng.NormFloat64())
	if rng.Float64() < 0.02 {
		rtt *= 1.5 + rng.Float64() // occasional queueing spike
	}
	if rtt < 0.05 {
		rtt = 0.05
	}
	avgUs := int64(rtt * 1000)
	minUs := int64(float64(avgUs) * (0.90 + 0.05*rng.Float64()))
	maxUs := int64(float64(avgUs) * (1.05 + 0.15*rng.Float64()))
	if minUs < 1 {
		minUs = 1
	}
	if maxUs <= avgUs {
		maxUs = avgUs + 1
	}
	return store.Sample{
		NodeID: p.nodeID, Ts: ts, Sent: sent, Recv: recv,
		RttMinUs: minUs, RttAvgUs: avgUs, RttMaxUs: maxUs,
	}
}

func inAny(ws []window, ts int64) bool {
	for _, w := range ws {
		if ts >= w.from && ts < w.to {
			return true
		}
	}
	return false
}

// diurnal peaks around 21:00 local time, when the household is busiest.
func diurnal(ts int64) float64 {
	t := time.Unix(ts, 0).In(cst)
	h := float64(t.Hour()) + float64(t.Minute())/60
	return 1 + 0.28*math.Cos((h-21.0)/24.0*2*math.Pi)
}

// writeHistory fills rollup_1h before the raw window so year-long queries have
// data. It talks to SQLite directly because the store only exposes aggregation
// from real samples, which would need tens of millions of rows for a year.
func writeHistory(dbPath string, nodes []store.Node, from, to, seed int64) (int64, error) {
	dsn := "file:" + dbPath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO rollup_1h
(node_id, ts, sent, recv, rtt_sum_us, rtt_min_us, rtt_max_us)
VALUES(?,?,?,?,?,?,?)
ON CONFLICT(node_id, ts) DO UPDATE SET
  sent=excluded.sent, recv=excluded.recv, rtt_sum_us=excluded.rtt_sum_us,
  rtt_min_us=excluded.rtt_min_us, rtt_max_us=excluded.rtt_max_us`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	var n int64
	for _, node := range nodes {
		sp := nodeSpec{base: 10, jit: 0.2, loss: 0.002}
		for _, s := range specs {
			if s.name == node.Name {
				sp = s
				break
			}
		}
		rng := rand.New(rand.NewSource(seed + node.ID*15485863))
		for ts := from; ts < to; ts += 3600 {
			// One hour bucket is 120 rounds of five packets.
			sent := int64(600)
			loss := sp.loss * 1.1
			recv := sent
			for i := int64(0); i < sent; i++ {
				if rng.Float64() < loss {
					recv--
				}
			}
			if recv <= 0 {
				if _, err := stmt.Exec(node.ID, ts, sent, 0, 0, 0, 0); err != nil {
					return n, err
				}
				n++
				continue
			}
			rtt := sp.base * diurnal(ts) * (1 + sp.jit*rng.NormFloat64())
			if rtt < 0.05 {
				rtt = 0.05
			}
			avgUs := int64(rtt * 1000)
			minUs := int64(float64(avgUs) * 0.88)
			maxUs := int64(float64(avgUs) * 1.18)
			if _, err := stmt.Exec(node.ID, ts, sent, recv, avgUs*recv, minUs, maxUs); err != nil {
				return n, err
			}
			n++
		}
	}
	if err := tx.Commit(); err != nil {
		return n, err
	}
	return n, nil
}

func runBench(ctx context.Context, st *store.Store, nodes []store.Node, now int64) {
	type test struct {
		name  string
		from  int64
		to    int64
		res   int64
		layer string
	}
	tests := []test{
		{"24h/5m", now - 86400, now, 300, store.Layer5m},
		{"1y/1d", now - 365*86400, now, 86400, store.Layer1h},
	}
	fmt.Println("range query timings (per node):")
	for _, tc := range tests {
		durations := make([]time.Duration, 0, len(nodes))
		var points int
		for _, n := range nodes {
			begin := time.Now()
			pts, err := st.Series(ctx, n.ID, tc.from, tc.to, tc.res, tc.layer)
			must(err, "bench series")
			durations = append(durations, time.Since(begin))
			points += len(pts)
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		p50 := durations[len(durations)/2]
		p95 := durations[int(float64(len(durations))*0.95)]
		worst := durations[len(durations)-1]
		fmt.Printf("  %-7s nodes=%d points=%d p50=%s p95=%s max=%s\n",
			tc.name, len(nodes), points, roundMS(p50), roundMS(p95), roundMS(worst))
	}
}

func roundMS(d time.Duration) time.Duration { return d.Round(100 * time.Microsecond) }
