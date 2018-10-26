package console

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cenkalti/rain/internal/jsonutil"
	"github.com/cenkalti/rain/rainrpc"
	"github.com/cenkalti/rain/torrent"
	"github.com/jroimartin/gocui"
)

type Console struct {
	client       *rainrpc.Client
	torrents     []rainrpc.Torrent
	errTorrents  error
	selectedID   uint64
	stats        torrent.Stats
	errStats     error
	m            sync.Mutex
	updateStatsC chan struct{}
}

func New(clt *rainrpc.Client) *Console {
	return &Console{
		client:       clt,
		updateStatsC: make(chan struct{}),
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
	err = c.drawStats(g)
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
			c.selectedID = 0
		} else {
			for _, t := range c.torrents {
				fmt.Fprintf(v, "%5d %s %s\n", t.ID, t.InfoHash, t.Name)
			}
		}
	}
	return nil
}

func (c *Console) drawStats(g *gocui.Gui) error {
	c.m.Lock()
	defer c.m.Unlock()

	maxX, maxY := g.Size()
	halfY := maxY / 2
	if v, err := g.SetView("stats", -1, halfY, maxX, maxY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Wrap = true
		fmt.Fprintln(v, "loading stats...")
	} else {
		v.Clear()
		if c.errStats != nil {
			fmt.Fprintln(v, "error:", c.errStats)
		} else {
			b, err := jsonutil.MarshalCompactPretty(c.stats)
			if err != nil {
				fmt.Fprintln(v, "error:", c.errStats)
			} else {
				fmt.Fprintln(v, string(b))
			}
		}
	}
	return nil
}

func (c *Console) updateLoop(g *gocui.Gui) {
	c.updateTorrents(g)
	c.updateStats(g)

	ticker := time.NewTicker(time.Second)
	for {
		select {
		case <-ticker.C:
			c.updateTorrents(g)
			c.updateStats(g)
		case <-c.updateStatsC:
			c.updateStats(g)
		}
	}
}

func (c *Console) updateTorrents(g *gocui.Gui) {
	resp, err := c.client.ListTorrents()

	sort.Slice(resp.Torrents, func(i, j int) bool { return resp.Torrents[i].ID < resp.Torrents[j].ID })

	c.m.Lock()
	c.torrents = resp.Torrents
	c.errTorrents = err
	if len(c.torrents) == 0 {
		c.setSelectedID(0)
	} else if c.selectedID == 0 {
		c.setSelectedID(c.torrents[0].ID)
	}
	c.m.Unlock()

	g.Update(c.drawTorrents)
}

func (c *Console) updateStats(g *gocui.Gui) {
	c.m.Lock()
	selectedID := c.selectedID
	c.m.Unlock()

	if selectedID != 0 {
		resp, err := c.client.GetTorrentStats(selectedID)
		c.m.Lock()
		c.stats = resp.Stats
		c.errStats = err
		c.m.Unlock()
	} else {
		c.m.Lock()
		c.errStats = errors.New("no torrent selected")
		c.m.Unlock()
	}

	g.Update(c.drawStats)
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func (c *Console) cursorDown(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	defer c.m.Unlock()

	cx, cy := v.Cursor()
	ox, oy := v.Origin()
	if cy+oy >= len(c.torrents)-1 {
		return nil
	}
	if err := v.SetCursor(cx, cy+1); err != nil {
		if err := v.SetOrigin(ox, oy+1); err != nil {
			return err
		}
	}
	c.setSelectedID(c.torrents[cy+oy+1].ID)
	return nil
}

func (c *Console) cursorUp(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	defer c.m.Unlock()

	cx, cy := v.Cursor()
	ox, oy := v.Origin()
	if cy+oy <= 0 {
		return nil
	}
	if err := v.SetCursor(cx, cy-1); err != nil && oy > 0 {
		if err := v.SetOrigin(ox, oy-1); err != nil {
			return err
		}
	}
	c.setSelectedID(c.torrents[cy+oy-1].ID)
	return nil
}

func (c *Console) removeTorrent(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	id := c.selectedID
	c.m.Unlock()

	_, err := c.client.RemoveTorrent(id)
	return err
}

func (c *Console) setSelectedID(id uint64) {
	changed := id != c.selectedID
	c.selectedID = id
	if changed {
		select {
		case c.updateStatsC <- struct{}{}:
		default:
		}
	}
}

func (c *Console) startTorrent(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	id := c.selectedID
	c.m.Unlock()

	_, err := c.client.StartTorrent(id)
	return err
}

func (c *Console) stopTorrent(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	id := c.selectedID
	c.m.Unlock()

	_, err := c.client.StopTorrent(id)
	return err
}
