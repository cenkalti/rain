package peerprotocol

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMessageIDString(t *testing.T) {
	cases := []struct {
		id   MessageID
		want string
	}{
		{Choke, "choke"},
		{Unchoke, "unchoke"},
		{Interested, "interested"},
		{NotInterested, "not interested"},
		{Have, "have"},
		{Bitfield, "bitfield"},
		{Request, "request"},
		{Piece, "piece"},
		{Cancel, "cancel"},
		{Port, "port"},
		{Suggest, "suggest"},
		{HaveAll, "have all"},
		{HaveNone, "have none"},
		{Reject, "reject"},
		{AllowedFast, "allowed fast"},
		{Extension, "extension"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.id.String())
		})
	}
}

func TestMessageIDStringUnknown(t *testing.T) {
	// IDs not present in the lookup table fall back to their decimal form.
	for _, id := range []MessageID{10, 11, 12, 99, 255} {
		assert.Equal(t, strconv.Itoa(int(id)), id.String())
	}
}
