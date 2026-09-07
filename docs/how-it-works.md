# How it works

This is the long version: what an iPhone does to find out where it is, what travels over the
network while it does so, and how a router can step into that conversation. No prior
knowledge of Apple's protocols or of Tailscale is assumed.

## 1. How an iPhone knows where it is

Three sources, fused by a system daemon called **`locationd`** (the process behind every
"Location Services" feature):

- **GPS**: computed entirely on the device from satellite signals. Precise outdoors, weak or
  absent indoors. Nothing about it goes over the network.
- **WiFi**: the phone scans the access points around it. Every access point has a unique
  hardware address, the **BSSID** (`aa:bb:cc:dd:ee:ff`). The phone does not know where those
  access points are — but Apple does, because millions of iPhones have reported "I saw this
  BSSID while my GPS said I was here". The phone sends the BSSIDs it sees to Apple, gets back
  their positions, and works out its own by distance and signal strength.
- **Cell**: same idea with the identifiers of nearby cell towers.

The WiFi and cell fixes are the ones that come from the network, and that is the only place
where a third party can step in. GPS cannot be touched from outside.

### The conversation with Apple

The query goes to the host **`gs-loc.apple.com`** ("gs" for GeoServices, "loc" for
location), over HTTPS, as:

```
POST https://gs-loc.apple.com/clls/wloc
```

`clls` is Core Location Location Service, `wloc` is WiFi location. The body is binary, in
two layers:

```
ARPC header  ─ a small Apple RPC framing:
   version, locale ("en_US"), calling app ("com.apple.locationd"),
   OS version ("26.6.1"), function id, payload length
payload      ─ a protobuf message called AppleWLoc:
   wifi_devices        the BSSIDs seen (no positions: that is the question)
   cell_tower_request  the serving cell, if any
   num_wifi_results    how many access points the phone would like back (typically 50)
   device_type         "iPhone18,1", OS version
```

Apple's reply uses the same `AppleWLoc` message, now with a `location` (latitude, longitude,
accuracy, altitude) inside each access point and cell — and not only the ones asked about:
the reply carries the **neighbourhood**, up to `num_wifi_results` access points around them.
`locationd` keeps that list in a cache and uses it to place every access point it sees
without asking again. That is why a phone standing still asks nothing, a phone walking
through a city asks every minute, and a phone whose Location Services were just toggled
asks in a burst: the toggle empties the cache.

