// Package fast provides an algorithm for generating fast set.
// See http://www.bittorrent.org/beps/bep_0006.html
package fast

import (
	"crypto/sha1" // nolint: gosec
	"encoding/binary"
	"net"
)

// GenerateFastSet returns a slice of k items that contains the piece indexes of the torrent with infoHash.
func GenerateFastSet(k int, numPieces uint32, infoHash [20]byte, ip net.IP) []uint32 {
	ip = ip.To4()
	if ip == nil {
		return nil
	}
	ip = ip.Mask(net.CIDRMask(24, 32))
	x := make([]byte, 24)
	copy(x, ip)
	copy(x[4:], infoHash[:])

	a := make([]uint32, 0, k)
	inA := func(index uint32) bool {
		for _, val := range a {
			if index == val {
				return true
			}
		}
		return false
	}
	h := sha1.New() // nolint: gosec
	for j := 0; j < k && len(a) < k; j++ {
		_, _ = h.Write(x)
		x = h.Sum(x[:0])
		h.Reset()
		for i := 0; i < 20 && len(a) < k; i += 4 {
			y := binary.BigEndian.Uint32(x[i : i+4])
			index := y % numPieces
			if !inA(index) {
				a = append(a, index)
			}
		}
	}
	return a
}
