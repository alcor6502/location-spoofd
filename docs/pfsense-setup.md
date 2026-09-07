# pfSense setup

pfSense is FreeBSD with the `pf` firewall. The daemon is the same as on OpenWrt; only the way
it is started and the way the redirect is installed differ — and both are handled by `spoofd`
itself, so a whole deployment is **one binary, one config file, one boot command**. Written
against pfSense 2.9 with the **Tailscale package** (0.1.9) in its default TUN mode
(`tailscaled -tun tailscale0`).

If any concept here is unfamiliar — exit node, certificate, why the router sees the traffic —
read [how-it-works.md](how-it-works.md) first. If Tailscale on the pfSense is not yet set up, or
has never been checked, start with [tailscale-pfsense.md](tailscale-pfsense.md): `spoofd`
depends on the interface group, the firewall rules and the DNS described there.

## What pfSense gives us — and the one thing that is different here

- The Tailscale package creates a real interface, `tailscale0`, that pf can match on. It shows
  up as **Tailscale** in Firewall › Rules but cannot be assigned as OPTx and does not appear in
  NAT › Port Forward — which is why `spoofd` installs the redirect itself.
- **Exit-node traffic never crosses `tailscale0`.** On FreeBSD `tailscaled` forwards its
  clients' traffic in userspace: it decrypts the packets and opens the connections to the
  Internet *itself*, from the box. In `pfctl -ss` they show up as connections from the
  firewall's own WAN address with no client in parentheses. A redirect on the Tailscale
  interface therefore sees nothing (that is how this was found). `spoofd` handles it the way
  pf diverts locally originated traffic: a `pass out route-to (lo0 127.0.0.1)` sends the
  host's own connections to Apple's `17.0.0.0/8:443` through the loopback, where an `rdr on
  lo0` hands them to `spoofd`. `user root` restricts this to the host's own sockets
  (`tailscaled` runs as root), so forwarded LAN traffic is untouched; `spoofd`'s own
  connections to Apple are excluded by their source port (`pf-ports`, default 18500-18599),
  otherwise they would loop back into it.
- pfSense regenerates its ruleset but keeps sub-anchors across reloads. On FreeBSD a `rdr`
  rule is only evaluated through an `rdr-anchor` attachment, and pfSense has exactly one,
  `rdr-anchor "tftp-proxy/*"`, so the redirects live in `tftp-proxy/spoofd`; the pass and
  route-to rules live in `userrules/spoofd`. Should they ever be flushed (`pfctl -F all`),
  phones simply get their real position until `spoofd` restarts — fail-open.
- **Shellcmd** (System › Advanced) runs a command at boot and survives upgrades.

### Consequence: no per-device switch on pfSense

Because every exit-node client's connection is opened by the firewall itself, `spoofd` sees
one and the same source address for all of them. The per-device on/off switch on the status
page cannot tell devices apart here: **every device using this box as exit node is spoofed**.
Use a second exit node, or switch exit nodes on the phone, when you want the real position.

## 0. One requirement first: no IPv6 towards the clients

The redirect is IPv4-only (`17.0.0.0/8`). An exit node that provides IPv6 lets `locationd`
reach Apple over IPv6 and skip `spoofd`. Keep IPv6 disabled on pfSense (System › Advanced ›
Networking) — Tailscale itself does not need it.

## 1. Tailscale exit node

System › Package Manager › install **Tailscale**. VPN › Tailscale › Settings: enable, tick
**Advertise Exit Node**, save. Approve the exit node in the Tailscale admin console.

Firewall › Rules › **Tailscale** tab must let the tailnet in. The clean way is a *Network*
alias `Tailscale_Net` = `100.64.0.0/10` (all of Tailscale's IPv4 space; `fd7a:115c:a1e0::/48`
for IPv6) and two rules, *from Tailscale_Net to any* and *from any to Tailscale_Net*. Who may
enter is already decided by Tailscale's ACLs; pf only needs to let the tailnet through. Do not
list devices by IP: a re-registered node gets a new address and the list goes stale silently.

