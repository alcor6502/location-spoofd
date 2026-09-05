# location-spoofd <img align="right" src="https://img.shields.io/badge/License-AGPL--3.0-blue.svg">

<img src="https://img.shields.io/github/v/tag/alcor6502/location-spoofd?label=version&color=blue"> <img src="https://img.shields.io/badge/Go-1.25-00ADD8.svg?logo=go&logoColor=white"> <img src="https://img.shields.io/badge/OpenWrt-GL.iNet-00B5E2.svg"> <img src="https://img.shields.io/badge/Tailscale-exit%20node-242424.svg"> <img src="https://img.shields.io/badge/iOS-18%2B-000000.svg?logo=apple&logoColor=white"> <a href="https://github.com/alcor6502/location-spoofd/actions/workflows/ci.yml"><img src="https://github.com/alcor6502/location-spoofd/actions/workflows/ci.yml/badge.svg"></a>

**Your iPhone is wherever your router says it is.**

A small daemon for a GL.iNet (or any OpenWrt) router that you already use as a Tailscale
exit node. Phones routed through it get their home IP address *and* their home GPS position,
with nothing installed on the phone but a certificate. No jailbreak, no app, no third-party
service, nothing leaves your network.

---

## Why it exists

Some services want two things at once: your home IP address **and** your home position.
A Tailscale exit node at home gives you the first. The second is the problem: an iPhone
computes its position from GPS *and* from what Apple tells it about the WiFi access points
and cell towers around it, and those answers come from Apple's servers over the very
connection that now passes through your router.

So the router answers instead. Every access point and cell tower the phone asks about is
"at home", the phone trilaterates a point with no uncertainty, and takes it. IP and position
agree, and the phone runs nothing but Tailscale.

Why a router and not an app on the phone: iOS allows a single VPN. An on-device spoofer
(the [original project](https://github.com/acheong08/ios-location-spoofer) this grew out of
is one) and Tailscale cannot run together.

## How it works

1. `locationd` sends the BSSIDs and the serving cell it sees to `gs-loc.apple.com` over TLS.
2. A netfilter rule on the router redirects that traffic — TCP 443 towards Apple's
   `17.0.0.0/8`, only from the Tailscale interface — to `spoofd`.
3. `spoofd` reads the SNI. Apple's location host is terminated with a certificate signed by a
   CA the phone trusts; every other host is spliced to its real destination untouched.
4. The reply is built from the request itself: each access point and cell tower is placed at
   the configured coordinates, byte-for-byte compatible with what Apple would send.
5. iOS trilaterates a point with almost no uncertainty and takes it.

Wire format and details: [docs/how-it-works.md](docs/how-it-works.md).

## Quick start

Requirements: an OpenWrt router running Tailscale as exit node (tested on a GL.iNet
GL-MT5000 / Brume 3), SSH access to it, Go 1.25 on your computer — or a binary from the
[Releases](https://github.com/alcor6502/location-spoofd/releases) page.

```sh
git clone https://github.com/alcor6502/location-spoofd.git
cd location-spoofd
make deploy ROUTER=root@192.168.8.1      # builds spoofd and installs it on the router
```

On the router, set the coordinates (decimal degrees; right-click a point in Google Maps or
Apple Maps to copy them) and switch it on:

```sh
uci set spoofd.main.lat=48.858370; uci set spoofd.main.lon=2.294481; uci commit spoofd
spoofctl on
```

On each phone, once, with the router selected as exit node in the Tailscale app:

1. Safari → `http://<router tailscale IP>:18080/ca.crt`, allow the download.
2. Settings › General › VPN & Device Management → install the profile.
3. Settings › General › About › Certificate Trust Settings → enable **Location Spoofer CA**.
4. Settings › Privacy & Security › Location Services → off, then on.

Open Maps. Full guide, tuning and troubleshooting: [docs/router-setup.md](docs/router-setup.md).

**Daily use** — exit node on, then Location Services off/on: you are at home. Exit node off,
then airplane mode on/off: you are back. Want the exit node without the position? The status
page has a per-device switch. `spoofctl off` on the router disables everything.

## Limits, honestly

- **GPS is computed on the device** and cannot be intercepted. Indoors, or with a weak sky
  view, the WiFi + cell fix wins; under an open sky a strong GPS fix can take over. Both
  spoofed sources agreeing (WiFi and cell) is what keeps the position stable in practice.
- **Location Services must be toggled by hand** after switching: iOS caches the fused fix,
  and no app, shortcut or profile is allowed to touch that switch. A Shortcut can set the exit
  node and open the right Settings page; the tap is yours.
- The phone trusts a private CA. It is generated on your router, used only for
  `gs-loc.apple.com`, and never leaves it — but it is a root certificate on your phone. Know
  what that means before installing it.
- Apple decides the wire format. When it changes, this breaks until updated.

## Repository layout

```
spoof/            ARPC framing, protobuf wire rewrite, certificate authority
cmd/spoofd/       the daemon: SNI peek, transparent TLS, splice, status page
pb/               AppleWLoc protobuf (from apple-corelocation-experiments)
deploy/openwrt/   init script, uci config, firewall rules, spoofctl, install.sh
docs/             how it works, router setup
```

`make test` runs unit tests on the wire rewrite (every untouched field must survive
byte-for-byte) and an end-to-end test of the daemon. CI builds arm64, arm, amd64 and mipsle
binaries and attaches them to tagged releases.

## Credits

The reverse engineering of Apple's location service, the protobuf definitions and the
original on-device app are by [acheong08](https://github.com/acheong08):
[apple-corelocation-experiments](https://github.com/acheong08/apple-corelocation-experiments),
[ios-location-spoofer](https://github.com/acheong08/ios-location-spoofer). This repository keeps
only the router daemon; the on-device app lives upstream.

## License

[AGPL-3.0](LICENSE), inherited from the upstream work this is built on. For your own devices
on your own network.
