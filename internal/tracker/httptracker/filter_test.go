package httptracker

import (
	"net"
	"testing"
)

func TestFilterExternalIP(t *testing.T) {
	addr := func(ip string) *net.TCPAddr {
		return &net.TCPAddr{IP: net.ParseIP(ip).To4(), Port: 6881}
	}
	ipBytes := func(ip string) []byte {
		return []byte(net.ParseIP(ip).To4())
	}
	got := func(peers []*net.TCPAddr) []string {
		out := make([]string, len(peers))
		for i, p := range peers {
			out[i] = p.IP.String()
		}
		return out
	}
	equal := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	cases := []struct {
		name       string
		peers      []*net.TCPAddr
		externalIP []byte
		want       []string
	}{
		{
			name:       "no external ip is a no-op",
			peers:      []*net.TCPAddr{addr("1.1.1.1"), addr("2.2.2.2")},
			externalIP: nil,
			want:       []string{"1.1.1.1", "2.2.2.2"},
		},
		{
			// self IP at the front: must be removed while every other peer is kept.
			name:       "removes self at front, keeps the rest",
			peers:      []*net.TCPAddr{addr("1.1.1.1"), addr("2.2.2.2"), addr("3.3.3.3")},
			externalIP: ipBytes("1.1.1.1"),
			want:       []string{"2.2.2.2", "3.3.3.3"},
		},
		{
			name:       "removes self in the middle",
			peers:      []*net.TCPAddr{addr("2.2.2.2"), addr("1.1.1.1"), addr("3.3.3.3")},
			externalIP: ipBytes("1.1.1.1"),
			want:       []string{"2.2.2.2", "3.3.3.3"},
		},
		{
			name:       "external ip not present keeps all",
			peers:      []*net.TCPAddr{addr("2.2.2.2"), addr("3.3.3.3")},
			externalIP: ipBytes("9.9.9.9"),
			want:       []string{"2.2.2.2", "3.3.3.3"},
		},
	}

	for _, tc := range cases {
		if out := got(filterExternalIP(tc.peers, tc.externalIP)); !equal(out, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, out, tc.want)
		}
	}
}