## 2. Install spoofd

`spoofd-freebsd-amd64` from the [Releases](https://github.com/alcor6502/location-spoofd/releases)
page (pfSense on ordinary hardware and VMs is `amd64`), then from the repo:

```sh
scp spoofd-freebsd-amd64 deploy/pfsense/spoofd.conf.sample deploy/pfsense/install.sh admin@<pfsense>:/tmp/
ssh admin@<pfsense> 'mv /tmp/spoofd-freebsd-amd64 /tmp/spoofd; sh /tmp/install.sh'
```

(`admin` lands in pfSense's console menu; option **8** gives a shell. SSH must be enabled in
System › Advanced › Secure Shell.)

Edit `/usr/local/etc/spoofd.conf` — coordinates in decimal degrees, right-click a point in
Google or Apple Maps — then start:

```sh
/usr/sbin/daemon -f -p /var/run/spoofd.pid /usr/local/sbin/spoofd -config /usr/local/etc/spoofd.conf
```

`tail /var/log/spoofd.log` must show the location line and `pf: redirect loaded on tailscale0`.
The status page is `http://<pfsense tailscale IP>:18080/`.

**At boot**: System › Advanced › Shellcmd (or Services › Shellcmd), add exactly that command,
type *shellcmd*, after any command that (re)starts Tailscale. **To stop**:
`kill $(cat /var/run/spoofd.pid)` — the redirect is removed with it.

## 3. What the redirect is

```
tftp-proxy/spoofd: rdr pass on tailscale0 inet proto tcp from any to 17.0.0.0/8 port 443 -> 127.0.0.1 port 18443
                   rdr pass on lo0        inet proto tcp from any to 17.0.0.0/8 port 443 -> 127.0.0.1 port 18443
userrules/spoofd:  pass in quick on tailscale0 proto tcp to 127.0.0.1 port 18443
                   pass in quick on tailscale0 proto tcp to (self) port 18080
                   pass out quick route-to (lo0 127.0.0.1) proto tcp from any port 18499 <> 18600
                                  to 17.0.0.0/8 port 443 user root
```

The first `rdr` covers kernel-forwarded traffic (harmless if there is none); the `route-to`
plus the `lo0` `rdr` cover what `tailscaled` originates itself. Only TCP 443 towards Apple's
`17.0.0.0/8`; `spoofd` then reads the server name and splices every host that is not the
location service straight through. Check with `pfctl -a tftp-proxy/spoofd -s nat` and
`pfctl -a userrules/spoofd -vsr` (the counters move when phones query).

## 4. Set up each phone (once)

With this box selected as exit node in the Tailscale app:

1. Safari → `http://<pfsense tailscale IP>:18080/ca.crt` → allow the profile download.
2. Settings › General › VPN & Device Management → install **Location Spoofer CA**.
3. Settings › General › About › Certificate Trust Settings → enable it. Without this step the
   TLS handshake fails and nothing happens.
4. Settings › Privacy & Security › Location Services → off, wait ten seconds, on.

Open Maps. Everything from here — daily use, polite mode, time zone, tuning `hacc`/`vacc`,
renewing the CA — is as in [openwrt-setup.md](openwrt-setup.md), except the per-device switch
(see above); the config file plus a restart replaces `uci` and `spoofctl`.

## Notes

- **Original destination**: on Linux `spoofd` recovers the pre-redirect address with
  `SO_ORIGINAL_DST`; on pfSense the redirected connection comes through `lo0` and spliced hosts
  are reached by their SNI name, which for Apple's hosts resolves to the same servers.
- **Tested** on pfSense 2.9 / Tailscale package 0.1.9 with a Mac as exit-node client:
  `gs-loc.apple.com` gets the router's certificate, `configuration.ls.apple.com` and the rest
  of the web pass through with their real ones.
- **Ports** 18443/18080 avoid pfSense's own web GUI; change `listen`/`http` in the conf.
