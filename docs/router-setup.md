# Router setup (OpenWrt / GL.iNet)

Tested on a GL.iNet GL-MT5000 (Brume 3, OpenWrt 21.02, fw3/iptables). Any OpenWrt router that
runs Tailscale as an exit node should work.

## Requirements

- Tailscale installed on the router and advertising itself as **exit node**
  (GL.iNet: Applications › Tailscale; enable the exit node with `tailscale up --advertise-exit-node`
  if the UI does not expose it).
- SSH access as root.
- A router binary: `make spoofd` builds `dist/spoofd-linux-{arm64,arm,amd64,mipsle}`, or take
  one from the Releases page. `cat /etc/openwrt_release` tells you the architecture.

## Install

From your Mac/PC:

```sh
make deploy ROUTER=root@192.168.8.1        # ARCH=arm64 by default
```

or by hand: copy `dist/spoofd-linux-<arch>` as `/tmp/spoofd` and the five files in
`deploy/openwrt/` to `/tmp/`, then `sh /tmp/install.sh` on the router.

Then set your coordinates (decimal degrees; right-click a point in Google Maps or Apple Maps)
and start:

```sh
uci set spoofd.main.lat=48.858370
uci set spoofd.main.lon=2.294481
uci set spoofd.main.alt=35            # metres, optional
uci commit spoofd
spoofctl on
```

`spoofctl status` shows whether the daemon and the NAT rule are both in place.

### What install.sh does

| Item | Path |
|------|------|
| daemon | `/usr/bin/spoofd` |
| procd service | `/etc/init.d/spoofd` (enabled, respawns) |
| settings | `/etc/config/spoofd` (uci: lat, lon, alt, hacc, vacc, ports, enabled) |
| firewall | `/etc/firewall.spoofd`, sourced from `/etc/firewall.user` |
| switch | `/usr/bin/spoofctl` |
| CA | `/etc/spoofd/ca.pem`, `ca-key.pem` (generated on first start, valid 10 years) |

The firewall rule: `-t nat -A prerouting_rule -i tailscale0 -p tcp -d 17.0.0.0/8 --dport 443 -j REDIRECT --to-ports 18443`.
Only exit-node clients are affected, only towards Apple's address block, and non-location hosts
inside that block are passed through untouched.

Ports 18443/18080 are used because GL.iNet's `uhttpd` already owns 8080/8443.

## Set up each phone (once)

With the router selected as exit node in the Tailscale app:

1. Safari → `http://<router tailscale IP>:18080/ca.crt` → Allow the profile download.
2. Settings › General › VPN & Device Management → install **Location Spoofer CA**.
3. Settings › General › About › **Certificate Trust Settings** → enable it. Without this step
   nothing happens: the TLS handshake fails and the log says so.
4. Settings › Privacy & Security › Location Services → off, wait ten seconds, on.
5. Open Maps.

The status page at `http://<router>:18080/` shows counters; `logread -f | grep spoofd` on the
router shows each intercepted query.

## Daily use

- **Spoofed**: exit node on, then Location Services off/on.
- **Real position**: exit node off, then airplane mode on/off (or Location Services off/on).

Shortcuts can automate the exit node (`Use exit node` / `Stop using exit node` actions) and
airplane mode. No app or shortcut can toggle Location Services; the best a shortcut can do is
open Settings › Privacy › Location Services for you.

## Turning it off

`spoofctl off` stops the daemon **and** removes the NAT rule, so exit-node clients get real
positioning again. Stopping only the service would leave the redirect pointing at a closed port.

Do not toggle it while a phone is connected and spoofed: during the "off" window iOS caches the
real position, and the phone needs another Location Services off/on afterwards.

## Tuning

| uci option | default | meaning |
|------------|---------|---------|
| `hacc` | 5 | horizontal accuracy reported for every access point, metres |
| `vacc` | 3 | vertical accuracy, metres |
| `alt` | 0 | altitude, metres |

Accuracy is an integer; 1 is the smallest meaningful value. It matters less than it looks:
the confidence of the fix comes from all access points being at the same point.

## Renewing the CA

`rm /etc/spoofd/*.pem && /etc/init.d/spoofd restart`, then remove the old profile on each phone
and repeat the four setup steps.
