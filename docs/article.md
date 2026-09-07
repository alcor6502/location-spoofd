# Your iPhone asks Apple where it is. My router answers instead.

*Draft for Hackaday. First person, no names, no coordinates. Installation details live in the
repository README; this is the story and the mechanism.*

---

I hold two passports and live between Bologna and Florida. Most things travel well between
the two: bank, phone number, email. Television does not. My American TV subscription is
tied to a "home area", and from Italy it politely refuses to work. Not because of my IP
address — that part I had solved years ago — but because the *phone* tells the app where it
is, and the phone is honest.

This is the story of how I got the phone to say "Florida" while sitting in Bologna, from my
home router, with nothing installed on the phone but a certificate. It turned into something
I had not planned: a close look at how an iPhone knows where it is, why that mechanism is
deliberately left open, what that openness means for anyone carrying a phone their employer
manages — and how to use it without polluting anyone else's data.

## Two things at once

Streaming services that care about geography check two things: where your traffic comes
from, and where your device says it is.

The first is easy. My home router in Florida is a GL.iNet box running OpenWrt, and it runs
Tailscale — a mesh VPN that connects my own devices to each other over WireGuard, wherever
they are. Tailscale has a feature called *exit node*: pick one device, and any other device
can route *all* of its Internet traffic through it with one tap. Phone in Bologna, tap, and
every web server sees a visitor from Florida.

The second is the problem. iOS runs one VPN at a time. The existing location spoofers for
non-jailbroken iPhones — acheong08's excellent `ios-location-spoofer` is the one I started
from — work by running a *local* VPN on the phone that catches the location traffic before
it leaves. A local VPN and Tailscale cannot both be active. Home IP or home position: pick
one.

Unless the catching moves to where the traffic already goes.

## How an iPhone knows where it is

Three sources, blended by a system process called `locationd`, the daemon behind everything
that says "Location Services" in Settings.

**GPS** is computed on the device from satellite signals. Precise under open sky, weak or
absent indoors. Nothing about it ever touches the network.

**WiFi** is the clever one. Every WiFi access point — your router, the café's, your
neighbour's — has a unique hardware address, the BSSID, something like `a4:91:b1:c0:35:5d`.
Your phone can see the BSSIDs around it any time the WiFi radio is on, whether you are
connected to them or not. It does not know where they are. Apple does, because for fifteen
years hundreds of millions of iPhones have been quietly reporting "I saw these BSSIDs while my
GPS said I was here". So the phone sends Apple the list of BSSIDs it sees, gets back their
positions, and — knowing how strong each signal is — works out its own. Indoors, in seconds,
with no satellite in sight. That fix is what Maps shows you in a shopping mall.

**