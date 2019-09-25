rain
====

BitTorrent client and library in Go.

[![Build Status](https://travis-ci.org/cenkalti/rain.svg?branch=master)](https://travis-ci.org/cenkalti/rain)
[![GoDoc](https://godoc.org/github.com/cenkalti/rain?status.svg)](https://godoc.org/github.com/cenkalti/rain/torrent)
[![GitHub Release](https://img.shields.io/github/release/cenkalti/rain.svg)](https://github.com/cenkalti/rain/releases)

Features
--------
- [x] [Core protocol](http://bittorrent.org/beps/bep_0003.html)
- [x] [Fast extension](http://bittorrent.org/beps/bep_0006.html)
- [x] [Magnet links](http://bittorrent.org/beps/bep_0009.html)
- [x] [Multiple trackers](http://bittorrent.org/beps/bep_0012.html)
- [x] [UDP trackers](http://bittorrent.org/beps/bep_0015.html)
- [x] [DHT](http://bittorrent.org/beps/bep_0005.html)
- [x] [PEX](http://bittorrent.org/beps/bep_0011.html)
- [x] [Message stream encryption](http://wiki.vuze.com/w/Message_Stream_Encryption)
- [x] [WebSeed](http://bittorrent.org/beps/bep_0019.html)
- [x] Fast resuming
- [x] IP blocklist
- [x] RPC server & client
- [x] Console UI

Screenshot
----------
![Rain Screenshot](https://cl.ly/462cefdf6c4a/rain.png)

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

Rain is distributed as single binary file. The main use case is running `rain server` command and operating the server with `rain client <subcommand>` commands. Server consists of a BitTorrent client and a RPC server. `rain client` is used to give commands to the server. There is also `rain client console` command which opens up a text based UI that you can manage the torrents on the server. Run `rain help` to see other commands.
