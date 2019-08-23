package blocklist

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/cenkalti/rain/internal/blocklist/stree"
)

var errNotIPv4Address = errors.New("address is not ipv4")

type Blocklist struct {
	tree  stree.Stree
	m     sync.RWMutex
	count int
}

func New() *Blocklist {
	return &Blocklist{}
}

func (b *Blocklist) Len() int {
	b.m.RLock()
	defer b.m.RUnlock()
	return b.count
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
	b.count = n
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
			return tree, n, fmt.Errorf("cannot parse blocklist line (%q): %s", string(l), err.Error())
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
		err = errNotIPv4Address
		return
	}
	if len(ipnet.Mask) != 4 {
		err = errNotIPv4Address
		return
	}
	r.first = binary.BigEndian.Uint32(ipnet.IP)
	r.last = r.first | ^binary.BigEndian.Uint32(ipnet.Mask)
	return
}
