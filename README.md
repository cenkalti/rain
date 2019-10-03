rain
====

BitTorrent client and library in Go. Running in production at [put.io](https://put.io).

[![Build Status](https://travis-ci.org/cenkalti/rain.svg?branch=master)](https://travis-ci.org/cenkalti/rain)
[![GoDoc](https://godoc.org/github.com/cenkalti/rain?status.svg)](https://godoc.org/github.com/cenkalti/rain/torrent)
[![GitHub Release](https://img.shields.io/github/release/cenkalti/rain.svg)](https://github.com/cenkalti/rain/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/cenkalti/rain)](https://goreportcard.com/report/github.com/cenkalti/rain)

Features
--------
- [Core protocol](http://bittorrent.org/beps/bep_0003.html)
- [Fast extension](http://bittorrent.org/beps/bep_0006.html)
- [Magnet links](http://bittorrent.org/beps/bep_0009.html)
- [Multiple trackers](http://bittorrent.org/beps/bep_0012.html)
- [UDP trackers](http://bittorrent.org/beps/bep_0015.html)
- [DHT](http://bittorrent.org/beps/bep_0005.html)
- [PEX](http://bittorrent.org/beps/bep_0011.html)
- [Message stream encryption](http://wiki.vuze.com/w/Message_Stream_Encryption)
- [WebSeed](http://bittorrent.org/beps/bep_0019.html)
- Fast resuming
- IP blocklist
- RPC server & client
- Console UI
- Tool for creating & reading .torrent files

Screenshot
----------
![Rain Screenshot](https://cl.ly/b03c639da66c/Screen%20Shot%202019-09-30%20at%2019.04.00.png)

Installing
----------

If you are on MacOS you can install from [brew](https://brew.sh/):
```sh
brew tap cenkalti/rain
brew install rain
```

Otherwise, get the latest binary from [releases page](https://github.com/cenkalti/rain/releases).

Usage
-----

Rain is distributed as single binary file.
The main use case is running `rain server` command and operating the server with `rain client <subcommand>` commands.
Server consists of a BitTorrent client and a RPC server.
`rain client` is used to give commands to the server.
There is also `rain client console` command which opens up a text based UI that you can manage the torrents on the server.
Run `rain help` to see other commands.

Difference from other clients
-----------------------------

Rain is the main BitTorrent client used at [put.io](https://put.io).
It is designed to handle hundreds of torrents while using low system resources.
The main difference from other clients is that Rain uses a different port for each torrent.
This allows Rain to download same torrent from same private tracker for multiple users and keep reporting their ratio correctly.

Missing features
----------------
- [IPv6 tracker extension](http://bittorrent.org/beps/bep_0007.html)
- [IPv6 extension for DHT](http://bittorrent.org/beps/bep_0032.html)
- [uTorrent transport protocol](http://bittorrent.org/beps/bep_0029.html)
- [Superseeding](http://bittorrent.org/beps/bep_0016.html)
- [HTTP seeding](http://bittorrent.org/beps/bep_0017.html)
- [Merkle tree torrent extension](http://bittorrent.org/beps/bep_0030.html)
- uPnP
- Selective downloading
- Sequential downloading
