package console

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/rain/internal/jsonutil"
	"github.com/cenkalti/rain/internal/rpctypes"
	"github.com/cenkalti/rain/rainrpc"
	"github.com/jroimartin/gocui"
)

const (
	// tabs
	general int = iota
	stats
	trackers
	peers
	webseeds
)

type Console struct {
	client          *rainrpc.Client
	torrents        []rpctypes.Torrent
	errTorrents     error
	selectedID      string
	selectedTab     int
	stats           rpctypes.Stats
	trackers        []rpctypes.Tracker
	peers           []rpctypes.Peer
	webseeds        []rpctypes.Webseed
	errDetails      error
	updatingDetails bool
	m               sync.Mutex
	updateTorrentsC chan struct{}
	updateDetailsC  chan struct{}
}

func New(clt *rainrpc.Client) *Console {
	return &Console{
		client:          clt,
		updateTorrentsC: make(chan struct{}, 1),
		updateDetailsC:  make(chan struct{}, 1),
	}
}

func (c *Console) Run() error {
	g, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		return err
	}
	defer g.Close()

	g.SetManagerFunc(c.layout)

	_ = g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit)
	_ = g.SetKeybinding("", 'q', gocui.ModNone, quit)
	_ = g.SetKeybinding("torrents", 'j', gocui.ModNone, c.cursorDown)
	_ = g.SetKeybinding("torrents", 'k', gocui.ModNone, c.cursorUp)
	_ = g.SetKeybinding("torrents", 'R', gocui.ModNone, c.removeTorrent)
	_ = g.SetKeybinding("torrents", 's', gocui.ModNone, c.startTorrent)
	_ = g.SetKeybinding("torrents", 'S', gocui.ModNone, c.stopTorrent)
	_ = g.SetKeybinding("torrents", 'a', gocui.ModNone, c.announce)
	_ = g.SetKeybinding("torrents", 'v', gocui.ModNone, c.verify)
	_ = g.SetKeybinding("torrents", 'g', gocui.ModNone, c.goTop)
	_ = g.SetKeybinding("torrents", 'G', gocui.ModNone, c.goBottom)
	_ = g.SetKeybinding("torrents", gocui.KeyCtrlG, gocui.ModNone, c.switchGeneral)
	_ = g.SetKeybinding("torrents", gocui.KeyCtrlS, gocui.ModNone, c.switchStats)
	_ = g.SetKeybinding("torrents", gocui.KeyCtrlT, gocui.ModNone, c.switchTrackers)
	_ = g.SetKeybinding("torrents", gocui.KeyCtrlP, gocui.ModNone, c.switchPeers)
	_ = g.SetKeybinding("torrents", gocui.KeyCtrlW, gocui.ModNone, c.switchWebseeds)

	go c.updateLoop(g)

	err = g.MainLoop()
	if err == gocui.ErrQuit {
		err = nil
	}
	return err
}

func (c *Console) layout(g *gocui.Gui) error {
	err := c.drawTorrents(g)
	if err != nil {
		return err
	}
	err = c.drawDetails(g)
	if err != nil {
		return err
	}
	_, err = g.SetCurrentView("torrents")
	return err
}

func (c *Console) drawTorrents(g *gocui.Gui) error {
	c.m.Lock()
	defer c.m.Unlock()

	maxX, maxY := g.Size()
	halfY := maxY / 2
	if v, err := g.SetView("torrents", -1, -1, maxX, halfY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Highlight = true
		v.SelBgColor = gocui.ColorGreen
		v.SelFgColor = gocui.ColorBlack
		fmt.Fprintln(v, "loading torrents...")
	} else {
		v.Clear()
		if c.errTorrents != nil {
			fmt.Fprintln(v, "error:", c.errTorrents)
			return nil
		}
		for _, t := range c.torrents {
			fmt.Fprintf(v, "%s %s %5d %s\n", t.ID, t.InfoHash, t.Port, t.Name)
		}
		_, cy := v.Cursor()
		_, oy := v.Origin()
		selectedRow := cy + oy
		if selectedRow < len(c.torrents) {
			c.setSelectedID(c.torrents[selectedRow].ID)
		}
	}
	return nil
}

