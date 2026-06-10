package tracker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventString(t *testing.T) {
	cases := map[Event]string{
		EventNone:      "empty",
		EventCompleted: "completed",
		EventStarted:   "started",
		EventStopped:   "stopped",
	}
	for e, want := range cases {
		assert.Equal(t, want, e.String())
	}
}

func TestErrorMessage(t *testing.T) {
	err := &Error{FailureReason: "torrent not allowed", RetryIn: time.Minute}
	assert.EqualError(t, err, "torrent not allowed")
}

// fakeTracker is a Tracker stub that records how many times it was announced to
// and returns a preconfigured response/error.
type fakeTracker struct {
	url  string
	resp *AnnounceResponse
	err  error
	hits int
}

func (f *fakeTracker) Announce(_ context.Context, _ AnnounceRequest) (*AnnounceResponse, error) {
	f.hits++
	return f.resp, f.err
}

func (f *fakeTracker) URL() string { return f.url }

func TestNewTierKeepsAllTrackers(t *testing.T) {
	trackers := []Tracker{
		&fakeTracker{url: "a"},
		&fakeTracker{url: "b"},
		&fakeTracker{url: "c"},
	}
	tier := NewTier(trackers)
	require.Len(t, tier.Trackers, 3)

	// NewTier shuffles, so assert the set is preserved rather than the order.
	got := map[string]bool{}
	for _, tr := range tier.Trackers {
		got[tr.URL()] = true
	}
	assert.Equal(t, map[string]bool{"a": true, "b": true, "c": true}, got)
}

func TestTierAnnounceAdvancesOnError(t *testing.T) {
	resp := &AnnounceResponse{}
	t0 := &fakeTracker{url: "t0", err: errors.New("boom")}
	t1 := &fakeTracker{url: "t1", resp: resp}
	tier := &Tier{Trackers: []Tracker{t0, t1}}

	// First announce hits t0 which fails; the tier advances to t1.
	_, err := tier.Announce(context.Background(), AnnounceRequest{})
	require.Error(t, err)
	assert.Equal(t, 1, t0.hits)
	assert.Equal(t, "t1", tier.URL())

	// Second announce now goes to t1 and succeeds.
	got, err := tier.Announce(context.Background(), AnnounceRequest{})
	require.NoError(t, err)
	assert.Same(t, resp, got)
	assert.Equal(t, 1, t1.hits)
}

func TestTierAnnounceKeepsIndexOnSuccess(t *testing.T) {
	t0 := &fakeTracker{url: "t0", resp: &AnnounceResponse{}}
	t1 := &fakeTracker{url: "t1"}
	tier := &Tier{Trackers: []Tracker{t0, t1}}

	for range 3 {
		_, err := tier.Announce(context.Background(), AnnounceRequest{})
		require.NoError(t, err)
	}
	assert.Equal(t, 3, t0.hits)
	assert.Equal(t, 0, t1.hits)
	assert.Equal(t, "t0", tier.URL())
}

func TestTierURLWrapsOutOfRangeIndex(t *testing.T) {
	t0 := &fakeTracker{url: "t0"}
	tier := &Tier{Trackers: []Tracker{t0}}
	tier.index.Store(5) // beyond the end of the slice
	assert.Equal(t, "t0", tier.URL())
}
