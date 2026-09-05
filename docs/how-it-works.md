# How it works

## Where an iPhone thinks it is

Core Location fuses three sources: GPS, WiFi and cellular. GPS is computed on the device;
WiFi and cell fixes are not. The phone scans nearby access points and cell towers, sends
their identifiers to Apple (`https://gs-loc.apple.com/clls/wloc`) and receives the known
position of each one, then trilaterates. That round trip is the one this project takes over.

## The request

`locationd` POSTs a small binary body:

```
ARPC header
  u16 version            (1)
  pascal string          locale        "en_US"
  pascal string          app           "com.apple.locationd"
  pascal string          OS version    "26.5.23F79"
  u32 function id
  u32 payload length
payload: protobuf AppleWLoc
```

`AppleWLoc` carries `wifi_devices` (field 2: BSSID, no location), `cell_tower_request`
(field 25: the serving cell) and device metadata. The `.proto` in `pb/` comes from
[apple-corelocation-experiments](https://github.com/acheong08/apple-corelocation-experiments).

## The answer we send

Apple is never asked. The request's own device list becomes the answer: every access point
and cell tower is given the same `Location` (latitude, longitude, accuracy, altitude), and for
a `cell_tower_request` a matching `cell_tower_response` (field 22) is emitted so the cell
fix agrees with the WiFi one. Everything else in the message is copied byte-for-byte on the
protobuf wire (`spoof/rewrite.go`) — unknown fields included — so the reply looks like what
`locationd` expects from Apple, framed as `8-byte magic + u16 length + payload`.

Apple's real reply is bigger than the question: it carries the *neighbourhood*, up to
`num_wifi_results` (50) access points around the ones asked about, and `locationd` uses that
list to place every access point in its scan without asking again. Some devices lean on this
heavily — iPadOS asks about one BSSID at a time and expects the rest to come back with it.
`spoofd` therefore remembers every BSSID any client has ever asked about (`/etc/spoofd/bssids`,
shared between devices, 500 entries) and appends them to each reply at the same location.
Without it a device keeps querying, one access point per request, until it has asked about
everything it can see.

All access points at one point means the trilateration collapses with minimal uncertainty:
that is what makes the WiFi fix win against a weak GPS fix. It cannot win against a strong
one; see the notes in the README.

## Getting in the middle

The traffic is TLS. The phone routes everything through a Tailscale exit node on the router,
and a netfilter rule redirects TCP 443 towards Apple's `17.0.0.0/8`, only from `tailscale0`,
to `spoofd`. The daemon peeks the ClientHello for the SNI without consuming it: Apple's
location host is terminated with a leaf certificate signed by a private CA the user installed
**and trusted** on the phone (Settings › General › About › Certificate Trust Settings), any
other name is spliced to its original destination (`SO_ORIGINAL_DST`) without being touched.
HTTP/1.1 only, which is what `locationd` speaks.

## What resets the fix

iOS caches the fused position. Toggling Location Services off and on clears it and forces
fresh queries. Airplane mode restarts the radios but not the GPS engine, so it works to come
back to the real position, not to leave it.
