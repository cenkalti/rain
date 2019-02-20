package console

import (
	"errors"
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
	trackers
	peers
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

	g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit)
	g.SetKeybinding("", 'q', gocui.ModNone, quit)
	g.SetKeybinding("torrents", 'j', gocui.ModNone, c.cursorDown)
	g.SetKeybinding("torrents", 'k', gocui.ModNone, c.cursorUp)
	g.SetKeybinding("torrents", 'R', gocui.ModNone, c.removeTorrent)
	g.SetKeybinding("torrents", 's', gocui.ModNone, c.startTorrent)
	g.SetKeybinding("torrents", 'S', gocui.ModNone, c.stopTorrent)
	g.SetKeybinding("torrents", 'g', gocui.ModNone, c.goTop)
	g.SetKeybinding("torrents", 'G', gocui.ModNone, c.goBottom)
	g.SetKeybinding("torrents", gocui.KeyCtrlG, gocui.ModNone, c.switchGeneral)
	g.SetKeybinding("torrents", gocui.KeyCtrlT, gocui.ModNone, c.switchTrackers)
	g.SetKeybinding("torrents", gocui.KeyCtrlP, gocui.ModNone, c.switchPeers)

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
			c.selectedID = ""
		} else {
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
		if c.updatingDetails {
			fmt.Fprintln(v, "refreshing...")
			return nil
		}
		if c.errDetails != nil {
			fmt.Fprintln(v, "error:", c.errDetails)
		} else {
			switch c.selectedTab {
			case general:
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
					if t.Error != nil {
						fmt.Fprintf(v, "    Error: %s\n", *t.Error)
					}
				}
			case peers:
				for i, p := range c.peers {
					fmt.Fprintf(v, "#%s Addr: %21s Flags: %s DL: %5d KiB/s UL: %5d KiB/s ID: %s\n", fmt.Sprintf("%2d", i), p.Addr, flags(p), p.DownloadSpeed/1024, p.UploadSpeed/1024, printableID(p.ID))
				}
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
			c.triggerUpdateDetails()
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

	if selectedID != "" {
		switch c.selectedTab {
		case general:
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
		}
	} else {
		c.m.Lock()
		c.errDetails = errors.New("no torrent selected")
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
	_, cy := v.Cursor()
	_, oy := v.Origin()
	_, height := v.Size()

	currentRow := oy + cy

	if len(c.torrents) > height {
		if row > currentRow {
			// sroll down
			if row >= oy+height {
				// move origin
				v.SetOrigin(0, row-height+1)
				v.SetCursor(0, height-1)
			} else {
				v.SetCursor(0, row-oy)
			}
		} else {
			// scroll up
			if row < oy {
				// move origin
				v.SetOrigin(0, row)
				v.SetCursor(0, 0)
			} else {
				v.SetCursor(0, row-oy)
			}
		}
	} else {
		v.SetOrigin(0, 0)
		v.SetCursor(0, row)
	}

	c.updatingDetails = true
	c.setSelectedID(c.torrents[row].ID)
	return nil
}

func (c *Console) cursorDown(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	defer c.m.Unlock()

	_, cy := v.Cursor()
	_, oy := v.Origin()

	row := cy + oy + 1
	if row >= len(c.torrents) {
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
	if row < 0 {
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
		c.triggerUpdateDetails()
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
	c.triggerUpdateDetails()
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
	c.triggerUpdateDetails()
	return nil
}

func (c *Console) switchGeneral(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	c.selectedTab = general
	c.m.Unlock()
	c.triggerUpdateDetails()
	return nil
}

func (c *Console) switchTrackers(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	c.selectedTab = trackers
	c.m.Unlock()
	c.triggerUpdateDetails()
	return nil
}

func (c *Console) switchPeers(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	c.selectedTab = peers
	c.m.Unlock()
	c.triggerUpdateDetails()
	return nil
}

func (c *Console) triggerUpdateDetails() {
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
	default:
		sb.WriteString(" ")
	}
	if p.EncryptedStream {
		sb.WriteString("E")
	} else if p.EncryptedHandshake {
		sb.WriteString("e")
	} else {
		sb.WriteString(" ")
	}
	return sb.String()
}

func printableID(id string) string {
	return asciify(clientID(id))
}

func clientID(id string) string {
	// ID follows BEP 20 convention
	if id[7] == '-' {
		return string(id[:8])
	}

	// Rain convention
	if strings.HasPrefix(id, "-RN") {
		i := strings.IndexRune(id[1:], '-')
		if i != -1 {
			return id[:i+2]
		}
	}

	return id
}

// asciify replaces non-ascii characters with '_'.
func asciify(id string) string {
	b := []byte(id)
	for i, val := range b {
		if val >= 32 && val < 127 {
			continue
		}
		b[i] = '_'
	}
	return string(b)
}
