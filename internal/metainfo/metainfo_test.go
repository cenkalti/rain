package metainfo

import (
	"encoding/hex"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTorrent(t *testing.T) {
	f, err := os.Open("testdata/ubuntu-14.04.1-server-amd64.iso.torrent")
	if err != nil {
		t.Fatal(err)
	}

	tor, err := New(f)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "ubuntu-14.04.1-server-amd64.iso", tor.Info.Name)
	assert.Equal(t, int64(599785472), tor.Info.Length)
	assert.Equal(t, "2d066c94480adcf52bfd1185a75eb4ddc1777673", hex.EncodeToString(tor.Info.Hash[:]))
	assert.Equal(t, [][]string{
		{"http://torrent.ubuntu.com:6969/announce"},
		{"http://ipv6.torrent.ubuntu.com:6969/announce"},
	}, tor.AnnounceList)
}
