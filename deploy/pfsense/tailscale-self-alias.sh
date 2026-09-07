#!/bin/sh
# Keeps a pfSense host alias equal to this box's current Tailscale IPv4, so outbound NAT and
# any rule that must name "this node's tailnet address" never go stale when the node is
# re-registered. Creates the alias if missing, updates it if the IP changed, reloads the
# filter only when something changed.
#
# Boot: System > Advanced > Shellcmd (or the Shellcmd package), right after the tailscaled
# restart:   /usr/local/etc/rc.d/tailscale-self-alias.sh
# Cron-safe as well: harmless when nothing changed.

ALIAS=${1:-Tailscale_Self}

# Wait for tailscaled to hand out an address (up to ~60 s after boot).
i=0
while [ $i -lt 30 ]; do
	IP=$(/usr/local/bin/tailscale ip -4 2>/dev/null | head -1)
	[ -n "$IP" ] && break
	sleep 2; i=$((i+1))
done
[ -n "$IP" ] || { logger -t tailscale-self-alias "no tailscale IPv4 yet, giving up"; exit 1; }

/usr/local/sbin/pfSsh.php playback 2>/dev/null <<PHP
require_once("config.inc");
require_once("filter.inc");
\$name = "$ALIAS";
\$ip = "$IP";
\$aliases = config_get_path("aliases/alias", []);
\$changed = false; \$found = false;
foreach (\$aliases as \$k => \$a) {
	if (\$a["name"] === \$name) {
		\$found = true;
		if (\$a["address"] !== \$ip) { \$aliases[\$k]["address"] = \$ip; \$changed = true; }
	}
}
if (!\$found) {
	\$aliases[] = ["name" => \$name, "type" => "host", "address" => \$ip,
		"descr" => "This box's Tailscale IPv4 (kept current by tailscale-self-alias.sh)", "detail" => "self"];
	\$changed = true;
}
if (\$changed) {
	config_set_path("aliases/alias", \$aliases);
	write_config("tailscale-self-alias: \$name = \$ip");
	filter_configure();
	echo "updated \$name to \$ip\n";
} else {
	echo "\$name already \$ip\n";
}
PHP
