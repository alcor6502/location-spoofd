# Router setup (OpenWrt / GL.iNet)

Tested on a GL.iNet GL-MT5000 (Brume 3, OpenWrt 21.02, fw3/iptables). Any OpenWrt router that
runs Tailscale as an exit node should work.

## What this does, in one paragraph

Your phone, when it travels through your home router (as a Tailscale **exit node** — a device
that routes all of another device's Internet traffic), asks Apple where it is. This installs a
small program, `spoofd`, on the router that answers that question with a fixed location of your
choosing. You install one certificate on the phone so it trusts the answer. Nothing else runs
on the phone. If any of these words are new — exit node, certificate, why the router can see
the traffic — read [how-it-works.md](how-it-works.md) first; it assumes no prior knowledge.

## Words you will meet here

- **SSH** — a way to type commands on the router from your computer's terminal:
  `ssh root@<router address>`, then the router's password. On the GL.iNet the address is
  usually `192.168.8.1` on its own network, or its Tailscale address from anywhere.
- **Tailscale IP** — the `100.x.y.z` address the router has inside your private Tailscale
  network; stable, reachable from your phone wherever you are.
- **coordinates** — latitude and longitude in decimal degrees, e.g. `48.858370, 2.294481`.
  Right-click a spot in Google Maps or Apple Maps and it shows them; copy the two numbers.

## Requirements

- Tailscale installed on the router and advertising itself as **exit node**
  (GL.iNet: Applications › Tailscale; enable the exit node with `tailscale up --advertise-exit-node`
  if the UI does not expose it).
- SSH access as root.
- The right `spoofd` binary for your router's CPU. Run `uname -m` on the router (over SSH):
  `aarch64` → `arm64`, `armv7l` → `arm`, `x86_64` → `amd64`, `mips` → `mipsle`. The GL-MT5000
  is `arm64`.

## Install

**If you have Go installed** (developers): from this repository on your computer,

```sh
make deploy ROUTER=root@192.168.8.1        # ARCH=arm64 by default; set ARCH= for others
```

**Without Go** (anyone): download the matching `spoofd-linux-<arch>` from the
[Releases](https://github.com/alcor6502/location-spoofd/releases) page, then from a terminal
in your Downloads folder:

```sh
scp -O spoofd-linux-arm64 root@192.168.8.1:/tmp/spoofd
scp -O deploy/openwrt/* root@192.168.8.1:/tmp/          # the five files from this repo
ssh root@192.168.8.1 sh /tmp/install.sh
```

(`deploy/openwrt/` holds `spoofd.init`, `spoofd.config`, `firewall.spoofd`, `spoofctl` and
`install.sh` — five small text files; grab them from this repo's `deploy/openwrt/` folder.)

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
- **Exit node on, real position**: open `http://<router>:18080/` on the phone and tap
  *Disable spoofing for this device*, then Location Services off/on. The phone keeps the home
  IP but its positioning queries go to Apple untouched; other devices are not affected. The
  same page re-enables it. The switch survives router reboots.

  From a Shortcut: *Get Contents of URL* → `http://<router>:18080/device`, method POST, form
  field `spoof` = `on` or `off`.

Do **not** "switch off" by disabling the certificate trust: the TLS handshake with `spoofd`
then fails and `locationd` cannot reach Apple at all, so the phone loses WiFi/cell
positioning entirely instead of getting its real one (only GPS remains).

Shortcuts can automate the exit node (`Use exit node` / `Stop using exit node` actions) and
airplane mode. No app or shortcut can toggle Location Services; the best a shortcut can do is
open Settings › Privacy › Location Services for you.

## Devices without a SIM

An iPad (or iPhone) with no active SIM/eSIM never queries cell towers, so it has only the WiFi
fix to set against GPS. It still holds the spoofed position indoors once the neighbourhood is
learned, but gives in to GPS more easily near windows or skylights. Nothing on the router can
change that; a device with cellular data on gets the cell fix as a second vote.

## Polite mode: nothing wrong is reported back

iPhones feed Apple's WiFi/cell database with what they see. A spoofed phone would be
reporting real access points at a fake place, so by default `spoofd` intercepts those uploads
(`/hvr/` on `gsp10-ssl` and `gsp64-ssl.ls.apple.com`) from spoofed devices, discards them and
replies `200 OK`. The status page counts them as *uploads swallowed*. Devices with spoofing
off are normal phones. `uci set spoofd.main.polite=0` turns this off. Details and how the
endpoint was found: [how-it-works.md](how-it-works.md).

With polite mode you can leave Settings › Privacy & Security › Location Services › System
Services › *Improve Location Accuracy* as it is.

## Time zone

iOS sets the time zone from the location when Settings › General › Date & Time › *Set
Automatically* is on, so a spoofed phone will move its clock to the spoofed zone by itself
(the time itself is NTP and stays correct). That is coherent with the position and usually
what you want. The update is not immediate: the *Setting Time Zone* system service only
re-evaluates on significant location changes and on its own schedule. To force it right away,
Settings › General › Date & Time › *Set Automatically* off, then on. To keep your real clock while spoofed, turn off Settings › Privacy & Security ›
Location Services › System Services › *Setting Time Zone* and set the zone by hand.

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

`hacc` is how precise the fix claims to be: the smaller the number, the more iOS trusts it
over a weak GPS reading. The default 5 is what a real, well-observed WiFi fix looks like and
is plausible. If a device keeps drifting back to its true position near a window or skylight,
lowering `hacc` (and `vacc`) to `1` — the smallest value; `0` means "unknown" and can be
discarded — makes the WiFi fix win more often:

```sh
uci set spoofd.main.hacc=1; uci set spoofd.main.vacc=1; uci commit spoofd
/etc/init.d/spoofd restart
```

Do not expect miracles: `hacc` only sets how much iOS *weighs* the WiFi fix, it does not move
the point, and it cannot beat a strong open-sky GPS fix. The real confidence comes from every
access point (and cell) being placed at the same spot.

## Renewing the CA

`rm /etc/spoofd/*.pem && /etc/init.d/spoofd restart`, then remove the old profile on each phone
and repeat the four setup steps.
