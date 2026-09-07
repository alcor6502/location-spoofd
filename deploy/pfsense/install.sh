#!/bin/sh
# Run on the pfSense box after copying spoofd, spoofd.sh and spoofd.conf.sample to /tmp.
set -e
cp /tmp/spoofd /usr/local/sbin/spoofd && chmod 755 /usr/local/sbin/spoofd
cp /tmp/spoofd.sh /usr/local/etc/rc.d/spoofd.sh && chmod 755 /usr/local/etc/rc.d/spoofd.sh
[ -f /usr/local/etc/spoofd.conf ] || cp /tmp/spoofd.conf.sample /usr/local/etc/spoofd.conf
mkdir -p /usr/local/etc/spoofd
echo "installed. Next:"
echo "  1. edit /usr/local/etc/spoofd.conf (LAT, LON)"
echo "  2. /usr/local/etc/rc.d/spoofd.sh start"
echo "  3. Services > Shellcmd: add '/usr/local/etc/rc.d/spoofd.sh start' (type shellcmd) so it runs at boot"
