package blocklist

import (
	"bytes"
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
	n, err := b.Reload(f)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("loaded %d values", n)
	if !b.Blocked(net.ParseIP("6.1.2.3")) {
		t.Errorf("must contain")
	}
	if b.Blocked(net.ParseIP("176.240.195.107")) {
		t.Errorf("must not contain")
	}
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
	if b.Blocked(net.ParseIP("0.0.0.0")) {
		t.Errorf("must not contain")
	}
	if b.Blocked(net.ParseIP("176.240.195.107")) {
		t.Errorf("must not contain")
	}
}
