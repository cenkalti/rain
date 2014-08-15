package rain

import (
	"fmt"
	"testing"
)

func TestParseMagnet(t *testing.T) {
	u := "magnet:?xt=urn:btih:F60CC95E3566AF84C1AB223FD4CE80FA88E6438A&dn=sample_torrent&tr=udp%3a%2f%2ftracker.rain%3a2710"
	m, err := ParseMagnet(u)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Print(m)
}
