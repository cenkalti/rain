package blocklist

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"

	"github.com/cenkalti/rain/client/blocklist/stree"
)

type Blocklist struct {
	// Number of items in list. Updated after Load().
	tree stree.Stree
}

func New() *Blocklist {
	return &Blocklist{}
}

func (b Blocklist) Blocked(ip net.IP) bool {
	ip = ip.To4()
	if ip == nil {
		return false
	}
	val := binary.BigEndian.Uint32(ip)
	return b.tree.Contains(stree.ValueType(val))
}

func (b *Blocklist) Load(r io.Reader) (int, error) {
	var n int
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		l := bytes.TrimSpace(scanner.Bytes())
		if len(l) == 0 {
			continue
		}
		if l[0] == '#' {
			continue
		}
		r, err := parseCIDR(l)
		if err != nil {
			continue
		}
		b.tree.AddRange(stree.ValueType(r.first), stree.ValueType(r.last))
		n++
	}
	b.tree.Build()
	return n, scanner.Err()
}

type ipRange struct {
	first, last uint32
}

func parseCIDR(b []byte) (r ipRange, err error) {
	_, ipnet, err := net.ParseCIDR(string(b))
	if err != nil {
		return
	}
	if len(ipnet.IP) != 4 {
		err = errors.New("address is not ipv4")
		return
	}
	if len(ipnet.Mask) != 4 {
		err = errors.New("address is not ipv4")
		return
	}
	r.first = binary.BigEndian.Uint32(ipnet.IP)
	r.last = r.first | ^binary.BigEndian.Uint32(ipnet.Mask)
	return
}
