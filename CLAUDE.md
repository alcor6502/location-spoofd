# location-spoofd

Daemon for an OpenWrt / GL.iNet router used as Tailscale exit node: answers Apple's WiFi/cell
positioning queries (`gs-loc.apple.com`) so iPhones and iPads routed through the router
believe they are at a fixed location.

- `spoof/` — ARPC framing, protobuf wire rewrite, CA. Read `docs/how-it-works.md` before touching it.
- `cmd/spoofd/` — SNI peek, transparent TLS, splice, status page.
- `deploy/openwrt/` — init script, uci config, firewall rules, `spoofctl`, `install.sh`.

`make test` before committing. English everywhere. No personal coordinates in the repo: they
live only in the router's `/etc/config/spoofd`. Licence AGPL-3.0, inherited from the upstream
reverse engineering; it cannot be changed.
