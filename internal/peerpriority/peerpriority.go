// Package peerpriority implements BEP 40.
package peerpriority

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"net"
)

// Priority is a number that determines the connect order for Peers.
type Priority = uint32

var table = crc32.MakeTable(crc32.Castagnoli)

// Calculate returns the priority of a to client address b.
func Calculate(a, b *net.TCPAddr) Priority {
	bs := calculateBytes(a, b)
	d := crc32.New(table)
	if bytes.Compare(bs[0], bs[1]) < 0 {
		_, _ = d.Write(bs[0])
		_, _ = d.Write(bs[1])
	} else {
		_, _ = d.Write(bs[1])
		_, _ = d.Write(bs[0])
	}
	return d.Sum32()
}

func calculateBytes(a, b *net.TCPAddr) (ret [2][]byte) {
	if a.IP.Equal(b.IP) {
		var buf [4]byte
		binary.BigEndian.PutUint16(buf[0:2], uint16(a.Port))
		binary.BigEndian.PutUint16(buf[2:4], uint16(b.Port))
		ret[0] = buf[0:2]
		ret[1] = buf[2:4]
		return
	}
	a4 := a.IP.To4()
	b4 := b.IP.To4()
	m := ipv4Mask(a4, b4)
	ret[0] = a4.Mask(m)
	ret[1] = b4.Mask(m)
	return
}

func ipv4Mask(a, b net.IP) net.IPMask {
	if !sameSubnet(16, 32, a, b) {
		return net.IPv4Mask(0xff, 0xff, 0x55, 0x55)
	}
	if !sameSubnet(24, 32, a, b) {
		return net.IPv4Mask(0xff, 0xff, 0xff, 0x55)
	}
	return net.IPv4Mask(0xff, 0xff, 0xff, 0xff)
}

func sameSubnet(ones, bits int, a, b net.IP) bool {
	mask := net.CIDRMask(ones, bits)
	return a.Mask(mask).Equal(b.Mask(mask))
}
