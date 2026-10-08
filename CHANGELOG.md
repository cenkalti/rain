# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Support plain address ranges, PeerGuardian/P2P and eMule `ipfilter.dat`
  formats in the IP blocklist, in addition to CIDR.

## [2.4.2] - 2026-10-03

### Changed

- Building requires Go 1.26.
- The macOS release is a universal binary that runs natively on Apple Silicon.
- Homebrew installs from a cask instead of a formula. Existing formula installs
  are migrated on `brew update`.

### Fixed

- Data race on a torrent's bitfield when a session closes while the torrent is
  verifying.
- The console no longer requests stats for every row when the visible columns
  don't need them.

## [2.4.1] - 2026-09-21

### Fixed

- Removing a running torrent now announces the "stopped" event to its trackers,
  so they receive the final upload/download counts.

## [2.4.0] - 2026-08-09

### Added

- "sequential" option for downloading pieces in order instead of rarest-first.

### Changed

- Building requires Go 1.25.

### Fixed

- Hybrid magnet links with both v1 and v2 info hashes are now recognized
  correctly.

### Security

- Reject tar entries that escape the destination directory in the
  `/move-torrent` endpoint (tar slip).

## [2.3.0] - 2026-02-07

### Added

- Padding size field in torrent stats and RPC response.

### Changed

- Path expansion now uses `$HOME` instead of `~`.
  Update config files to use `$HOME/path` instead of `~/path`.
  Example: `~/rain/config.yaml` becomes `$HOME/rain/config.yaml`.

## [2.2.0] - 2025-02-02

### Added

- "CustomStorage" field to Config struct for overriding default file storage provider.

## [2.1.0] - 2025-01-02

### Added

- "keep-data" option to RemoveTorrent command.

## [2.0.0] - 2024-12-20

### Changed

- Change field names in the config.yaml file.
  Previously, fields were parsed as lowercase strings. Now, they are dash-separated.
  Example: "portbegin" becomes "port-begin".

