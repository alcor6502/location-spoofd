# Changelog

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
