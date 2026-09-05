#!/bin/sh
# Run on the router after copying spoofd, spoofd.init, spoofd.config, firewall.spoofd and spoofctl to /tmp.
set -e
cp /tmp/spoofd /usr/bin/spoofd && chmod 755 /usr/bin/spoofd
cp /tmp/spoofd.init /etc/init.d/spoofd && chmod 755 /etc/init.d/spoofd
cp /tmp/spoofd.config /etc/config/spoofd.new; [ -f /etc/config/spoofd ] || mv /etc/config/spoofd.new /etc/config/spoofd
cp /tmp/firewall.spoofd /etc/firewall.spoofd
cp /tmp/spoofctl /usr/bin/spoofctl && chmod 755 /usr/bin/spoofctl
grep -q firewall.spoofd /etc/firewall.user || printf '\n# spoofd (Apple location MITM for Tailscale exit-node clients)\n. /etc/firewall.spoofd\n' >> /etc/firewall.user
/etc/init.d/spoofd enable
if [ -z "$(uci -q get spoofd.main.lat)" ]; then
	echo "Set your coordinates first:"
	echo "  uci set spoofd.main.lat=48.858370; uci set spoofd.main.lon=2.294481; uci commit spoofd"
	echo "then run: spoofctl on"
	exit 0
fi
/etc/init.d/spoofd restart
/etc/init.d/firewall restart >/dev/null 2>&1
sleep 2
echo "--- spoofd process:"; pgrep -f /usr/bin/spoofd >/dev/null && echo running || echo NOT RUNNING
echo "--- nat rule:"; iptables -t nat -S prerouting_rule | grep 18443 || echo "NAT RULE MISSING"
echo "--- input rules:"; iptables -S input_tailscale0_rule | grep -c ACCEPT
echo "--- log:"; logread | grep spoofd | tail -5
