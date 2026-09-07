# pfSense setup

pfSense is FreeBSD with the `pf` firewall. The daemon is the same as on OpenWrt; what differs
is how the redirect is installed and how the service starts. Written against pfSense 2.9 with
the **Tailscale package** (0.1.9) in its default TUN mode (`tailscaled -tun tailscale0`).

If any concept here is unfamiliar — exit node, certificate, why the router sees the traffic —
read [how-it-works.md](how-it-works.md) first.

## What pfSense gives us, and what it does not

- The Tailscale package creates a real interface, `tailscale0`, that pf can match on. pfSense
  shows it as **Tailscale** in Firewall › Rules (the package adds the tab), with the rules you
  wrote for it — typically "allow the hosts in an alias".
- It does **not** offer `tailscale0` in the NAT › Port Forward interface list, and anything
  typed by hand into the pf ruleset is lost the next time pfSense regenerates it.
- It does provide extension points for exactly this: the anchors `natearly` and `userrules`
  are attached to the main ruleset and are kept across reloads. `spoofd.sh` loads its two rules
  there. Should they ever be flushed (a full `pfctl -F all`), phones simply get their real
  position until `spoofd.sh pf` runs again — the failure mode is open, never broken positioning.
- The **Shellcmd** package (System › Package Manager) is the supported way to run a command at
  boot; it survives upgrades where a stray rc.d script might not.

## 1. Tailscale exit node

System › Package Manager › install **Tailscale**. In VPN › Tailscale › Settings: enable, tick
**Advertise Exit Node**, save. Approve the exit node in the Tailscale admin console.

Check the firewall rules on the **Tailscale** tab: if they allow only an alias of hosts (the
package's example does), the phones that will use this box as exit node must be in that alias,
or they cannot get out at all — spoofed or not.

## 2. Install spoofd

Get `spoofd-freebsd-amd64` from the [Releases](https://github.com/alcor6502/location-spoofd/releases)
page (pfSense on ordinary hardware and VMs is `amd64`; `uname -m` says so), then from the repo:

```sh
scp spoofd-freebsd-amd64 admin@<pfsense>:/tmp/spoofd
scp deploy/pfsense/spoofd.sh deploy/pfsense/spoofd.conf.sample deploy/pfsense/install.sh admin@<pfsense>:/tmp/
ssh admin@<pfsense> sh /tmp/install.sh
```

(`admin` lands in pfSense's console menu; option **8** gives a shell. SSH must be enabled in
System › Advanced › Secure Shell.)

Edit `/usr/local/etc/spoofd.conf` — coordinates in decimal degrees, right-click a point in
Google or Apple Maps:

```sh
LAT="48.858370"
LON="2.294481"
ALT=35
```

Start and check:

```sh
/usr/local/etc/rc.d/spoofd.sh start
/usr/local/etc/rc.d/spoofd.sh status
```

`status` must say both *running* and *pf redirect: loaded*. The log is `/var/log/spoofd.log`.

To start at boot: Services › Shellcmd › Add, command `/usr/local/etc/rc.d/spoofd.sh start`,
type *shellcmd*.

## 3. What the redirect is

Two rules, loaded by the script into pfSense's anchors, nothing in the GUI:

```
natearly/spoofd:   rdr pass on tailscale0 inet proto tcp from any to 17.0.0.0/8 port 443 -> 127.0.0.1 port 18443
userrules/spoofd:  pass in quick on tailscale0 proto tcp to 127.0.0.1 port 18443
                   pass in quick on tailscale0 proto tcp to (self) port 18080
```

Only traffic arriving from the Tailscale interface (exit-node clients), only TCP 443, only
towards Apple's `17.0.0.0/8`. `spoofd` then inspects the server name and splices every host
that is not the location service straight through. `spoofd.sh unpf` removes both rules;
`spoofd.sh stop` removes them and stops the daemon.

## 4. Set up each phone (once)

With this box selected as exit node in the Tailscale app:

1. Safari → `http://<pfsense tailscale IP>:18080/ca.crt` → allow the profile download.
2. Settings › General › VPN & Device Management → install **Location Spoofer CA**.
3. Settings › General › About › Certificate Trust Settings → enable it. Without this step the
   TLS handshake fails and nothing happens.
4. Settings › Privacy & Security › Location Services → off, wait ten seconds, on.

Open Maps. Everything from here — daily use, the per-device switch on the status page, polite
mode, time zone, tuning `HACC`/`VACC`, renewing the CA — is as in
[openwrt-setup.md](openwrt-setup.md); only the config file (`/usr/local/etc/spoofd.conf`, then
`spoofd.sh restart`) replaces `uci`, and `spoofd.sh start|stop|status` replaces `spoofctl`.

## Notes

- **Original destination**: on Linux `spoofd` recovers the pre-redirect address with
  `SO_ORIGINAL_DST`; the FreeBSD equivalent (`DIOCNATLOOK` on `/dev/pf`) is not implemented.
  Spliced hosts are reached by their SNI name instead, which for Apple's hosts resolves to the
  same servers. Normal use is unaffected.
- **Ports** 18443/18080 avoid pfSense's own web GUI; change them in the conf if needed (the
  script derives the pf rules from the conf).
