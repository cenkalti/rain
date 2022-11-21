package blocklist

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/cenkalti/rain/internal/blocklist/stree"
)

var errNotIPv4Address = errors.New("address is not ipv4")

// Blocklist holds a list of IP ranges in a Segment Tree structure for faster lookups.
type Blocklist struct {
	logger Logger

	tree  stree.Stree
	m     sync.RWMutex
	count int
}

// Logger prints error messages during loading. Arguments are handled in the manner of fmt.Printf.
type Logger func(format string, v ...any)

// New returns a new Blocklist.
func New() *Blocklist {
	return NewLogger(nil)
}

// NewLogger returns a new Blocklist with a logger that prints error messages during loading.
func NewLogger(logger Logger) *Blocklist {
	return &Blocklist{logger: logger}
}

// Len returns the number of rules in the Blocklist.
func (b *Blocklist) Len() int {
	b.m.RLock()
	defer b.m.RUnlock()
	return b.count
}

// Blocked returns true if ip is in Blocklist.
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

// Reload the segment tree by reading new rules from a io.Reader.
func (b *Blocklist) Reload(r io.Reader) (int, error) {
	b.m.Lock()
	defer b.m.Unlock()

	tree, n, err := load(r, b.logger)
	if err != nil {
		return n, err
	}

	b.tree = *tree
	b.count = n
	return n, nil
}

func load(r io.Reader, logger Logger) (*stree.Stree, int, error) {
	var tree stree.Stree
	var n int
	var hasError bool
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
			hasError = true
			if logger != nil {
				logger("cannot parse blocklist line (%q): %q", string(l), err.Error())
			}
			continue
		}
		tree.AddRange(stree.ValueType(r.first), stree.ValueType(r.last))
		n++
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	if n == 0 && hasError {
		// Probably we couln't decode the stream correctly.
		// At least one line must be correct before we consider the load operation as successful.
		return nil, 0, errors.New("no valid rules")
	}
	tree.Build()
	return &tree, n, nil
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
