package peer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClientID(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want string
	}{
		// BEP 20: two-letter client code and version between dashes.
		{"bep20", "-AZ2060-123456789012", "-AZ2060-"},
		// Rain convention: "-RN<version>-<random>".
		{"rain", "-RN0000000-abcdefghi", "-RN0000000-"},
		// Unknown convention: returned unchanged.
		{"fallback", "ABCDEFGHIJKLMNOPQRST", "ABCDEFGHIJKLMNOPQRST"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, clientID(tc.id))
		})
	}
}
