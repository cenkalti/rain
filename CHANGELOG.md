# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

