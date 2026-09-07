#!/bin/sh
# Run on the pfSense box after copying spoofd and spoofd.conf.sample to /tmp.
set -e
cp /tmp/spoofd /usr/local/sbin/spoofd && chmod 755 /usr/local/sbin/spoofd
[ -f /usr/local/etc/spoofd.conf ] || cp /tmp/spoofd.conf.sample /usr/local/etc/spoofd.conf
mkdir -p /usr/local/etc/spoofd
echo "installed. Next:"
echo "  1. edit /usr/local/etc/spoofd.conf (lat, lon)"
echo "  2. start:  /usr/sbin/daemon -f -p /var/run/spoofd.pid /usr/local/sbin/spoofd -config /usr/local/etc/spoofd.conf"
echo "  3. boot:   add that same line in System > Advanced > Shellcmd (after your tailscaled restart)"
echo "  stop:      kill \$(cat /var/run/spoofd.pid)   (removes the pf redirect too)"
