package dht

import "net"

type DHT interface {
	Announce()
	Peers() chan []*net.TCPAddr
}
