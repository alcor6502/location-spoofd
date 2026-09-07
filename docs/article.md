# Your iPhone asks Apple where it is. My router answers instead.

*Draft for Hackaday. First person, no names, no coordinates.*

---

I hold two passports and live between Bologna and Florida. Most things travel well
between the two: bank, phone number, email. Television does not. My American TV
subscription is tied to a "home area", and from Italy it politely refuses to work — not
because of the IP address, that part I had solved years ago, but because the *phone* tells
the app where it is, and the phone is honest.

This is the story of how I made the phone lie, from the router, with nothing installed on
the phone but a certificate. Along the way it turned into something I did not expect: a
clear look at how an iPhone knows where it is, why that mechanism is deliberately left
open, and what that openness means for anyone carrying a phone their employer manages.

## Two things at once

Streaming services that care about geography check two things: where your traffic comes
from, and where your device says it is. The first is easy. A GL.iNet router at home runs
Tailscale, advertises itself as an *exit node*, and every device of mine can route all its
Internet traffic through it with one tap. The phone in Bologna then surfaces in Florida, as
far as any web server can tell.

The second is the problem. iOS lets one VPN run at a time. The existing location spoofers
for non-jailbroken iPhones — acheong08's excellent `ios-location-spoofer` is the one I
started from — work by running a *local* VPN on the phone that intercepts the location
traffic. Local VPN and Tailscale cannot coexist. Home IP or home position: pick one.

Unless the interception moves to where the traffic already goes.

## How an iPhone knows where it is

Three sources, fused by a system daemon called `locationd`. GPS is computed on the device
and never touches the network. WiFi and cell positioning do: the phone scans the access
points around it, collects their hardware addresses (BSSIDs), and asks Apple where those
access points are. Apple knows because millions of iPhones have reported "I saw this BSSID
while my GPS said I was here". The phone gets the positions back, does some trilateration,
and has a fix — indoors, in seconds, with no satellite in sight.

The question goes to `gs-loc.apple.com` as a small binary POST: a list of BSSIDs, the
serving cell, and a number — how many access points the phone would like back, typically
fifty. Because the reply is not just the answer: it is the *neighbourhood*, fifty access
points around the ones asked about, which `locationd` caches so it can place everything it
sees for a while without asking again. A phone on a sofa asks nothing for hours. A phone
walking through a city centre asks every minute. A phone whose Location Services were just
toggled asks in a burst, because the toggle empties the cache. All of this is on the wire,
and all of it is known thanks to acheong08's reverse engineering and a 2024 paper from the
University of Maryland.

Now: with the router as exit node, that question passes through my router on its way to
Apple. If the router could read it, it could answer it.

## The door, and why it is open

The question travels over HTTPS. TLS is not broken here and I did not break it; what TLS
protects is a conversation with *whoever the phone trusts*, and the list of whom the phone
trusts lives on the phone. Next to the ~150 public root certificates iOS ships with, a user
can install one more through a profile, and — since iOS 10.3 — flip a switch buried in
Settings › General › About › *Certificate Trust Settings* to make it count for TLS. Once that
switch is on, a certificate signed by that root is, to that phone, as good as one from
DigiCert. For any host name.

So the router generates its own certificate authority once, keeps the private key to itself,
and hands the public certificate to the phone. That is the one setup step that cannot be
automated, and it should not be: it is the phone's owner saying "I trust this router to speak
for anyone".

Why does Apple let `locationd` accept that? Not by oversight. Companies run fleets of
iPhones behind TLS-inspecting proxies that do exactly this, with their own CA installed by
mobile device management, and Apple supports it — its enterprise networking guidance lists
the handful of services that must bypass inspection because they pin their certificates
(iMessage, Apple Pay, activation). Location is not on that list. `locationd` is designed to
work through a corporate proxy, and therefore through mine.

## The other side of the coin

Sit with that for a moment, because it is the part of this project I did not go looking for.

On a supervised corporate iPhone, the CA pushed by MDM is trusted for TLS *silently* — no
switch, no prompt; the switch exists for profiles the user installs by hand. The same MDM can
impose an always-on VPN the user cannot turn off. From that point the company's gateway sees
what my router sees: every host name the phone talks to, the full content of every HTTPS
request that is not pinned — Safari, app APIs, forms — and, from the location traffic alone,
where the phone is every time `locationd` asks. Not from any app reporting location. From the
question itself.

It also sees the batches `locationd` uploads a few hours after you move around, which carry
the access points you saw tagged with your GPS trail. What it does not see is the content of
iMessage, FaceTime, Apple Pay, and end-to-end encrypted apps — for those, only the host
names.

