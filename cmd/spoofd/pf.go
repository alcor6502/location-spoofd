package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// pf integration for FreeBSD/pfSense: spoofd loads its own redirect into anchors that pfSense
// attaches to its main ruleset and keeps across reloads, and removes them on shutdown. Enabled
// with -pf <interface>. Fail-open: if the anchors are ever flushed, phones just get their real
// position until spoofd is restarted.
//
// On FreeBSD, rdr rules inside an anchor are only evaluated through an `rdr-anchor` attachment
// (nat-anchor does not include them). pfSense's ruleset has exactly one: `rdr-anchor
// "tftp-proxy/*"`, so the redirect lives in a sub-anchor there; the pass rules go into
// `anchor "userrules/*"`.
const (
	pfNatAnchor   = "tftp-proxy/spoofd"
	pfRulesAnchor = "userrules/spoofd"
)

func pfLoad(iface, tlsPort, httpPort string) error {
	nat := fmt.Sprintf("rdr pass on %s inet proto tcp from any to 17.0.0.0/8 port 443 -> 127.0.0.1 port %s\n", iface, tlsPort)
	rules := fmt.Sprintf("pass in quick on %s inet proto tcp from any to 127.0.0.1 port %s flags S/SA keep state\n"+
		"pass in quick on %s inet proto tcp from any to (self) port %s flags S/SA keep state\n", iface, tlsPort, iface, httpPort)
	if err := pfctl(nat, "-a", pfNatAnchor, "-f", "-"); err != nil {
		return fmt.Errorf("loading nat anchor: %w", err)
	}
	if err := pfctl(rules, "-a", pfRulesAnchor, "-f", "-"); err != nil {
		return fmt.Errorf("loading rules anchor: %w", err)
	}
	log.Printf("pf: redirect loaded on %s (17.0.0.0/8:443 -> :%s)", iface, tlsPort)
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
