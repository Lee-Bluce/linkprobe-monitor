package probe

import (
	"context"
	"math/rand"
	"net"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

var payload = []byte("linkprobe-echo-0123456789abcdef0123456789abcdef0123456789abcdef")

func icmpChecksumProto(is4 bool) int {
	if is4 {
		return 1 // IPPROTO_ICMP
	}
	return 58 // IPPROTO_ICMPV6
}

func icmpProbe(ctx context.Context, ip net.IP, count int, timeout, gap time.Duration) Result {
	res := Result{}
	is4 := ip.To4() != nil
	network, address := "ip4:icmp", "0.0.0.0"
	if !is4 {
		network, address = "ip6:ipv6-icmp", "::"
	}
	conn, err := icmp.ListenPacket(network, address)
	unprivileged := false
	if err != nil {
		// Fall back to Linux ping sockets (net.ipv4.ping_group_range). The
		// kernel rewrites the echo ID to the socket port, so ID matching is
		// skipped in that mode.
		if is4 {
			conn, err = icmp.ListenPacket("udp4", "0.0.0.0")
		} else {
			conn, err = icmp.ListenPacket("udp6", "::")
		}
		unprivileged = true
		if err != nil {
			return Result{Sent: count, Reason: "perm"}
		}
	}
	defer conn.Close()

	id := uint16(rand.Intn(0xffff))
	echoType := icmp.Type(ipv4.ICMPTypeEcho)
	replyType := icmp.Type(ipv4.ICMPTypeEchoReply)
	if !is4 {
		echoType = ipv6.ICMPTypeEchoRequest
		replyType = ipv6.ICMPTypeEchoReply
	}
	proto := icmpChecksumProto(is4)
	buf := make([]byte, 1500)
	rtts := make([]int64, 0, count)

	for seq := 0; seq < count; seq++ {
		if ctx.Err() != nil {
			break
		}
		if seq > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(gap):
			}
			if ctx.Err() != nil {
				break
			}
		}
		msg := icmp.Message{
			Type: echoType,
			Code: 0,
			Body: &icmp.Echo{ID: int(id), Seq: seq, Data: payload},
		}
		b, err := msg.Marshal(nil)
		if err != nil {
			continue
		}
		start := time.Now()
		if _, err := conn.WriteTo(b, &net.IPAddr{IP: ip}); err != nil {
			res.Sent++
			res.Reason = "send"
			continue
		}
		res.Sent++
		deadline := start.Add(timeout)
		for {
			_ = conn.SetReadDeadline(deadline)
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				break
			}
			m, err := icmp.ParseMessage(proto, buf[:n])
			if err != nil || m.Type != replyType {
				continue
			}
			echo, ok := m.Body.(*icmp.Echo)
			if !ok || echo.Seq != seq {
				continue
			}
			if !unprivileged && echo.ID != int(id) {
				continue
			}
			rtts = append(rtts, time.Since(start).Microseconds())
			res.Recv++
			break
		}
	}
	if res.Recv > 0 {
		res.RttMinUs, res.RttAvgUs, res.RttMaxUs = summarize(rtts)
		res.Reason = ""
	} else if res.Reason == "" {
		res.Reason = "timeout"
	}
	return res
}

func summarize(rtts []int64) (min, avg, max int64) {
	if len(rtts) == 0 {
		return 0, 0, 0
	}
	min, max = rtts[0], rtts[0]
	var sum int64
	for _, v := range rtts {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
		sum += v
	}
	return min, sum / int64(len(rtts)), max
}
