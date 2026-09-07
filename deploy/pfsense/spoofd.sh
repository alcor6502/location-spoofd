#!/bin/sh
# spoofd launcher for pfSense / FreeBSD.
# Install as /usr/local/etc/rc.d/spoofd.sh — pfSense runs every *.sh here at boot.
# Reads /usr/local/etc/spoofd.conf. Usage: spoofd.sh start|stop|restart|status
#
# The pf redirect (rdr) is NOT created here; add it in the pfSense GUI or pf config,
# see docs/pfsense-setup.md.

CONF=/usr/local/etc/spoofd.conf
BIN=/usr/local/sbin/spoofd
PIDFILE=/var/run/spoofd.pid
DATADIR=/var/db/spoofd

# Defaults, overridden by spoofd.conf
LAT=""
LON=""
ALT=0
HACC=5
VACC=3
TLS_PORT=18443
HTTP_PORT=18080
POLITE=1
ENABLED=1
[ -f "$CONF" ] && . "$CONF"

start() {
	[ "$ENABLED" = 1 ] || { echo "spoofd: disabled in $CONF"; return 0; }
	if [ -z "$LAT" ] || [ -z "$LON" ]; then
		echo "spoofd: set LAT and LON in $CONF first"; return 1
	fi
	if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
		echo "spoofd: already running"; return 0
	fi
	mkdir -p "$DATADIR"
	POLITE_FLAG=true; [ "$POLITE" = 1 ] || POLITE_FLAG=false
	/usr/sbin/daemon -f -p "$PIDFILE" "$BIN" \
		-lat "$LAT" -lon "$LON" -alt "$ALT" -hacc "$HACC" -vacc "$VACC" \
		-listen ":$TLS_PORT" -http ":$HTTP_PORT" -ca-dir "$DATADIR" -polite="$POLITE_FLAG"
	echo "spoofd: started ($LAT, $LON)"
}

stop() {
	if [ -f "$PIDFILE" ]; then
		kill "$(cat "$PIDFILE")" 2>/dev/null
		rm -f "$PIDFILE"
		echo "spoofd: stopped"
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
}

case "$1" in
	start|quietstart|faststart|onestart|forcestart) start ;;
	stop|quietstop|faststop|onestop|forcestop) stop ;;
	restart) stop; sleep 1; start ;;
	status) status ;;
	*) echo "usage: $0 start|stop|restart|status"; exit 1 ;;
esac