Everything above is known thanks to the reverse engineering of
[acheong08/apple-corelocation-experiments](https://github.com/acheong08/apple-corelocation-experiments)
and the 2024 University of Maryland paper on Apple's WiFi positioning system; the protobuf
definition in `pb/` comes from that work.

### The other direction: what the phone reports back

The database is fed by the phones themselves. A few hours after moving around, `locationd`
uploads a batch of what it saw — BSSIDs, cell ids, and where its GPS said it was — to

```
POST https://gsp10-ssl.ls.apple.com/hvr/aploc
```

(`hvr` for harvest, `aploc` for access-point locations; we watched a phone for two days to
find it, see [Being polite](#6-being-polite) below). Maps usage analytics go to a sibling,
`gsp64-ssl.ls.apple.com/hvr/v3/use`. This matters because a phone that has been told it is
somewhere else could, in principle, report real access points at a fake place.

## 2. What the router is, and why the traffic goes through it

**Tailscale** is a VPN that connects your own devices to each other, wherever they are, using
the WireGuard protocol: every device gets a stable private address (`100.x.y.z`) and an
encrypted tunnel to every other device. Normally only traffic *between your devices* uses
the tunnel. An **exit node** changes that: a device you pick — here, the home router —
becomes the gateway for *all* of another device's Internet traffic. The phone in a hotel
sends everything, encrypted, to the router at home, which sends it out to the Internet from
the home connection. That is how the phone gets its home IP address.

It also means every packet the phone sends to Apple enters the router through the tunnel
interface, called `tailscale0`, before going anywhere. The router is in the path. That is
the whole trick: nothing has to be installed on the phone to see its traffic, because the
phone is already sending it to us.

### Picking out the interesting packets

Traffic arriving on `tailscale0` is normally just forwarded on to the Internet. One firewall
rule changes the destination of a narrow slice of it:

```
TCP, from tailscale0, to any address in 17.0.0.0/8, port 443  →  send to 127.0.0.1:18443
```

`17.0.0.0/8` is the block of 16 million IP addresses that belongs entirely to Apple (Apple
was an early Internet participant and owns the whole "17." range). Port 443 is HTTPS. So
"HTTPS from exit-node phones towards Apple" is diverted to a program listening on the router
itself, `spoofd`, on port 18443. Everything else — HTTP, other ports, other destinations,
traffic from the router's own LAN — is untouched.

This diversion (a NAT `REDIRECT`) has nothing to do with WireGuard or Tailscale; it is the
ordinary Linux firewall, and it acts after the tunnel has already decrypted the packet. The
phone sees none of it: its TCP connection to "Apple" simply succeeds.

## 3. TLS, certificates, and the one thing the phone is asked to trust

The diverted connection is HTTPS, i.e. TLS. Before any data flows, client and server
perform a *handshake*: the server presents a **certificate** — "I am gs-loc.apple.com, and
a certificate authority vouches for it" — the client checks that the vouching chain ends at a
**root CA** it trusts, then both derive a session key and everything after is encrypted.
Anyone in the middle sees opaque bytes.

TLS is not broken here. The question it hinges on is *whom does the phone trust*, and the
answer lives on the phone: about 150 public root CAs shipped by Apple, plus any root the
**user** installs through a configuration profile. Since iOS 10.3 a user-installed root is
not trusted for TLS until the user also flips its switch in Settings › General › About ›
*Certificate Trust Settings*. That switch is the entire basis of this project, and the reason
the setup has one step that cannot be automated.

`spoofd` creates its own root CA on first start ("Location Spoofer CA", valid ten years). The
private key stays on the router (`/etc/spoofd/ca-key.pem`, readable by root only) and never
travels. Only the public certificate is served for installation. Once installed and trusted,
a certificate signed by that key is, to that phone, as good as one from DigiCert — for any
host name. Which host names it is actually used for is a decision of the software, described
next, not a technical limit.

## 4. Deciding by name, without opening anything

A diverted connection arrives at `spoofd` with no obvious label: the original destination
address is just "some Apple server". The phone, however, tells us what it wants in the very
first message of the handshake, the *ClientHello*, which by design travels in clear and
includes the **SNI** (Server Name Indication): the host name the client intends to reach.
It is there because a server hosting many sites has to know which certificate to present
before anything can be encrypted.

`spoofd` reads the ClientHello **without consuming it** — it is buffered and replayed later
— and takes one of two paths depending on the name:

**Not a location host** (`gateway.icloud.com`, `mesu.apple.com`, iMessage, the App Store,
… the overwhelming majority): `spoofd` asks the kernel for the original destination address
(`SO_ORIGINAL_DST`), opens a plain TCP connection to it, pushes the buffered bytes into it,
and from then on copies bytes in both directions without looking at them. It is a pipe. It
never has the session key, so it cannot decrypt; it sees what any ISP sees, a host name and
opaque bytes. The phone completes its handshake with the *real* Apple server and verifies
the *real* Apple certificate. Apps that pin their certificates — iMessage, Apple Pay,
banking apps — notice nothing, because nothing happened to them.

**A location host** (`gs-loc.apple.com`; with polite mode also the two harvest hosts):
`spoofd` answers the handshake itself. It needs a certificate for that exact name, so it
**mints one on the spot** — subject `gs-loc.apple.com`, signed by the router's CA, valid 30
days, cached for the next connection. The phone checks the chain: leaf → Location Spoofer CA
→ *present in my trust list, with full trust* → accepted. `spoofd` now holds the session
key; this one connection is plaintext *to it*.

## 5. Inside the connection

What `spoofd` sees is ordinary HTTP/1.1: `POST /clls/wloc` with the ARPC+protobuf body of
section 1. It parses it, places every access point and cell tower in the request at the
configured coordinates, adds the neighbourhood (every BSSID any device has ever asked about,
so the reply looks like Apple's and the cache fills up), frames the reply exactly as Apple
would, and sends it back. **Apple is never contacted for these queries**: the phone's
question already contains everything needed for the answer.

The phone trilaterates a point with almost no uncertainty — every access point is at the
same spot — and, with the cell agreeing, takes it. Fields that are not spoofed are copied
byte-for-byte on the protobuf wire, unknown ones included, so the reply is indistinguishable
from a real one to the parser.

Any other request on that host (rare) is forwarded: `spoofd` opens its own TLS connection
to the real Apple server — verifying Apple's real certificate, as the phone would have — and
proxies it. The *observe* mode used during investigation is the same mechanism pointed at
chosen hosts: terminate, log method/path/sizes/user-agent, forward unchanged.

## 6. Being polite

A spoofed phone has been told it is somewhere else. Would it report the real access points
it sees, tagged with the fake place, and pollute the database? To find out we watched two
devices for two days with the upload switch (*Improve Location Accuracy*) on: an iPhone with
spoofing off and an iPad with spoofing on, on the same hour-long walk through a dense city
centre. The iPhone posted an 86 KB batch to `/hvr/aploc` about four hours later. The iPad
never posted one — consistent with `locationd` discarding observations whose GPS and WiFi
disagree, but one device is not proof.

So `spoofd` does not rely on that. For every device it spoofs, requests to `/hvr/` on
`gsp10-ssl` and `gsp64-ssl.ls.apple.com` are terminated, read, discarded and answered
`200 OK`: the phone considers the batch delivered, Apple never receives it. Devices with
spoofing switched off are normal phones and upload normally. `option polite '0'` disables it.

## 7. What resets the fix, and what cannot be fixed

`locationd` caches both the fused position and the access-point neighbourhood. Toggling
Location Services off and on empties both and forces fresh queries; that is why every
switch between spoofed and real needs it, and why no app or shortcut can do it for you.
Airplane mode restarts the radios but not the GPS engine, so it works to come *back* to the
real position, not to leave it.

GPS itself is out of reach. Indoors the WiFi+cell fix wins; under an open sky a strong GPS
fix can take over. A device without an active SIM has no cell fix to corroborate the WiFi
one and gives in to GPS more easily.

## 8. The security model, and the way this ends

**Whoever holds `ca-key.pem` can impersonate any site to that phone, for traffic that passes
them.** `spoofd` chooses to do it for a handful of Apple hosts and leaves everything else in a
blind pipe, but that is a choice in the code, not a limit. Hence: the key lives only on the
router, the router is reachable only from your tailnet, and the certificate goes only on
phones you trust as much as yourself.

Why Apple does not stop this today: `locationd` trusts the system trust store and does no
**certificate pinning** — the practice of accepting, for a given host, only a certificate or
CA hard-wired into the program, ignoring the trust store (iMessage and Apple Pay do this).
If Apple ever pinned `locationd`, the phone would reject the minted certificate, the
handshake would fail, and location queries would stop passing through. Nothing could be done
about it short of a jailbreak, since the rule would live inside iOS. The project would end
cleanly: the rest of the phone's traffic, which is never touched, would keep working.

```
 iPhone                        router (Tailscale exit node)                       Apple
 ──────                        ────────────────────────────                       ─────
 TLS ClientHello ──WireGuard──▶ tailscale0 ──REDIRECT (17/8:443)──▶ spoofd :18443
   SNI = ?                                                        │ peek SNI
                                                                  ├─ other name ─▶ splice ──────────────▶ real server
                                                                  │                (blind pipe, bytes untouched)
                                                                  └─ gs-loc ─────▶ handshake with minted cert
                                                                                   ▶ parse /clls/wloc
                                                                                   ◀ reply "you are at home"       (Apple never asked)
                                                                                   ▶ /hvr/ uploads → discard, 200  (polite mode)
```
