package peersource

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSourceString(t *testing.T) {
	cases := map[Source]string{
		Tracker:  "tracker",
		DHT:      "dht",
		PEX:      "pex",
		Manual:   "manual",
		Incoming: "incoming",
	}
	for s, want := range cases {
		assert.Equal(t, want, s.String())
	}
}

func TestSourceStringPanicsOnUnknown(t *testing.T) {
	assert.Panics(t, func() { _ = Source(99).String() })
}
