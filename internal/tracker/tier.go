package tracker

import (
	"context"
	"math/rand"
)

// Tier implements the Tracker interface and contains multiple Trackers which tries to announce to the working Tracker.
type Tier struct {
	Trackers []Tracker
	index    int
}

var _ Tracker = (*Tier)(nil)

// NewTier returns a new Tier.
func NewTier(trackers []Tracker) *Tier {
	rand.Shuffle(len(trackers), func(i, j int) { trackers[i], trackers[j] = trackers[j], trackers[i] })
	return &Tier{
		Trackers: trackers,
	}
}

// Announce a torrent to the tracker.
// If annouce fails, the next announce will be made to the next Tracker in the tier.
func (t *Tier) Announce(ctx context.Context, req AnnounceRequest) (*AnnounceResponse, error) {
	resp, err := t.Trackers[t.index].Announce(ctx, req)
	if err != nil {
		t.index = (t.index + 1) % len(t.Trackers)
	}
	return resp, err
}

// URL returns the current Tracker in the Tier.
func (t *Tier) URL() string {
	return t.Trackers[t.index].URL()
}
