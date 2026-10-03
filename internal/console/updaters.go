package console

import (
	"cmp"
	"slices"
	"sync"
	"time"

	"github.com/cenkalti/rain/v2/internal/rpctypes"
	"github.com/jroimartin/gocui"
)

func (c *Console) updateTorrentsAndDetailsLoop(g *gocui.Gui, stop chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	c.triggerUpdateTorrents()
	for {
		select {
		case <-ticker.C:
			c.triggerUpdateTorrents()
			c.triggerUpdateDetails(false)
		case <-c.updateTorrentsC:
			c.updateTorrents(g)
		case <-c.updateDetailsC:
			go c.updateDetails(g)
		case <-stop:
			return
		}
	}
}

func (c *Console) updateSessionStatsLoop(g *gocui.Gui, stop chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	c.updateSessionStats(g)
	for {
		select {
		case <-ticker.C:
			c.updateSessionStats(g)
		case <-stop:
			return
		}
	}
}

func (c *Console) updateTorrents(g *gocui.Gui) {
	rpcTorrents, err := c.client.ListTorrents()

	slices.SortFunc(rpcTorrents, func(a, b rpctypes.Torrent) int {
		return cmp.Or(
			a.AddedAt.Compare(b.AddedAt.Time),
			cmp.Compare(a.ID, b.ID),
		)
	})

	torrents := make([]Torrent, 0, len(rpcTorrents))
	for _, t := range rpcTorrents {
		torrents = append(torrents, Torrent{Torrent: t})
	}

	// Get torrent stats in parallel
	if c.needStats {
		inside := c.rowsInsideView(g)
		var wg sync.WaitGroup
		for _, i := range inside {
			if i < len(torrents) {
				t := &torrents[i]
				wg.Add(1)
				go func(t *Torrent) {
					t.Stats, _ = c.client.GetTorrentStats(t.ID)
					wg.Done()
				}(t)
			}
		}
		wg.Wait()
	}

	c.m.Lock()
	c.torrents = torrents
	c.errTorrents = err
	if len(c.torrents) == 0 {
		c.setSelectedID("")
	} else if c.selectedID == "" {
		c.setSelectedID(c.torrents[0].ID)
	}
	c.m.Unlock()

	g.Update(c.drawTorrents)
}

func (c *Console) rowsInsideView(g *gocui.Gui) []int {
	c.m.Lock()
	defer c.m.Unlock()
	v, err := g.View("torrents")
	if err != nil {
		return nil
	}
	if c.errTorrents != nil {
		return nil
	}
	_, maxY := g.Size()
	halfY := maxY / 2
	split := halfY + c.tabAdjust
	_, oy := v.Origin()
	var ret []int
	for i := oy; i < oy+split-2; i++ {
		ret = append(ret, i)
	}
	return ret
}

func (c *Console) updateDetails(g *gocui.Gui) {
	c.m.Lock()
	selectedID := c.selectedID
	c.m.Unlock()

	if selectedID == "" {
		return
	}

	switch c.selectedTab {
	case general, stats:
		stats, err := c.client.GetTorrentStats(selectedID)
		c.m.Lock()
		c.stats = *stats
		c.errDetails = err
		c.m.Unlock()
	case trackers:
		trackers, err := c.client.GetTorrentTrackers(selectedID)
		slices.SortFunc(trackers, func(a, b rpctypes.Tracker) int { return cmp.Compare(a.URL, b.URL) })
		c.m.Lock()
		c.trackers = trackers
		c.errDetails = err
		c.m.Unlock()
	case peers:
		peers, err := c.client.GetTorrentPeers(selectedID)
		slices.SortFunc(peers, func(a, b rpctypes.Peer) int {
			return cmp.Or(
				a.ConnectedAt.Compare(b.ConnectedAt.Time),
				cmp.Compare(a.Addr, b.Addr),
			)
		})
		c.m.Lock()
		c.peers = peers
		c.errDetails = err
		c.m.Unlock()
	case webseeds:
		webseeds, err := c.client.GetTorrentWebseeds(selectedID)
		slices.SortFunc(webseeds, func(a, b rpctypes.Webseed) int { return cmp.Compare(a.URL, b.URL) })
		c.m.Lock()
		c.webseeds = webseeds
		c.errDetails = err
		c.m.Unlock()
	}

	c.m.Lock()
	defer c.m.Unlock()
	c.updatingDetails = false
	if selectedID != c.selectedID {
		return
	}
	g.Update(c.drawDetails)
}

func (c *Console) updateSessionStats(g *gocui.Gui) {
	stats, err := c.client.GetSessionStats()
	c.m.Lock()
	defer c.m.Unlock()
	c.sessionStats = *stats
	c.errSessionStats = err

	g.Update(c.drawSessionStats)
}

func (c *Console) triggerUpdateDetails(clear bool) {
	if clear {
		c.updatingDetails = true
	}
	select {
	case c.updateDetailsC <- struct{}{}:
	default:
	}
}

func (c *Console) triggerUpdateTorrents() {
	select {
	case c.updateTorrentsC <- struct{}{}:
	default:
	}
}
