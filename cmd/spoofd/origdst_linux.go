package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
)

// originalDst recovers the pre-DNAT destination of a redirected connection (SO_ORIGINAL_DST),
// so non-Apple traffic can be spliced to where the phone actually wanted to go.
func originalDst(c net.Conn) (string, error) {
	tcp, ok := c.(*net.TCPConn)
	if !ok {
		return "", fmt.Errorf("not a TCP connection")
	}
	raw, err := tcp.SyscallConn()
	if err != nil {
		return "", err
	}
	var addr string
	var sockErr error
	err = raw.Control(func(fd uintptr) {
		const soOriginalDst = 80 // linux/netfilter_ipv4.h
		mreq, e := syscall.GetsockoptIPv6Mreq(int(fd), syscall.IPPROTO_IP, soOriginalDst)
		if e != nil {
			sockErr = e
			return
		}
		// sockaddr_in: family(2) port(2, big-endian) addr(4)
		port := binary.BigEndian.Uint16(mreq.Multiaddr[2:4])
		ip := net.IPv4(mreq.Multiaddr[4], mreq.Multiaddr[5], mreq.Multiaddr[6], mreq.Multiaddr[7])
		addr = net.JoinHostPort(ip.String(), fmt.Sprint(port))
	})
	if err != nil {
		return "", err
	}
	return addr, sockErr
}
