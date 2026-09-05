//go:build !linux

package main

import (
	"fmt"
	"net"
)

// originalDst is only available behind Linux netfilter DNAT; elsewhere spoofd falls back to the SNI.
func originalDst(net.Conn) (string, error) {
	return "", fmt.Errorf("SO_ORIGINAL_DST unsupported on this platform")
}
