package blocklist

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"

	"github.com/cenkalti/rain/v2/internal/blocklist/stree"
)

var (
	errNotIPv4Address = errors.New("address is not ipv4")
	errAllowedEntry   = errors.New("allowed entry")
)

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
		r, err := parseLine(l)
		if err == errAllowedEntry {
			// eMule ipfilter.dat marks a range as allowed with a level of 128
			// or higher. A blocklist cannot express exceptions, so it is
			// skipped instead of being treated as blocked.
			continue
		}
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

// parseLine parses a single blocklist rule. In addition to CIDR, the address
// range formats used by common blocklist files are recognized:
//
//   - plain range:         1.2.3.0-1.2.3.255
//   - PeerGuardian / P2P:  description:1.2.3.0-1.2.3.255
//   - eMule ipfilter.dat:  001.002.003.000 - 001.002.003.255 , 000 , description
//
// Only IPv4 addresses are considered.
func parseLine(b []byte) (ipRange, error) {
	b = bytes.TrimSpace(b)
	if bytes.IndexByte(b, '-') < 0 {
		return parseCIDR(b)
	}
	return parseRange(b)
}

// parseRange parses an address range in the forms listed in parseLine.
func parseRange(b []byte) (r ipRange, err error) {
	if i := bytes.IndexByte(b, ','); i >= 0 {
		// eMule: "first - last , level , description". A level of 128 or
		// higher means the range is allowed, not blocked.
		fields := bytes.SplitN(b, []byte(","), 3)
		level, perr := strconv.Atoi(string(bytes.TrimSpace(fields[1])))
		if perr == nil && level >= 128 {
			err = errAllowedEntry
			return
		}
		b = fields[0]
	} else if i := bytes.LastIndexByte(b, ':'); i >= 0 {
		// PeerGuardian: "description:first-last".
		b = b[i+1:]
	}

	left, right, ok := bytes.Cut(b, []byte("-"))
	if !ok {
		err = errNotIPv4Address
		return
	}
	first, ok := parseIPv4(left)
	if !ok {
		err = errNotIPv4Address
		return
	}
	last, ok := parseIPv4(right)
	if !ok {
		err = errNotIPv4Address
		return
	}
	if last < first {
		first, last = last, first
	}
	r.first, r.last = first, last
	return
}

// parseIPv4 parses a dotted-quad IPv4 address. Octets may be zero-padded, as
// written in eMule ipfilter.dat files.
func parseIPv4(b []byte) (uint32, bool) {
	parts := bytes.Split(bytes.TrimSpace(b), []byte("."))
	if len(parts) != 4 {
		return 0, false
	}
	var ip uint32
	for _, part := range parts {
		octet, err := strconv.Atoi(string(part))
		if err != nil || octet < 0 || octet > 255 {
			return 0, false
		}
		ip = ip<<8 | uint32(octet)
	}
	return ip, true
}