func (c *Console) drawDetails(g *gocui.Gui) error {
	c.m.Lock()
	defer c.m.Unlock()

	maxX, maxY := g.Size()
	halfY := maxY / 2
	if v, err := g.SetView("details", -1, halfY, maxX, maxY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Wrap = true
		fmt.Fprintln(v, "loading details...")
	} else {
		v.Clear()
		if c.selectedID == "" {
			return nil
		}
		if c.updatingDetails {
			fmt.Fprintln(v, "refreshing...")
			return nil
		}
		if c.errDetails != nil {
			fmt.Fprintln(v, "error:", c.errDetails)
			return nil
		}
		switch c.selectedTab {
		case general:
			fmt.Fprintf(v, "Name: %s\n", c.stats.Name)
			fmt.Fprintf(v, "Private: %v\n", c.stats.Private)
			fmt.Fprintf(v, "Status: %s\n", c.stats.Status)
			if c.stats.Error != nil {
				fmt.Fprintf(v, "Error: %s\n", *c.stats.Error)
			}
			if c.stats.Pieces.Total > 0 {
				switch c.stats.Status {
				case "Verifying":
					fmt.Fprintf(v, "Progress: %d\n", c.stats.Pieces.Checked*100/c.stats.Pieces.Total)
				case "Allocating":
					fmt.Fprintf(v, "Progress: %d\n", c.stats.Bytes.Allocated*100/c.stats.Bytes.Total)
				default:
					fmt.Fprintf(v, "Progress: %d\n", c.stats.Pieces.Have*100/c.stats.Pieces.Total)
				}
			}
			fmt.Fprintf(v, "Peers: %d in %d out\n", c.stats.Peers.Incoming, c.stats.Peers.Outgoing)
			fmt.Fprintf(v, "Handshakes: %d in %d out\n", c.stats.Handshakes.Incoming, c.stats.Handshakes.Outgoing)
			fmt.Fprintf(v, "Addresses: %d from trackers %d from DHT %d from PEX\n", c.stats.Addresses.Tracker, c.stats.Addresses.DHT, c.stats.Addresses.PEX)
			fmt.Fprintf(v, "Download speed: %5d KiB/s\n", c.stats.Speed.Download/1024)
			fmt.Fprintf(v, "Upload speed:   %5d KiB/s\n", c.stats.Speed.Upload/1024)
			if c.stats.ETA != nil {
				fmt.Fprintf(v, "ETA: %s\n", time.Duration(*c.stats.ETA)*time.Second)
			} else {
				fmt.Fprintf(v, "ETA: N/A\n")
			}
		case stats:
			b, err := jsonutil.MarshalCompactPretty(c.stats)
			if err != nil {
				fmt.Fprintln(v, "error:", c.errDetails)
			} else {
				fmt.Fprintln(v, string(b))
			}
		case trackers:
			for i, t := range c.trackers {
				fmt.Fprintf(v, "#%d %s\n", i, t.URL)
				fmt.Fprintf(v, "    Status: %s, Seeders: %d, Leechers: %d\n", t.Status, t.Seeders, t.Leechers)
				var nextAnnounce string
				if t.NextAnnounce.IsZero() {
					nextAnnounce = "Unknown"
				} else {
					nextAnnounce = t.NextAnnounce.Time.Format(time.RFC3339)
				}
				fmt.Fprintf(v, "    Last announce: %s, Next announce: %s\n", t.LastAnnounce.Time.Format(time.RFC3339), nextAnnounce)
				if t.Warning != nil {
					fmt.Fprintf(v, "    Warning: %s\n", *t.Warning)
				}
				if t.Error != nil {
					errStr := *t.Error
					if t.ErrorUnknown {
						errStr = errStr + " (" + *t.ErrorInternal + ")"
					}
					fmt.Fprintf(v, "    Error: %s\n", errStr)
				}
			}
		case peers:
			format := "%2s %21s %7s %8s %6s %s\n"
			fmt.Fprintf(v, format, "#", "Addr", "Flags", "Download", "Upload", "Client")
			for i, p := range c.peers {
				num := fmt.Sprintf("%d", i)
				var dl string
				if p.DownloadSpeed > 0 {
					dl = fmt.Sprintf("%d", p.DownloadSpeed/1024)
				}
				var ul string
				if p.UploadSpeed > 0 {
					ul = fmt.Sprintf("%d", p.UploadSpeed/1024)
				}
				fmt.Fprintf(v, format, num, p.Addr, flags(p), dl, ul, p.Client)
			}
		case webseeds:
			format := "%2s %40s %8s %s\n"
			fmt.Fprintf(v, format, "#", "URL", "Speed", "Error")
			for i, p := range c.webseeds {
				num := fmt.Sprintf("%d", i)
				var dl string
				if p.DownloadSpeed > 0 {
					dl = fmt.Sprintf("%d", p.DownloadSpeed/1024)
				}
				var errstr string
				if p.Error != nil {
					errstr = *p.Error
				}
				fmt.Fprintf(v, format, num, p.URL, dl, errstr)
			}
		}
	}
	return nil
}

