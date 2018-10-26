rain
====

BitTorrent client and library in Go.

[![Build Status](https://travis-ci.org/cenkalti/rain.svg?branch=master)](https://travis-ci.org/cenkalti/rain) [![GoDoc](https://godoc.org/github.com/cenkalti/rain?status.svg)](https://godoc.org/github.com/cenkalti/rain)

Features
--------
- [x] [Core protocol](http://bittorrent.org/beps/bep_0003.html)
- [x] [Fast extension](http://bittorrent.org/beps/bep_0006.html)
- [x] [Magnet links](http://bittorrent.org/beps/bep_0009.html)
- [x] [Multiple trackers](http://bittorrent.org/beps/bep_0012.html)
- [x] [UDP trackers](http://bittorrent.org/beps/bep_0015.html)
- [x] [Message stream encryption](http://wiki.vuze.com/w/Message_Stream_Encryption)
- [x] Fast resuming
- [x] Custom storage
- [x] CLI
- [x] RPC server
- [ ] Webseed
- [ ] IP blocklist

Installing
----------

`go get github.com/cenkalti/rain`

Usage
-----

`rain download <torrent file or magnet link>`
