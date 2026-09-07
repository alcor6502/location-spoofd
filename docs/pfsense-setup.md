# pfSense setup

pfSense is FreeBSD with the `pf` firewall. The daemon is the same as on OpenWrt; only the way
it is started and the way the redirect is installed differ — and both are handled by `spoofd`
itself, so a whole deployment is **one binary, one config file, one boot command**. Written
against pfSense 2.9 with the **Tailscale package** (0.1.9) in its default TUN mode
(`tailscaled -tun tailscale0`).

If any concept here is unfamiliar — exit node, certificate, why the router sees the traffic —
read [how-it-works.md](how-it-works.md) first.

## What pfSense gives us

- The Tailscale package creates a real interface, `tailscale0`, that pf can match on. pfSense
  shows it as **Tailscale** in Firewall › Rules. It cannot be assigned as an OPT interface and
  does not appear in NAT › Port Forward — which is why `spoofd` installs the redirect itself.
- pfSense regenerates its ruleset, but keeps the extension anchors `natearly` and `userrules`
  across reloads. `spoofd -pf tailscale0` loads its two rules there at start and removes them at
  stop. Should they ever be flushed (`pfctl -F all`), phones simply get their real position
  until `spoofd` restarts: the failure mode is open, never broken positioning.
- **Shellcmd** (System › Advanced, or the Shellcmd package) runs a command at boot and survives
  upgrades.

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
natearly/spoofd:   rdr pass on tailscale0 inet proto tcp from any to 17.0.0.0/8 port 443 -> 127.0.0.1 port 18443
userrules/spoofd:  pass in quick on tailscale0 proto tcp to 127.0.0.1 port 18443
                   pass in quick on tailscale0 proto tcp to (self) port 18080
```

Only traffic arriving from the Tailscale interface (exit-node clients), only TCP 443, only
towards Apple's `17.0.0.0/8`. `spoofd` then reads the server name and splices every host that is
not the location service straight through. `pfctl -a natearly/spoofd -s nat` shows it.

## 4. Set up each phone (once)

With this box selected as exit node in the Tailscale app:

1. Safari → `http://<pfsense tailscale IP>:18080/ca.crt` → allow the profile download.
2. Settings › General › VPN & Device Management → install **Location Spoofer CA**.
3. Settings › General › About › Certificate Trust Settings → enable it. Without this step the
   TLS handshake fails and nothing happens.
4. Settings › Privacy & Security › Location Services → off, wait ten seconds, on.

Open Maps. Everything from here — daily use, the per-device switch on the status page, polite
mode, time zone, tuning `hacc`/`vacc`, renewing the CA — is as in
[openwrt-setup.md](openwrt-setup.md); the config file plus a restart replaces `uci` and
`spoofctl`.

## A pfSense-specific tip: never hard-code the node's Tailscale IP

Outbound NAT from the LAN towards the tailnet (LAN devices without Tailscale reaching tailnet
hosts through this box) must translate to the box's own Tailscale address, and pfSense offers no
"Interface Address" for the unassigned Tailscale interface. Instead of typing the IP — which
goes stale the day the node is re-registered — let pfSense resolve it:

1. Services › DNS Resolver › **Domain Overrides**: domain `<your-tailnet>.ts.net`, IP
   `100.100.100.100` (the MagicDNS resolver `tailscaled` runs on the box itself).
2. Firewall › Aliases: alias `Tailscale_Self`, type *Host*, value `<this-node>.<your-tailnet>.ts.net`.
3. Use `Tailscale_Self` (/32) as the NAT translation address.

pfSense re-resolves host aliases periodically, so the alias follows the node's IP with no
script. `tailscale status` on the box shows both names.

## Notes

- **Original destination**: on Linux `spoofd` recovers the pre-redirect address with
  `SO_ORIGINAL_DST`; the FreeBSD equivalent (`DIOCNATLOOK`) is not implemented. Spliced hosts
  are reached by their SNI name, which for Apple's hosts resolves to the same servers.
- **Ports** 18443/18080 avoid pfSense's own web GUI; change `listen`/`http` in the conf.
