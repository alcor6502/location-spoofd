#!/bin/sh
# spoofd launcher for pfSense (FreeBSD/pf), tested on pfSense 2.9 with the Tailscale package
# in TUN mode (tailscaled -tun tailscale0).
#
# Install as /usr/local/etc/rc.d/spoofd.sh and run it at boot with the Shellcmd package
# (command: /usr/local/etc/rc.d/spoofd.sh start). Reads /usr/local/etc/spoofd.conf.
#
# Besides the daemon it loads the pf redirect into pfSense's own extension anchors
# (natearly/spoofd, userrules/spoofd), which pfSense keeps across ruleset reloads. If they
# ever get flushed, run `spoofd.sh pf` again; until then phones simply get their real
# position (fail-open).
#
# usage: spoofd.sh start|stop|restart|status|pf|unpf

CONF=/usr/local/etc/spoofd.conf
BIN=/usr/local/sbin/spoofd
PIDFILE=/var/run/spoofd.pid
DATADIR=/usr/local/etc/spoofd

LAT=""; LON=""; ALT=0; HACC=5; VACC=3
TLS_PORT=18443; HTTP_PORT=18080
POLITE=1; ENABLED=1
TS_IF=tailscale0
[ -f "$CONF" ] && . "$CONF"

load_pf() {
	# Redirect exit-node clients' HTTPS towards Apple's block to spoofd, and let it in.
	printf 'rdr pass on %s inet proto tcp from any to 17.0.0.0/8 port 443 -> 127.0.0.1 port %s\n' \
		"$TS_IF" "$TLS_PORT" | pfctl -a natearly/spoofd -f - || return 1
	printf 'pass in quick on %s inet proto tcp from any to 127.0.0.1 port %s flags S/SA keep state\npass in quick on %s inet proto tcp from any to (self) port %s flags S/SA keep state\n' \
		"$TS_IF" "$TLS_PORT" "$TS_IF" "$HTTP_PORT" | pfctl -a userrules/spoofd -f - || return 1
	echo "spoofd: pf redirect loaded ($TS_IF -> 17.0.0.0/8:443 => :$TLS_PORT)"
}

unload_pf() {
	pfctl -a natearly/spoofd -F nat 2>/dev/null
	pfctl -a userrules/spoofd -F rules 2>/dev/null
	echo "spoofd: pf redirect removed"
}

start() {
	[ "$ENABLED" = 1 ] || { echo "spoofd: disabled in $CONF"; return 0; }
	if [ -z "$LAT" ] || [ -z "$LON" ]; then
		echo "spoofd: set LAT and LON in $CONF first"; return 1
	fi
	if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
		echo "spoofd: already running"
	else
		mkdir -p "$DATADIR"
		POLITE_FLAG=true; [ "$POLITE" = 1 ] || POLITE_FLAG=false
		/usr/sbin/daemon -f -p "$PIDFILE" -o /var/log/spoofd.log "$BIN" \
			-lat "$LAT" -lon "$LON" -alt "$ALT" -hacc "$HACC" -vacc "$VACC" \
			-listen ":$TLS_PORT" -http ":$HTTP_PORT" -ca-dir "$DATADIR" -polite="$POLITE_FLAG"
		echo "spoofd: started ($LAT, $LON), log in /var/log/spoofd.log"
	fi
	load_pf
}

stop() {
	unload_pf
	if [ -f "$PIDFILE" ]; then
		kill "$(cat "$PIDFILE")" 2>/dev/null; rm -f "$PIDFILE"; echo "spoofd: stopped"
	else
		echo "spoofd: not running"
	fi
}

status() {
	if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
		echo "spoofd: running (pid $(cat "$PIDFILE")), location $LAT, $LON"
	else
		echo "spoofd: not running"
	fi
	if pfctl -a natearly/spoofd -s nat 2>/dev/null | grep -q rdr; then
		echo "pf redirect: loaded"
	else
		echo "pf redirect: NOT loaded (run: $0 pf)"
	fi
}

case "$1" in
	start|quietstart|faststart|onestart|forcestart) start ;;
	stop|quietstop|faststop|onestop|forcestop) stop ;;
	restart) stop; sleep 1; start ;;
	status) status ;;
	pf) load_pf ;;
	unpf) unload_pf ;;
	*) echo "usage: $0 start|stop|restart|status|pf|unpf"; exit 1 ;;
esac
