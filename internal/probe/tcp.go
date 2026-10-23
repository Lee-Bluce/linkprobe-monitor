package probe

import (
	"context"
	"errors"
	"net"
	"strconv"
	"syscall"
	"time"
)

func tcpProbe(ctx context.Context, ip net.IP, port, count int, timeout, gap time.Duration) Result {
	res := Result{}
	network := "tcp4"
	if ip.To4() == nil {
		network = "tcp6"
	}
	addr := net.JoinHostPort(ip.String(), strconv.Itoa(port))
	rtts := make([]int64, 0, count)

	dialer := net.Dialer{Timeout: timeout}
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
		start := time.Now()
		conn, err := dialer.DialContext(ctx, network, addr)
		rtt := time.Since(start)
		res.Sent++
		if err != nil {
			if res.Reason == "" {
				res.Reason = classifyDialError(err)
			}
			continue
		}
		// RST close: no TIME_WAIT pile-up on either side.
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetLinger(0)
		}
		conn.Close()
		rtts = append(rtts, rtt.Microseconds())
		res.Recv++
	}
	if res.Recv > 0 {
		res.RttMinUs, res.RttAvgUs, res.RttMaxUs = summarize(rtts)
		res.Reason = ""
	}
	return res
}

func classifyDialError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "refused"
	}
	if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return "unreach"
	}
	return "error"
}
