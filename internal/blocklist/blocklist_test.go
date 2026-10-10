package blocklist

import (
	"bytes"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCIDR(t *testing.T) {
	l := "0.0.1.1/24"
	r, err := parseCIDR([]byte(l))
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, uint32(256), r.first)
	assert.Equal(t, uint32(511), r.last)
}

func TestContains(t *testing.T) {
	p := filepath.Join("testdata", "blocklist.cidr")
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	b := New()
	n, err := b.Reload(f)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("loaded %d values", n)
	assert.True(t, b.Blocked(net.ParseIP("6.1.2.3")))
	assert.False(t, b.Blocked(net.ParseIP("176.240.195.107")))
}

func TestEmptyList(t *testing.T) {
	r := bytes.NewReader(make([]byte, 0))
	b := New()
	n, err := b.Reload(r)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("loaded %d values", n)
	}
	assert.False(t, b.Blocked(net.ParseIP("0.0.0.0")))
	assert.False(t, b.Blocked(net.ParseIP("176.240.195.107")))
}

func ip4(t *testing.T, s string) uint32 {
	t.Helper()
	ip := net.ParseIP(s).To4()
	if ip == nil {
		t.Fatalf("not an ipv4 address: %q", s)
	}
	return binary.BigEndian.Uint32(ip)
}

func TestParseRange(t *testing.T) {
	tests := []struct {
		line        string
		first, last string
	}{
		{"1.2.3.0-1.2.3.255", "1.2.3.0", "1.2.3.255"},
		{"1.2.3.4 - 1.2.3.5", "1.2.3.4", "1.2.3.5"},
		{"Some list:1.2.3.0-1.2.3.255", "1.2.3.0", "1.2.3.255"},
		{"Example Corp, Inc:1.2.3.0-1.2.3.255", "1.2.3.0", "1.2.3.255"},
		{"Example Corp, Inc, US:1.2.3.0-1.2.3.255", "1.2.3.0", "1.2.3.255"},
		{"005.006.007.000 - 005.006.007.010 , 000 , emule", "5.6.7.0", "5.6.7.10"},
		{"1.2.3.255-1.2.3.0", "1.2.3.0", "1.2.3.255"},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			r, err := parseLine([]byte(tt.line))
			assert.NoError(t, err)
			assert.Equal(t, ip4(t, tt.first), r.first)
			assert.Equal(t, ip4(t, tt.last), r.last)
		})
	}
}

func TestParseLineInvalid(t *testing.T) {
	for _, line := range []string{
		"not an address",
		"1.2.3.0-",
		"1.2.3.0-1.2.3.256",
	} {
		_, err := parseLine([]byte(line))
		assert.Error(t, err, "line %q", line)
	}
	// IPv6 is not supported.
	_, err := parseLine([]byte("2001:db8::-2001:db8::ff"))
	assert.ErrorIs(t, err, errNotIPv4Address)
}

func TestParseAllowedEntry(t *testing.T) {
	_, err := parseLine([]byte("1.2.3.0 - 1.2.3.255 , 128 , allowed"))
	assert.ErrorIs(t, err, errAllowedEntry)
}

func TestReloadFormats(t *testing.T) {
	rules := `# rules in different formats
10.0.0.0/8
Bad people:1.2.3.0-1.2.3.255
Bad people, Inc:4.4.4.0-4.4.4.255
005.006.007.000 - 005.006.007.010 , 000 , emule
009.009.009.000 - 009.009.009.255 , 200 , allowed
`
	b := New()
	n, err := b.Reload(strings.NewReader(rules))
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 4, n)
	assert.True(t, b.Blocked(net.ParseIP("10.1.2.3")))
	assert.True(t, b.Blocked(net.ParseIP("1.2.3.200")))
	assert.True(t, b.Blocked(net.ParseIP("4.4.4.4")))
	assert.True(t, b.Blocked(net.ParseIP("5.6.7.9")))
	assert.False(t, b.Blocked(net.ParseIP("5.6.7.11")))
	assert.False(t, b.Blocked(net.ParseIP("9.9.9.9")))
}
