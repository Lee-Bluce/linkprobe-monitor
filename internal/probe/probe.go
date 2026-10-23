// Package probe implements the ICMP and TCP probing engines plus the
// scheduler that drives them.
package probe

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// Target is one probe run configuration.
type Target struct {
	NodeID    int64
	Host      string
	Port      int
	Type      string // "icmp" or "tcp"
	Count     int
	Timeout   time.Duration
	Family    string // "auto", "4", "6"
	PacketGap time.Duration
}

// Result is the outcome of one round.
type Result struct {
	Sent     int
	Recv     int
	RttMinUs int64
	RttAvgUs int64
	RttMaxUs int64
	Reason   string // empty when every packet came back
}

// Loss returns the round loss ratio in [0,1].
func (r Result) Loss() float64 {
	if r.Sent == 0 {
		return 0
	}
	return 1 - float64(r.Recv)/float64(r.Sent)
}

// Prober is the interface the scheduler uses; tests substitute their own.
type Prober interface {
	Probe(ctx context.Context, t Target) Result
}

// Engine runs real ICMP and TCP probes and caches DNS answers.
type Engine struct {
	Resolver *Resolver
}

func NewEngine(dnsTTL time.Duration) *Engine {
	return &Engine{Resolver: NewResolver(dnsTTL)}
}

func (e *Engine) Probe(ctx context.Context, t Target) Result {
	if t.Count < 1 {
		t.Count = 1
	}
	if t.Timeout <= 0 {
		t.Timeout = 2 * time.Second
	}
	if t.PacketGap <= 0 {
		t.PacketGap = 400 * time.Millisecond
	}
	ips, err := e.Resolver.Resolve(ctx, t.Host, t.Family)
	if err != nil {
		// A resolution failure is a full loss: the round was attempted but no
		// packet could be sent.
		return Result{Sent: t.Count, Recv: 0, Reason: "dns"}
	}
	ip := ips[0]
	if t.Type == "tcp" {
		return tcpProbe(ctx, ip, t.Port, t.Count, t.Timeout, t.PacketGap)
	}
	return icmpProbe(ctx, ip, t.Count, t.Timeout, t.PacketGap)
}

// Resolver caches DNS answers for a short time and understands literal IPs.
type Resolver struct {
	ttl time.Duration
	mu  sync.Mutex
	m   map[string]dnsEntry
}

type dnsEntry struct {
	ips     []net.IP
	expires time.Time
}

func NewResolver(ttl time.Duration) *Resolver {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Resolver{ttl: ttl, m: make(map[string]dnsEntry)}
}

func (r *Resolver) Resolve(ctx context.Context, host, family string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if family == "4" && ip.To4() == nil {
			return nil, fmt.Errorf("host is not IPv4")
		}
		if family == "6" && ip.To4() != nil {
			return nil, fmt.Errorf("host is not IPv6")
		}
		return []net.IP{ip}, nil
	}
	key := family + "|" + host
	r.mu.Lock()
	if e, ok := r.m[key]; ok && time.Now().Before(e.expires) {
		r.mu.Unlock()
		return e.ips, nil
	}
	r.mu.Unlock()

	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var (
		ips []net.IP
		err error
	)
	switch family {
	case "4":
		ips, err = net.DefaultResolver.LookupIP(lookupCtx, "ip4", host)
	case "6":
		ips, err = net.DefaultResolver.LookupIP(lookupCtx, "ip6", host)
	default:
		ips, err = net.DefaultResolver.LookupIP(lookupCtx, "ip", host)
	}
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no address for %s", host)
	}
	// Prefer IPv4 for "auto" so nodes keep a stable address family.
	if family == "auto" {
		for i, ip := range ips {
			if ip.To4() != nil {
				ips[0], ips[i] = ips[i], ips[0]
				break
			}
		}
	}
	r.mu.Lock()
	r.m[key] = dnsEntry{ips: ips, expires: time.Now().Add(r.ttl)}
	r.mu.Unlock()
	return ips, nil
}
