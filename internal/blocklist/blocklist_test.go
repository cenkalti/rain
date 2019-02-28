package blocklist

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
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
