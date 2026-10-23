package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linkprobe/internal/rangeplan"
	"linkprobe/internal/store"
	"linkprobe/web"
)

func TestRoutesDoNotLeakSPAShell(t *testing.T) {
	s := &Server{mux: http.NewServeMux(), web: web.Dist()}
	s.routes()

	cases := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/api/nope", http.StatusNotFound, "not_found"},
		{"/assets/missing.js", http.StatusNotFound, ""},
		{"/node/1", http.StatusOK, "<!doctype html>"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.wantStatus {
			t.Errorf("%s: status %d, want %d", c.path, rec.Code, c.wantStatus)
			continue
		}
		if c.wantBody != "" && !strings.Contains(strings.ToLower(rec.Body.String()), c.wantBody) {
			t.Errorf("%s: body %q lacks %q", c.path, rec.Body.String(), c.wantBody)
		}
	}
}

func validNode() store.Node {
	return store.Node{
		Name: "网关", Type: "icmp", Host: "192.168.10.1",
		IntervalS: 30, Count: 5, TimeoutMs: 2000, Family: "auto", Enabled: true,
	}
}

func TestValidateNode(t *testing.T) {
	n := validNode()
	if err := validateNode(&n); err != nil {
		t.Fatalf("valid node rejected: %v", err)
	}

	cases := []struct {
		name string
		mod  func(*store.Node)
	}{
		{"empty name", func(n *store.Node) { n.Name = "  " }},
		{"bad type", func(n *store.Node) { n.Type = "udp" }},
		{"bad host", func(n *store.Node) { n.Host = "bad host!" }},
		{"tcp no port", func(n *store.Node) { n.Type = "tcp" }},
		{"tcp port high", func(n *store.Node) { n.Type = "tcp"; n.Port = 70000 }},
		{"short interval", func(n *store.Node) { n.IntervalS = 5 }},
		{"long interval", func(n *store.Node) { n.IntervalS = 600 }},
		{"few packets", func(n *store.Node) { n.Count = 2 }},
		{"timeout", func(n *store.Node) { n.TimeoutMs = 9000 }},
		{"family", func(n *store.Node) { n.Family = "ipv4" }},
		{"interval vs timeout", func(n *store.Node) { n.IntervalS = 20; n.TimeoutMs = 3000 }},
	}
	for _, c := range cases {
		x := validNode()
		c.mod(&x)
		if err := validateNode(&x); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}

	tcp := validNode()
	tcp.Type, tcp.Port = "tcp", 443
	tcp.Host = "example.com"
	if err := validateNode(&tcp); err != nil {
		t.Fatalf("valid tcp rejected: %v", err)
	}
	if tcp.Port != 443 || tcp.Family != "auto" {
		t.Fatalf("defaults not normalised: %+v", tcp)
	}
	icmp := validNode()
	icmp.Port = 1234
	if err := validateNode(&icmp); err != nil {
		t.Fatal(err)
	}
	if icmp.Port != 0 {
		t.Fatal("icmp node must not keep a port")
	}
}

func TestValidateHost(t *testing.T) {
	good := []string{"192.168.10.1", "::1", "example.com", "a-b.example.co.uk", "localhost"}
	bad := []string{"", "bad host", "-start.com", "end-.com", "a..b"}
	for _, h := range good {
		if err := validateHost(h); err != nil {
			t.Errorf("%q rejected: %v", h, err)
		}
	}
	for _, h := range bad {
		if err := validateHost(h); err == nil {
			t.Errorf("%q accepted", h)
		}
	}
}

func TestBuildSummaryWeightsByReceived(t *testing.T) {
	pts := []store.Point{
		{Ts: 100, Sent: 5, Recv: 5, RttMinUs: 1000, RttMaxUs: 3000, RttSumUs: 10000},
		{Ts: 200, Sent: 5, Recv: 0},
		{Ts: 300, Sent: 5, Recv: 5, RttMinUs: 2000, RttMaxUs: 4000, RttSumUs: 15000},
	}
	plan := rangeplan.Plan{From: 100, To: 400, Res: 100, Layer: "raw"}
	s := buildSummary(pts, plan)
	if s.Sent != 15 || s.Recv != 10 {
		t.Fatalf("counts: %+v", s)
	}
	if s.Loss < 0.333 || s.Loss > 0.334 {
		t.Fatalf("loss %v", s.Loss)
	}
	if s.AvgMs == nil || *s.AvgMs != 2.5 {
		t.Fatalf("avg %v", s.AvgMs)
	}
	if s.MinMs == nil || *s.MinMs != 1 || s.MaxMs == nil || *s.MaxMs != 4 {
		t.Fatalf("min/max %v %v", s.MinMs, s.MaxMs)
	}
	if s.WorstLossTS != 200 || s.WorstLoss != 1 {
		t.Fatalf("worst %+v", s)
	}
	if s.Coverage != 1 {
		t.Fatalf("coverage %v", s.Coverage)
	}
}

func TestBuildSummaryEmpty(t *testing.T) {
	s := buildSummary(nil, rangeplan.Plan{From: 0, To: 100, Res: 10})
	if s.AvgMs != nil || s.Loss != 0 || s.Coverage != 0 {
		t.Fatalf("unexpected empty summary %+v", s)
	}
}

func TestConvertPointsKeepsNoData(t *testing.T) {
	pts := convertPoints([]store.Point{{Ts: 100, Sent: 5, Recv: 0}})
	if len(pts) != 1 || pts[0].Loss != 1 || pts[0].RttAvgMs != nil {
		t.Fatalf("no-data bucket mishandled: %+v", pts[0])
	}
}
