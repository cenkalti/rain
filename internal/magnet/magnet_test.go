package magnet

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	u := "magnet:?xt=urn:btih:F60CC95E3566AF84C1AB223FD4CE80FA88E6438A&dn=sample_torrent&tr=udp%3a%2f%2ftracker.rain%3a2710"
	m, err := New(u)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(m.InfoHash[:]) != strings.ToLower("F60CC95E3566AF84C1AB223FD4CE80FA88E6438A") {
		t.Fatal("invalid info hash")
	}
	if m.Name != "sample_torrent" {
		t.Fatal("invalid name")
	}
	if len(m.Trackers) != 1 {
		t.Fatal("invalid trackers")
	}
	if m.Trackers[0][0] != "udp://tracker.rain:2710" {
		t.Fatal("invalid tracker")
	}
	s := m.String()
	if !strings.EqualFold(u, s) {
		t.Log(u)
		t.Log(s)
		t.FailNow()
	}
}
