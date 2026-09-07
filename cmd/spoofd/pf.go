package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// pf integration for FreeBSD/pfSense. Enabled with -pf <interface>; spoofd loads its rules into
// anchors pfSense attaches to its main ruleset and keeps across reloads, and removes them on
// shutdown. Fail-open: if the anchors are ever flushed, phones just get their real position.
//
// Two ways traffic can reach Apple from an exit node on FreeBSD, both covered:
//
//  1. Kernel forwarding: packets arrive on the Tailscale interface and pf forwards them.
//     A plain `rdr on <iface>` catches them.
//  2. Userspace forwarding (what tailscaled does on pfSense): the daemon decrypts the
//     client's packets and opens the connections itself, from the host. They never cross
//     the interface. pf can still divert a *locally originated* connection: a `pass out
//     route-to (lo0 127.0.0.1)` sends it through the loopback, where `rdr on lo0` rewrites
//     it to spoofd. `user root` limits this to the host's own sockets (tailscaled runs as
//     root; forwarded LAN traffic has no socket owner and is untouched). spoofd's own
//     upstream connections to Apple are excluded by source port: they are bound to the
//     range in -pf-ports.
//
// pfSense evaluates `rdr` only through an `rdr-anchor` (its only one is "tftp-proxy/*");
// the pass rules go into `anchor "userrules/*"`.
const (
	pfNatAnchor   = "tftp-proxy/spoofd"
	pfRulesAnchor = "userrules/spoofd"
)

func pfLoad(iface, tlsPort, httpPort string, ports portRange) error {
	nat := fmt.Sprintf(
		"rdr pass on %s inet proto tcp from any to 17.0.0.0/8 port 443 -> 127.0.0.1 port %s\n"+
			"rdr pass on lo0 inet proto tcp from any to 17.0.0.0/8 port 443 -> 127.0.0.1 port %s\n",
		iface, tlsPort, tlsPort)
	rules := fmt.Sprintf(
		"pass in quick on %s inet proto tcp from any to 127.0.0.1 port %s flags S/SA keep state\n"+
			"pass in quick on %s inet proto tcp from any to (self) port %s flags S/SA keep state\n"+
			"pass out quick route-to (lo0 127.0.0.1) inet proto tcp from any port %d <> %d to 17.0.0.0/8 port 443 user root flags S/SA keep state\n",
		iface, tlsPort, iface, httpPort, ports.lo-1, ports.hi+1)
	if err := pfctl(nat, "-a", pfNatAnchor, "-f", "-"); err != nil {
		return fmt.Errorf("loading nat anchor: %w", err)
	}
	if err := pfctl(rules, "-a", pfRulesAnchor, "-f", "-"); err != nil {
		return fmt.Errorf("loading rules anchor: %w", err)
	}
	log.Printf("pf: redirect loaded (in on %s, and host-originated via lo0; upstream ports %d-%d excluded) -> :%s", iface, ports.lo, ports.hi, tlsPort)
	return nil
}

func pfUnload() {
	_ = pfctl("", "-a", pfNatAnchor, "-F", "nat")
	_ = pfctl("", "-a", pfRulesAnchor, "-F", "rules")
	log.Printf("pf: redirect removed")
}

func pfctl(stdin string, args ...string) error {
	cmd := exec.Command("/sbin/pfctl", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pfctl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
