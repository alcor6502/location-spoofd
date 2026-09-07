package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// portRange is the block of local source ports spoofd's own upstream connections are bound to
// when pf host-mode interception is on, so the pf rule can tell them apart from tailscaled's
// and not send them back into spoofd.
type portRange struct{ lo, hi int }

func parsePortRange(s string) (portRange, error) {
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return portRange{}, fmt.Errorf("want lo-hi, got %q", s)
	}
	lo, err1 := strconv.Atoi(strings.TrimSpace(a))
	hi, err2 := strconv.Atoi(strings.TrimSpace(b))
	if err1 != nil || err2 != nil || lo < 1024 || hi > 65535 || hi < lo {
		return portRange{}, fmt.Errorf("invalid port range %q", s)
	}
	return portRange{lo, hi}, nil
}

// upstreamDialer opens connections towards the Internet. Without a port range it is a plain
// dialer; with one, each connection is bound to the next free port in the range.
type upstreamDialer struct {
	ports portRange
	next  atomic.Int64
}

func (d *upstreamDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if d.ports.hi == 0 {
		return (&net.Dialer{Timeout: dialLimit}).DialContext(ctx, network, addr)
	}
	span := int64(d.ports.hi - d.ports.lo + 1)
	var lastErr error
	for i := int64(0); i < span; i++ {
		port := d.ports.lo + int(d.next.Add(1)%span)
		dl := &net.Dialer{Timeout: dialLimit, LocalAddr: &net.TCPAddr{Port: port}}
		c, err := dl.DialContext(ctx, network, addr)
		if err == nil {
			return c, nil
		}
		lastErr = err
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("no free upstream port in %d-%d: %w", d.ports.lo, d.ports.hi, lastErr)
}

func (d *upstreamDialer) Dial(network, addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dialLimit)
	defer cancel()
	return d.DialContext(ctx, network, addr)
}

var _ = time.Second