func (c *Console) updateLoop(g *gocui.Gui) {
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
		}
	}
}

func (c *Console) updateTorrents(g *gocui.Gui) {
	torrents, err := c.client.ListTorrents()

	sort.Slice(torrents, func(i, j int) bool {
		a, b := torrents[i], torrents[j]
		if a.AddedAt.Equal(b.AddedAt.Time) {
			return a.ID < b.ID
		}
		return a.AddedAt.Time.Before(b.AddedAt.Time)
	})

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
		sort.Slice(trackers, func(i, j int) bool { return trackers[i].URL < trackers[j].URL })
		c.m.Lock()
		c.trackers = trackers
		c.errDetails = err
		c.m.Unlock()
	case peers:
		peers, err := c.client.GetTorrentPeers(selectedID)
		sort.Slice(peers, func(i, j int) bool {
			a, b := peers[i], peers[j]
			if a.ConnectedAt.Equal(b.ConnectedAt.Time) {
				return a.Addr < b.Addr
			}
			return a.ConnectedAt.Time.Before(b.ConnectedAt.Time)
		})
		c.m.Lock()
		c.peers = peers
		c.errDetails = err
		c.m.Unlock()
	case webseeds:
		webseeds, err := c.client.GetTorrentWebseeds(selectedID)
		sort.Slice(webseeds, func(i, j int) bool {
			a, b := webseeds[i], webseeds[j]
			return a.URL < b.URL
		})
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

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func (c *Console) switchRow(v *gocui.View, row int) error {
	switch {
	case len(c.torrents) == 0:
		return nil
	case row < 0:
		row = 0
	case row >= len(c.torrents):
		row = len(c.torrents) - 1
	}

	_, cy := v.Cursor()
	_, oy := v.Origin()
	_, height := v.Size()

	currentRow := oy + cy

	if len(c.torrents) > height {
		if row > currentRow {
			// sroll down
			if row >= oy+height {
				// move origin
				_ = v.SetOrigin(0, row-height+1)
				_ = v.SetCursor(0, height-1)
			} else {
				_ = v.SetCursor(0, row-oy)
			}
		} else {
			// scroll up
			if row < oy {
				// move origin
				_ = v.SetOrigin(0, row)
				_ = v.SetCursor(0, 0)
			} else {
				_ = v.SetCursor(0, row-oy)
			}
		}
	} else {
		_ = v.SetOrigin(0, 0)
		_ = v.SetCursor(0, row)
	}

	c.setSelectedID(c.torrents[row].ID)
	return nil
}

func (c *Console) cursorDown(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	defer c.m.Unlock()

	_, cy := v.Cursor()
	_, oy := v.Origin()

	row := cy + oy + 1
	if row == len(c.torrents) {
		return nil
	}
	return c.switchRow(v, row)
}

func (c *Console) cursorUp(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	defer c.m.Unlock()

	_, cy := v.Cursor()
	_, oy := v.Origin()

	row := cy + oy - 1
	if row == -1 {
		return nil
	}
	return c.switchRow(v, row)
}

func (c *Console) goTop(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	defer c.m.Unlock()

	if len(c.torrents) == 0 {
		return nil
	}
	return c.switchRow(v, 0)
}

func (c *Console) goBottom(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	defer c.m.Unlock()

	if len(c.torrents) == 0 {
		return nil
	}
	return c.switchRow(v, len(c.torrents)-1)
}

func (c *Console) removeTorrent(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	id := c.selectedID
	c.m.Unlock()

	err := c.client.RemoveTorrent(id)
	if err != nil {
		return err
	}
	c.triggerUpdateTorrents()
	return nil
}

func (c *Console) setSelectedID(id string) {
	changed := id != c.selectedID
	c.selectedID = id
	if changed {
		c.triggerUpdateDetails(true)
	}
}

func (c *Console) startTorrent(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	id := c.selectedID
	c.m.Unlock()

	err := c.client.StartTorrent(id)
	if err != nil {
		return err
	}
	c.triggerUpdateDetails(true)
	return nil
}

func (c *Console) stopTorrent(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	id := c.selectedID
	c.m.Unlock()

	err := c.client.StopTorrent(id)
	if err != nil {
		return err
	}
	c.triggerUpdateDetails(true)
	return nil
}

func (c *Console) announce(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	id := c.selectedID
	c.m.Unlock()

	err := c.client.AnnounceTorrent(id)
	if err != nil {
		return err
	}
	c.triggerUpdateDetails(true)
	return nil
}

func (c *Console) verify(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	id := c.selectedID
	c.m.Unlock()

	err := c.client.VerifyTorrent(id)
	if err != nil {
		return err
	}
	c.triggerUpdateDetails(true)
	return nil
}

func (c *Console) switchGeneral(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	c.selectedTab = general
	c.m.Unlock()
	c.triggerUpdateDetails(true)
	return nil
}

func (c *Console) switchStats(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	c.selectedTab = stats
	c.m.Unlock()
	c.triggerUpdateDetails(true)
	return nil
}

func (c *Console) switchTrackers(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	c.selectedTab = trackers
	c.m.Unlock()
	c.triggerUpdateDetails(true)
	return nil
}

func (c *Console) switchPeers(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	c.selectedTab = peers
	c.m.Unlock()
	c.triggerUpdateDetails(true)
	return nil
}

func (c *Console) switchWebseeds(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	c.selectedTab = webseeds
	c.m.Unlock()
	c.triggerUpdateDetails(true)
	return nil
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

func flags(p rpctypes.Peer) string {
	var sb strings.Builder
	sb.Grow(7)
	if p.Downloading {
		sb.WriteString("A")
	} else {
		sb.WriteString(" ")
	}
	if p.ClientInterested {
		if p.PeerChoking {
			sb.WriteString("d")
		} else {
			sb.WriteString("D")
		}
	} else {
		if !p.PeerChoking {
			sb.WriteString("K")
		} else {
			sb.WriteString(" ")
		}
	}
	if p.PeerInterested {
		if p.ClientChoking {
			sb.WriteString("u")
		} else {
			sb.WriteString("U")
		}
	} else {
		if !p.ClientChoking {
			sb.WriteString("?")
		} else {
			sb.WriteString(" ")
		}
	}
	if p.OptimisticUnchoked {
		sb.WriteString("O")
	} else {
		sb.WriteString(" ")
	}
	if p.Snubbed {
		sb.WriteString("S")
	} else {
		sb.WriteString(" ")
	}
	switch p.Source {
	case "DHT":
		sb.WriteString("H")
	case "PEX":
		sb.WriteString("X")
	case "INCOMING":
		sb.WriteString("I")
	case "MANUAL":
		sb.WriteString("M")
	default:
		sb.WriteString(" ")
	}
	switch {
	case p.EncryptedStream:
		sb.WriteString("E")
	case p.EncryptedHandshake:
		sb.WriteString("e")
	default:
		sb.WriteString(" ")
	}
	return sb.String()
}
