package externalip

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPublicIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		// Public addresses.
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"172.15.0.1", true}, // just below the 172.16.0.0/12 private block
		{"172.32.0.1", true}, // just above the 172.16.0.0/12 private block
		// RFC 1918 private ranges.
		{"10.0.0.1", false},
		{"10.255.255.255", false},
		{"172.16.0.1", false},
		{"172.31.255.255", false},
		{"192.168.0.1", false},
		{"192.168.255.255", false},
		// Loopback and link-local.
		{"127.0.0.1", false},
		{"169.254.1.1", false},
	}
	for _, tc := range cases {
		t.Run(tc.ip, func(t *testing.T) {
			ip := net.ParseIP(tc.ip).To4()
			require.NotNil(t, ip, "test IP must be a valid IPv4 address")
			assert.Equal(t, tc.want, isPublicIP(ip))
		})
	}
}
