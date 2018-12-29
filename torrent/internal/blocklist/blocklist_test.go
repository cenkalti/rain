package blocklist

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestParseCIDR(t *testing.T) {
	l := "0.0.1.1/24"
	r, err := parseCIDR([]byte(l))
	if err != nil {
		t.Fatal(err)
	}
	if r.first != 256 {
		t.Errorf("first: %d", r.first)
	}
	if r.last != 511 {
		t.Errorf("first: %d", r.first)
	}
}

func TestContains(t *testing.T) {
	p := filepath.Join("testdata", "blocklist.cidr")
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	b := New()
	n, err := b.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("loaded %d values", n)
	if !b.Contains(net.ParseIP("6.1.2.3")) {
		t.Errorf("must contain")
	}
	if b.Contains(net.ParseIP("176.240.195.107")) {
		t.Errorf("must not contain")
	}
}
