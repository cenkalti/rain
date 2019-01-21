package blocklist

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/cenkalti/rain/session/internal/blocklist/stree"
)

type Blocklist struct {
	tree stree.Stree
	m    sync.RWMutex
}

func New() *Blocklist {
	return &Blocklist{}
}

func (b *Blocklist) Blocked(ip net.IP) bool {
	b.m.RLock()
	defer b.m.RUnlock()

	ip = ip.To4()
	if ip == nil {
		return false
	}

	val := binary.BigEndian.Uint32(ip)
	return b.tree.Contains(stree.ValueType(val))
}

func (b *Blocklist) Reload(r io.Reader) (int, error) {
	b.m.Lock()
	defer b.m.Unlock()

	tree, n, err := load(r)
	if err != nil {
		return n, err
	}

	b.tree = tree
	return n, nil
}

func load(r io.Reader) (stree.Stree, int, error) {
	var tree stree.Stree
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
		tree.AddRange(stree.ValueType(r.first), stree.ValueType(r.last))
		n++
	}
	tree.Build()
	return tree, n, scanner.Err()
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
