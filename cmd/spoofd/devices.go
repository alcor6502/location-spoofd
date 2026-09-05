package main

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// deviceSwitch remembers which clients asked not to be spoofed. Their location queries are
// spliced to Apple untouched, so a phone can keep the exit node and still see where it really is.
// Keyed by client IP (the Tailscale address of the phone); persisted as one IP per line.
type deviceSwitch struct {
	mu       sync.Mutex
	path     string
	disabled map[string]bool
}

func newDeviceSwitch(dir string) *deviceSwitch {
	d := &deviceSwitch{path: filepath.Join(dir, "disabled-devices"), disabled: map[string]bool{}}
	if f, err := os.Open(d.path); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if ip := strings.TrimSpace(sc.Text()); ip != "" {
				d.disabled[ip] = true
			}
		}
	}
	return d
}

// Enabled reports whether spoofing applies to the client behind addr.
func (d *deviceSwitch) Enabled(addr net.Addr) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.disabled[clientIP(addr)]
}

// Set turns spoofing on or off for one client and persists the change.
func (d *deviceSwitch) Set(addr net.Addr, enabled bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ip := clientIP(addr)
	if enabled {
		delete(d.disabled, ip)
	} else {
		d.disabled[ip] = true
	}
	ips := make([]string, 0, len(d.disabled))
	for ip := range d.disabled {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	_ = os.WriteFile(d.path, []byte(strings.Join(ips, "\n")+"\n"), 0o644)
}

func clientIP(addr net.Addr) string {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}
