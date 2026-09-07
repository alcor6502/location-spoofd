# Tailscale on pfSense, done properly

What has to be right on a pfSense box that serves as Tailscale **exit node** and **subnet
router**, and in the Tailscale admin console that governs it. Written from a real cleanup of a
pfSense 2.9 install whose Tailscale had silently stopped working; every point below was a
problem found there. Nothing here is specific to `spoofd` — it is the ground `spoofd` stands on
([pfsense-setup.md](pfsense-setup.md) assumes it).

## Part 1 — pfSense

### 1.1 The package, and what it creates

System › Package Manager › **Tailscale**. It installs `tailscaled` in TUN mode
(`tailscaled -tun tailscale0`) and a wrapper service, `/usr/local/etc/rc.d/pfsense_tailscaled`,
that starts the daemon **and** puts `tailscale0` into a pf **interface group** named
`Tailscale`. That group is what the firewall rules refer to (`on Tailscale`). It shows up as a
tab in Firewall › Rules; the interface itself cannot be assigned as OPTx and does not appear in
NAT › Port Forward.

Verify after every boot or package change:

```sh
ifconfig tailscale0 | grep groups      # must say:  groups: tun Tailscale
tailscale status | head -1             # must say:  ... offers exit node
```

If the group is missing, every `on Tailscale` rule matches nothing and pf's default deny
drops all traffic entering from the tailnet — exit node, subnet routes, DNS — while the node
still looks perfectly online in the admin console (WireGuard and the control plane are
unaffected). This is the single most confusing failure mode: `tailscale ping` gets a pong,
`ping` does not.

### 1.2 Do not restart Tailscale from Shellcmd (pfSense ≥ 2.9)

Older packages started too early and people added a boot-time
`/usr/local/etc/rc.d/tailscaled restart` in Shellcmd. That script is the plain FreeBSD port's:
it restarts the daemon and recreates `tailscale0` **without the interface group** — exactly the
failure above, at every boot. On pfSense 2.9 the package starts correctly on its own; remove
the Shellcmd entry. If you ever must restart it from a script, use the wrapper:
`/usr/local/etc/rc.d/pfsense_tailscaled restart`.

### 1.3 VPN › Tailscale › Settings

- **Enable**, **Advertise Exit Node**, **Advertised routes**: your LAN (e.g. `192.168.120.0/24`).
- **Accept DNS**: on is fine; on pfSense it does not take over the system resolver.
- Listen port: leave 41641 and let pfSense's outbound NAT keep it stable if you want direct
  connections (see 1.7).

After saving, approve the exit node and the routes in the admin console (Part 2).

### 1.4 Firewall rules on the Tailscale tab: networks, never device lists

Tailscale addresses are assigned by Tailscale and **change when a node is re-registered**
(new tailnet, reinstall, expired key re-authenticated). A rule that lists devices by IP goes
stale silently. Use the whole Tailscale range instead and let Tailscale's ACLs decide who may
enter:

- Firewall › Aliases: `Tailscale_Net`, type *Network*, `100.64.0.0/10`
  (add `fd7a:115c:a1e0::/48` only if you run IPv6).
- Firewall › Rules › Tailscale: *pass from Tailscale_Net to any* and, if you want tailnet
  peers to reach services on the box, *pass from any to Tailscale_Net*.

Tailscale only delivers packets the ACL allows, so this is not "open to everyone in
100.64/10": it is "whatever Tailscale lets through, pf lets through too".

### 1.5 Outbound NAT towards the tailnet: no hard-coded IP

LAN devices without Tailscale reach tailnet hosts through the box (subnet router in the
reverse direction) only if their source is translated to the box's **own Tailscale address**;
peers do not answer to `192.168.x`. pfSense offers no "Interface Address" for the unassigned
Tailscale interface, so people type the IP — which goes stale like everything else.

Let pfSense resolve it instead:

1. Services › DNS Resolver › **Domain Overrides**: domain `ts.net`, IP `100.100.100.100`
   (`tailscaled` runs the MagicDNS resolver on the box itself; `ts.net` rather than your
   tailnet's name so shared nodes from other tailnets resolve too, and renaming the tailnet
   changes nothing).
2. Firewall › Aliases: `Tailscale_Self`, type *Host*, value `<this-node>.<tailnet>.ts.net`.
3. Firewall › NAT › Outbound, the rule *LAN subnets → any on Tailscale*: Translation →
   *Network or Alias* → `Tailscale_Self`, /32. Address family IPv4.

pfSense re-resolves host aliases every few minutes; the NAT follows the node's IP forever.

### 1.6 DNS Resolver (Unbound)

- **Network Interfaces: All.** `tailscale0` is not in the list; "All" is the only way to
  listen on it. Safe: pf's default deny keeps WAN/VPN interfaces closed, and the access list
  below refuses everyone else.
- **Access Lists**: add `100.64.0.0/10` → Allow. Without it, queries arriving from tailnet
  addresses get `REFUSED`.
- **Outgoing Network Interfaces**: your real uplinks only (WAN, backup WAN). Not Tailscale, not
  VPN clients — DNS should leave from where the traffic leaves.
- Delete any leftover **Virtual IP** on `lo0` that was created to "listen on the Tailscale
  address" in the past; it is the old IP.

