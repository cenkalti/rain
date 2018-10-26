package console

import (
	"fmt"
	"sync"
	"time"

	"github.com/cenkalti/rain/rainrpc"
	"github.com/cenkalti/rain/torrent"
	"github.com/hokaccha/go-prettyjson"
	"github.com/jroimartin/gocui"
)

type Console struct {
	client      *rainrpc.Client
	torrents    []rainrpc.Torrent
	errTorrents error
	selectedID  uint64
	stats       torrent.Stats
	errStats    error
	m           sync.Mutex
}

func New(clt *rainrpc.Client) *Console {
	return &Console{
		client: clt,
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

	go c.updateLoop(g)

	err = g.MainLoop()
	if err == gocui.ErrQuit {
		err = nil
	}
	return err
}

func (c *Console) layout(g *gocui.Gui) error {
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
			f := prettyjson.NewFormatter()
			f.Indent = 0
			f.Newline = ""
			b, err := f.Marshal(c.stats)
			if err != nil {
				fmt.Fprintln(v, "error:", c.errStats)
			} else {
				fmt.Fprintln(v, string(b))
			}
		}
	}
	_, err := g.SetCurrentView("torrents")
	return err
}

func (c *Console) updateLoop(g *gocui.Gui) {
	ticker := time.NewTicker(time.Second)
	c.doUpdate(g)
	for {
		select {
		case <-ticker.C:
			c.doUpdate(g)
		}
	}
}

func (c *Console) doUpdate(g *gocui.Gui) {
	resp, err := c.client.ListTorrents()

	c.m.Lock()
	c.torrents = resp.Torrents
	c.errTorrents = err
	if len(c.torrents) == 0 {
		c.selectedID = 0
	} else if c.selectedID == 0 {
		c.selectedID = c.torrents[0].ID
	}
	selectedID := c.selectedID
	c.m.Unlock()

	g.Update(c.layout)

	resp2, err := c.client.GetTorrentStats(selectedID)
	c.m.Lock()
	c.stats = resp2.Stats
	c.errStats = err
	c.m.Unlock()

	g.Update(c.layout)
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func (c *Console) cursorDown(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	defer c.m.Unlock()

	cx, cy := v.Cursor()
	ox, oy := v.Origin()
	if cy+oy == len(c.torrents)-1 {
		return nil
	}
	if err := v.SetCursor(cx, cy+1); err != nil {
		if err := v.SetOrigin(ox, oy+1); err != nil {
			return err
		}
	}
	c.selectedID = c.torrents[cy+oy+1].ID
	return nil
}

func (c *Console) cursorUp(g *gocui.Gui, v *gocui.View) error {
	c.m.Lock()
	defer c.m.Unlock()

	cx, cy := v.Cursor()
	ox, oy := v.Origin()
	if cy+oy == 0 {
		return nil
	}
	if err := v.SetCursor(cx, cy-1); err != nil && oy > 0 {
		if err := v.SetOrigin(ox, oy-1); err != nil {
			return err
		}
	}
	c.selectedID = c.torrents[cy+oy-1].ID
	return nil
}