GDPR requires companies to disclose and to be proportionate, and most inspect for malware
and data leaks rather than to follow people. But the capability is a property of the
architecture, not of anyone's intentions, and the lock icon in the browser answers "whom
does this phone trust", not "whom do I trust". I did not know this precisely before building
the thing that demonstrates it. I suspect most people carrying a work phone do not either.

## spoofd

Back to my router, which is the gateway *I* own. The daemon is about 600 lines of Go plus
the protobuf definitions, and it does five things.

**It gets the right packets.** One firewall rule on the router redirects TCP port 443 traffic
that arrives from the Tailscale interface and is headed for `17.0.0.0/8` — the block of
sixteen million addresses that is entirely Apple's — to the daemon. Everything else is
forwarded as usual.

**It decides by name without opening anything.** The first message of a TLS handshake, the
ClientHello, carries the intended host name in clear (the SNI); it has to, so a server can
pick a certificate. The daemon peeks at it without consuming it. For every name that is not
the location service — iCloud, iMessage, the App Store, software updates — it connects to
the original destination, replays the buffered bytes, and copies traffic both ways without
looking. It never has the session key. It sees what an ISP sees: a name and opaque bytes.
Certificate pinning in other apps notices nothing, because nothing happened to them.

**It becomes Apple for one host.** For `gs-loc.apple.com` it mints a certificate for that
exact name on the spot, signed by the router's CA, and completes the handshake itself. The
phone checks the chain, finds the trusted root, and proceeds.

**It answers from the question.** The request already lists every access point and cell the
phone can see. The daemon places each one at the configured coordinates, adds the
neighbourhood — every BSSID any device has ever asked about, so the reply looks like Apple's
and the cache fills — and sends it back framed exactly as Apple would. Apple is never asked.
The phone trilaterates a point with almost no uncertainty, since every access point is at
the same spot, and takes it.

**It keeps the cell in agreement.** The first version spoofed only WiFi, and under a skylight
the phone drifted back to Bologna: GPS said one thing, WiFi another, and the cell tower — a
real one, queried separately — sided with GPS. Answering the cell query with the same place
turned it into two against one. An iPad without a SIM has no cell and gives in to GPS more
easily; there is no software fix for physics.

The iPad taught me the neighbourhood, too. It asks about *one* BSSID per request and expects
the fifty around it; with a reply containing only that one, it kept asking, one access point
every forty seconds, for as long as I watched. Capturing a few exchanges and decoding them
showed the pattern in minutes. Returning the neighbourhood ended it.

## Being polite

If my phone has been told it is in Florida, will it report the access points of Bologna
tagged with Florida and pollute Apple's database? I did not want to guess. For two days I
logged the metadata of every Apple request from two devices with the upload switch on: an
iPhone with spoofing off and an iPad with spoofing on, on the same hour-long walk through
the centre of Bologna. The iPhone posted an 86 KB batch to `gsp10-ssl.ls.apple.com/hvr/aploc`
— *harvest, access-point locations* — about four hours later. The iPad never posted one.
Consistent with `locationd` discarding observations whose GPS and WiFi disagree, but one
device is not proof.

So the daemon does not rely on it. For every device it spoofs, uploads to that endpoint are
terminated, read, discarded and answered `200 OK`. The phone considers the batch delivered;
Apple never receives access points at a place they are not. Devices with spoofing off are
normal phones. It is on by default and it is the reason I felt comfortable publishing this.

## What it cannot do

GPS is computed on the device and cannot be intercepted. Indoors, WiFi plus cell wins;
under an open sky a strong GPS fix takes over, and on the roof terrace I am in Italy again.
Switching between home and real requires toggling Location Services by hand, because the
cache lives on the phone and no app or Shortcut may touch that switch. And the phone trusts a
private CA: the key never leaves the router and the router is reachable only from my
tailnet, but whoever holds that key can impersonate any site to that phone. I put it only on
phones I trust as much as myself.

Apple could end this. Not with pinning — that would break every corporate iPhone behind a
proxy — but with a signature on the reply verified by a key inside iOS, which a proxy would
pass through untouched and I could not forge. If that day comes the project stops cleanly;
nothing else on the phone is ever touched.

## Build it

A GL.iNet router (or any OpenWrt box) running Tailscale as exit node, SSH, and Go on your
computer — or a binary from the releases page. `make deploy ROUTER=root@192.168.8.1`, set two
coordinates, `spoofctl on`. On each phone, once: download the CA from the router's status
page, install the profile, flip the trust switch, toggle Location Services. Open Maps.

The code, the protocol notes and the two-day census are at
[github.com/alcor6502/location-spoofd](https://github.com/alcor6502/location-spoofd), AGPL-3.0,
built on acheong08's work. Bring your own coordinates.