### 1.7 Direct connections through CGNAT/PPPoE

`tailscale status` shows `direct` or `relay "xxx"` per peer. Relayed traffic works but adds
latency (a DERP in another city). For direct paths the box must keep a stable UDP source
port: Firewall › NAT › Outbound, a rule for the box itself on port 41641 with **Static Port**
checked. Behind a carrier-grade NAT (T-Mobile Home Internet, most mobile uplinks) direct
connections are usually impossible; the relay is what you get.

### 1.8 IPv6

Keep it off unless you need it (System › Advanced › Networking, plus a *block LAN IPv6 to
any* rule if you want belt and braces). Tailscale does not need it; `spoofd`'s redirect is
IPv4-only by design.

### 1.9 Out-of-band access

A router that runs your only Tailscale node can lock you out with one bad restart (1.1).
Keep a second, independent node on the same LAN — a Raspberry Pi, a VM, a NAS — with
**Tailscale SSH** enabled and, if useful, the LAN as a subnet route. When the firewall's own
Tailscale breaks, you still reach its web GUI through that node. For a router with nothing
else on its LAN, the only fallback is someone on site.

## Part 2 — the admin console (login.tailscale.com)

### 2.1 DNS

- **MagicDNS**: on. Every node gets `<name>.<tailnet>.ts.net`.
- **Split DNS** for your home domain → the pfSense's **Tailscale** address (not its LAN
  address, which is unreachable from outside without the subnet route on every client).
- **Override local DNS**: off, unless you deliberately want every device's DNS to go through
  one resolver.
- Know this: **while a device uses an exit node, all its DNS goes to the exit node**, split
  DNS included (measured with `tailscale dns query`, client 1.102). Local names resolve only if
  the exit node's resolver knows them. On pfSense as exit node that is automatic (Unbound owns
  the domain); on another exit node forward the domain to the pfSense over the tailnet
  (dnsmasq on OpenWrt: `server=/home.arpa/<pfsense tailnet IP>` plus `rebind_domain`).
- Prefer a domain you own (`lan.example.com`) over `home.arpa`: `home.arpa` is the same in
  every home, so two people's local names collide the moment one uses the other's exit node.

### 2.2 Key expiry — and how to make routers never expire

Every node key expires after 180 days by default and the node drops off until someone
re-authenticates on it — deadly for a router nobody touches. There is no tailnet-wide
"never expire" default, but there are two ways:

1. **Per machine**: Machines › the node › ⋯ › *Disable key expiry*. Simple; remember to do it
   on every router.
2. **Tags** (the proper way): tagged nodes have **no key expiry by definition**, and tags are
   also what ACLs refer to. In the ACL policy:

   ```json
   "tagOwners": { "tag:router": ["autogroup:admin"] }
   ```

   then on the node (or in the pfSense package's advanced args) `tailscale up --advertise-tags=tag:router ...`,
   or Machines › the node › *Edit ACL tags*. From then on the node is identified by its tag,
   never expires, and stays in ACLs when you rebuild it.

### 2.3 Exit node and routes

Machines › the node › *Edit route settings*: approve **Use as exit node** and the advertised
LAN route. Both are opt-in per node; a node that shows "not advertising an exit node" in
`tailscale set --exit-node=` either has the box option off (1.3) or has not been approved.

Optionally *Auto-approve* in the ACL for tagged routers:

```json
"autoApprovers": {
  "exitNode": ["tag:router"],
  "routes": { "192.168.120.0/24": ["tag:router"] }
}
```

### 2.4 ACLs: what your own devices and your guests may do

A minimal, explicit policy:

```json
{
  "tagOwners": { "tag:router": ["autogroup:admin"] },
  "acls": [
    // my own devices: everything, including the LAN behind the routers
    { "action": "accept", "src": ["autogroup:member"], "dst": ["*:*"] },
    // people I share a router with: Internet through it, nothing else
    { "action": "accept", "src": ["autogroup:shared"], "dst": ["autogroup:internet:*"] }
  ],
  "ssh": [
    { "action": "check", "src": ["autogroup:member"], "dst": ["autogroup:self"], "users": ["autogroup:nonroot", "root"] }
  ]
}
```

`autogroup:shared` is everyone from another tailnet who received a node you shared. They
can use the exit node; they get no route to your LAN even if they resolve a name into it, and
no access to the node's own services. The pf rule *Tailscale_Net to any* (1.4) is behind this
gate, not in front of it.

### 2.5 Sharing a node with a friend

Machines › the node › *Share* → their email. A shared node keeps its address in their
tailnet. They pick it as exit node in their Tailscale app; your ACL decides what it can do
for them. Their own split DNS is ignored while they use your exit node (2.1); your resolver
answers, so they see your local names resolve but cannot reach them without a route.

## Checklist

```
ifconfig tailscale0 | grep groups        →  groups: tun Tailscale
tailscale status | head -1               →  offers exit node
pfctl -s rules | grep "on Tailscale"     →  Tailscale_Net, no IP lists
pfctl -s nat   | grep Tailscale          →  -> <current tailnet IP>, from the alias
sockstat -4 -l | grep :53                →  *:53   (Unbound on All)
grep 100.64 /var/unbound/access_lists.conf
dig home.name @<pfsense tailnet IP>      →  NOERROR, from anywhere in the tailnet
```
