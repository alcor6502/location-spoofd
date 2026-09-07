# pfSense setup

pfSense is FreeBSD-based and uses the `pf` firewall, so the pieces differ from OpenWrt but the
idea is identical: the router is a Tailscale exit node, a redirect sends the phone's HTTPS
traffic bound for Apple to `spoofd`, and the phone trusts a certificate the router serves.

> Status: the FreeBSD build and the daemon are shared with the OpenWrt version and covered by
> the same tests, but the pfSense integration steps below have not yet been run on real pfSense
> hardware. Treat this as a first draft and verify each step on your box; corrections welcome.

If any concept here is unfamiliar — exit node, certificate, why the router sees the traffic —
read [how-it-works.md](how-it-works.md) first.

## 1. Tailscale exit node on pfSense

Install the **Tailscale** package (System > Package Manager), sign in, and make this box an
exit node. If the package UI does not expose it, add to the Tailscale settings' advanced args,
or from a shell:

```sh
tailscale up --advertise-exit-node --reset
```

Approve the exit node in the Tailscale admin console. Assign the Tailscale interface in pfSense
(Interfaces > Assignments — it usually appears as `tailscale0`) so you can write firewall and
NAT rules on it; note the name pfSense gives it (e.g. `OPT1`).

## 2. Install spoofd

Copy the FreeBSD binary and the helper files to the firewall (from a machine with this repo, or
download `spoofd-freebsd-amd64` from the [Releases](https://github.com/alcor6502/location-spoofd/releases)
page — pfSense on standard hardware is `amd64`):

```sh
scp spoofd-freebsd-amd64            root@<pfsense>:/usr/local/sbin/spoofd
scp deploy/pfsense/spoofd.sh        root@<pfsense>:/usr/local/etc/rc.d/spoofd.sh
scp deploy/pfsense/spoofd.conf.sample root@<pfsense>:/usr/local/etc/spoofd.conf
ssh root@<pfsense> 'chmod 755 /usr/local/sbin/spoofd /usr/local/etc/rc.d/spoofd.sh'
```

Edit `/usr/local/etc/spoofd.conf` and set your coordinates (decimal degrees; right-click a
point in Google or Apple Maps):

```sh
LAT="48.858370"
LON="2.294481"
ALT=35
```

Start it and check:

```sh
/usr/local/etc/rc.d/spoofd.sh start
/usr/local/etc/rc.d/spoofd.sh status
```

pfSense runs every `*.sh` in `/usr/local/etc/rc.d/` at boot, so it will start on reboot. The CA
(`/var/db/spoofd/ca.pem`, valid ten years) and the status page are now on the firewall.

## 3. The redirect

`spoofd` only sees traffic that pf sends to it. The clean, persistent way on pfSense is the GUI,
because pfSense regenerates its `pf` ruleset and would drop hand-edited rules.

First make an alias for Apple's address block (Firewall > Aliases > add): type **Network**, name
`Apple_Net`, value `17.0.0.0/8`.

Then Firewall > NAT > **Port Forward** > Add:

| Field | Value |
|-------|-------|
| Interface | your Tailscale interface (e.g. `OPT1`) |
| Protocol | TCP |
| Destination | Address or Alias → `Apple_Net` |
| Destination port range | HTTPS (443) to HTTPS (443) |
| Redirect target IP | `127.0.0.1` |
| Redirect target port | 18443 |
| Description | spoofd |

Leave "Filter rule association" at **Add associated filter rule** so the matching pass rule is
created for you.

Then Firewall > Rules > your Tailscale interface > add a rule allowing TCP to **This Firewall**
port `18080` (the CA download and status page).

> Redirecting to `127.0.0.1` can be finicky on pfSense (it keeps `lo0` skipped). If phones do
> not get a spoofed location and the status page shows no intercepted queries, set the redirect
> target to the firewall's own Tailscale interface address instead — `spoofd` listens on all
> interfaces, so either works once pf actually delivers the connection.

## 4. Set up each phone (once)

With this firewall selected as exit node in the Tailscale app:

1. Safari → `http://<pfsense tailscale IP>:18080/ca.crt` → allow the profile download.
2. Settings › General › VPN & Device Management → install **Location Spoofer CA**.
3. Settings › General › About › Certificate Trust Settings → enable it. Without this step the
   TLS handshake fails and nothing happens.
4. Settings › Privacy & Security › Location Services → off, wait ten seconds, on.

Open Maps. Everything after this — daily use, the per-device switch, polite mode, time zone,
tuning `hacc`/`vacc`, renewing the CA — works exactly as in [openwrt-setup.md](openwrt-setup.md);
only the config file (`/usr/local/etc/spoofd.conf`, then `spoofd.sh restart`) replaces `uci`.

## Notes

- **Original destination**: on Linux `spoofd` recovers the pre-redirect address with
  `SO_ORIGINAL_DST`; FreeBSD's equivalent (`DIOCNATLOOK` on `/dev/pf`) is not implemented yet.
  Without it, non-Apple hosts that are terminated for observation would be reached by their SNI
  name. This does not affect normal use: location hosts are answered locally and everything else
  is spliced by SNI, which for Apple's hosts resolves to the same place.
- **No `spoofctl`**: use `spoofd.sh start|stop|restart|status`. To disable spoofing without
  removing the NAT rule, set `ENABLED=0` in the conf and restart, then also disable the Port
  Forward in the GUI (otherwise the redirect points at a stopped daemon).
