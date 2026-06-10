package udptracker

import (
	"testing"
	"time"
)

// TestUDPBackOff verifies the BEP-15 retransmission schedule: the timeout is
// 15 * 2^n seconds, with n increasing from 0 and capped at 8 (3840s).
func TestUDPBackOff(t *testing.T) {
	want := []time.Duration{
		15 * time.Second,
		30 * time.Second,
		60 * time.Second,
		120 * time.Second,
		240 * time.Second,
		480 * time.Second,
		960 * time.Second,
		1920 * time.Second,
		3840 * time.Second,
		3840 * time.Second, // n capped at 8
		3840 * time.Second,
	}
	var b udpBackOff
	for i, w := range want {
		if got := b.NextBackOff(); got != w {
			t.Errorf("call %d: got %s, want %s", i, got, w)
		}
	}
}
