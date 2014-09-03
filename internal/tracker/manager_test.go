package tracker

import "testing"

func TestManager(t *testing.T) {
	const url = "magnet:?xt=urn:btih:f60cc95e3566af84c1ab223fd4ce80fa88e6438a&dn=sample%5Ftorrent&tr=udp://178.62.175.45:6969"
	m := NewManager(nil)
	tr, err := m.NewTracker(url)
	if err != nil {
		t.Fatal(err)
	}
	if m.trackers[url].count != 1 {
		t.Fatal("not 1")
	}
	tr2, err := m.NewTracker(url)
	if err != nil {
		t.Fatal(err)
	}
	if m.trackers[url].count != 2 {
		t.Fatal("not 2")
	}
	if tr != tr2 {
		t.Fatal("not equal")
	}
	tr.Close()
	if m.trackers[url].count != 1 {
		t.Fatal("not 1")
	}
	tr2.Close()
	if _, ok := m.trackers[url]; ok {
		t.Fatal("ok")
	}
}
