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

func TestParseHybrid(t *testing.T) {
	const (
		v1   = "xt=urn:btih:631a31dd0a46257d5078c0dee4e66e26f73e42ac"
		v2   = "xt=urn:btmh:1220d8dd32ac93357c368556af3ac1d95c9d76bd0dff6fa9833ecdac3d53134efabb"
		want = "631a31dd0a46257d5078c0dee4e66e26f73e42ac"
	)
	for _, u := range []string{
		"magnet:?" + v1 + "&" + v2,
		"magnet:?" + v2 + "&" + v1,
	} {
		m, err := New(u)
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(m.InfoHash[:]) != want {
			t.Fatalf("invalid info hash for %q", u)
		}
	}
}

func TestParseV2Only(t *testing.T) {
	u := "magnet:?xt=urn:btmh:1220d8dd32ac93357c368556af3ac1d95c9d76bd0dff6fa9833ecdac3d53134efabb"
	_, err := New(u)
	if err == nil {
		t.Fatal("expected error")
	}
}
