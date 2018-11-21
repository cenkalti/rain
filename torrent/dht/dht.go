package dht

import "net"

type DHT interface {
	// Announce must request new peers from DHT.
	// Announce is called by torrent periodically or when more peers are needed.
	Announce()
	// Peers must return a channel for peer addresses returned in response to Announce call.
	Peers() chan []*net.TCPAddr
}
