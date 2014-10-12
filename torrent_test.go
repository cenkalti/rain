package rain

import "testing"

func TestTorrent(t *testing.T) {
	tor, err := newTorrent("testfiles/ubuntu-14.04.1-server-amd64.iso.torrent")
	if err != nil {
		t.Fatal(err)
	}

	if tor.Info.Name != "ubuntu-14.04.1-server-amd64.iso" {
		t.Errorf("invalid name: %q", tor.Info.Name)
	}

	if tor.Info.Length != 599785472 {
		t.Errorf("invalid length: %d", tor.Info.Length)
	}

	if tor.Info.Hash.String() != "2d066c94480adcf52bfd1185a75eb4ddc1777673" {
		t.Errorf("invalid info hash: %q must be '2d066c94480adcf52bfd1185a75eb4ddc1777673'", tor.Info.Hash)
	}
}
