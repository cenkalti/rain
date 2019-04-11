package tracker

import (
	"context"
	"math/rand"
)

type Tier struct {
	Trackers []Tracker
	index    int
}

var _ Tracker = (*Tier)(nil)

func NewTier(trackers []Tracker) *Tier {
	rand.Shuffle(len(trackers), func(i, j int) { trackers[i], trackers[j] = trackers[j], trackers[i] })
	return &Tier{
		Trackers: trackers,
	}
}

func (t *Tier) Announce(ctx context.Context, req AnnounceRequest) (*AnnounceResponse, error) {
	resp, err := t.Trackers[t.index].Announce(ctx, req)
	if err != nil {
		t.index = (t.index + 1) % len(t.Trackers)
	}
	return resp, err
}

func (t *Tier) URL() string {
	return t.Trackers[t.index].URL()
}
