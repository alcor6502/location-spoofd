# Changelog

## v1.5.0 — 2026-09-07

### Added
- **pfSense support that actually works**, tested on pfSense 2.9 / Tailscale 0.1.9. `spoofd`
  now reads a config file (`-config`) and manages its own pf rules (`-pf tailscale0`): a
  redirect on the interface plus a `route-to (lo0)` diversion of the host's own connections,
  because on FreeBSD `tailscaled` forwards exit-node traffic in userspace and no packet ever
  crosses `tailscale0`. spoofd's upstream connections are bound to `-pf-ports` so they are not
  diverted back. One binary, one file, one boot command; no shell scripts.
- `docs/tailscale-pfsense.md`: Tailscale on pfSense done properly (interface group, no Shellcmd
  restart, network aliases instead of IP lists, Unbound, admin-console DNS, key expiry via
  tags, ACLs, sharing).

### Changed
- The per-device switch is unavailable on pfSense (all exit-node clients share the firewall's
  source address); documented.
- Config file accepts trailing comments.


## v1.4.0 — 2026-09-07

### Added
- **pfSense / FreeBSD support**: `freebsd/amd64` and `freebsd/arm64` binaries, an rc.d launcher,
  a config sample and a pf `rdr` snippet in `deploy/pfsense/`, and a setup guide
  ([docs/pfsense-setup.md](docs/pfsense-setup.md)). The daemon is unchanged; only the
  integration differs. Not yet tested on real pfSense hardware.
- The CA expiry is warned about in the log at startup and on the status page, starting 60 days
  before it lapses.

### Docs
- `docs/openwrt-setup.md` gains a plain-language intro, a small glossary (SSH, Tailscale IP,
  coordinates) and a no-Go install path for non-developers.
- `docs/how-it-works.md` rewritten as a self-contained explanation for readers new to the
  protocols, TLS and Tailscale, including why Apple leaves `locationd` unpinned.


## v1.3.0 — 2026-09-07

### Added
- **Polite mode** (default on): the crowdsourcing uploads of spoofed devices
  (`gsp10-ssl.ls.apple.com/hvr/aploc` from `locationd`, `gsp64-ssl.ls.apple.com/hvr/v3/use`
  from `geoanalyticsd`) are read, discarded and answered `200 OK` on the router. Endpoint
  found by observing a real phone for two days. `option polite` in uci, *uploads swallowed*
  on the status page.
- A device switched off on the status page is now a fully normal phone (observed hosts are
  still logged when `observe` is set).
- `spoofd -log FILE` / `log_file` uci option.

### Added
- Debug aids: `spoofd -observe host,...` logs method, path, sizes and user-agent of requests to
  those hosts while forwarding them unchanged; `-v` logs every spliced connection; both
  exposed as `observe` / `verbose` uci options. Used to look for the crowdsourcing upload
  endpoint (not found on 17.0.0.0/8 in an hour; `iphone-ld.apple.com` is the open lead).

## v1.2.0 — 2026-09-05

### Added
- Replies carry the neighbourhood: every BSSID any client has asked about is appended to each
  reply at the spoofed location, capped by `num_wifi_results`, cached in `/etc/spoofd/bssids`.
  Fixes devices (iPadOS) that ask one access point per request and kept polling every 40 s.
- `spoofd -dump DIR` and `cmd/wlocdump` to capture and decode raw exchanges; `dump_dir` uci option.

## v1.1.0 — 2026-09-05

### Added
- Per-device switch on the status page (`POST /device spoof=on|off`): a phone can keep the
  exit node and still get its real position, without touching certificates. Persisted across
  restarts.

### Docs
- Why disabling the certificate trust is not a way to switch off; time zone behaviour.

## v1.0.0 — 2026-09-05

First release. Built on the reverse engineering and the on-device app of
[acheong08/ios-location-spoofer](https://github.com/acheong08/ios-location-spoofer) (a local-VPN
spoofer for the phone itself); this repository starts from a clean history and keeps only the
router daemon.

### Added
- `spoofd`: transparent MITM daemon for OpenWrt routers acting as Tailscale exit node.
  Intercepts `gs-loc.apple.com` by SNI, splices every other host through untouched, serves the
  CA for installation and a status page with counters.
- Cell towers are spoofed as well as WiFi access points; cell-only queries get a
  `cell_tower_response` for the phone's own cell, so cell and WiFi agree.
- Configurable accuracy and altitude; 10-year CA.
- OpenWrt integration: procd init script, uci config, fw3 firewall rules,
  `spoofctl on|off|status`, one-shot `install.sh`, `make deploy`.
- Tests: protobuf wire rewrite with field-preservation checks, ARPC round trip and truncation,
  end-to-end test of the daemon. CI builds arm64/arm/amd64/mipsle and publishes releases on tags.

### Fixed (relative to upstream)
- ARPC parser accepted truncated headers (`io.ReadFull`).
- Dead helpers and a stray macOS binary removed.
