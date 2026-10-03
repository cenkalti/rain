package console

import (
	"fmt"
	"os"
	"strings"

	"github.com/jroimartin/gocui"
)

func (c *Console) quit(g *gocui.Gui, v *gocui.View) error {
	c.selectedPage = torrents
	return nil
}

func (c *Console) forceQuit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func (c *Console) addTorrentHandleEnter(g *gocui.Gui, v *gocui.View) error {
	handleError := func(err error) error {
		v.Clear()
		_ = v.SetCursor(0, 0)
		fmt.Fprintln(v, "error:", err)
		return nil
	}
	for _, line := range v.BufferLines() {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var err error
		if isURI(line) {
			_, err = c.client.AddURI(line, nil)
		} else {
			var f *os.File
			f, err = os.Open(line)
			if err != nil {
				return handleError(err)
			}
			_, err = c.client.AddTorrent(f, nil)
			_ = f.Close()
		}
		if err != nil {
			return handleError(err)
		}
	}
	v.Clear()
	c.selectedPage = torrents
	return nil
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
			// scroll down
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

	err := c.client.RemoveTorrent(id, false)
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

func (c *Console) tabAdjustDown(g *gocui.Gui, v *gocui.View) error {
	_, maxY := g.Size()
	halfY := maxY / 2
	if c.tabAdjust < halfY-1 {
		c.tabAdjust++
	}
	return nil
}

func (c *Console) tabAdjustUp(g *gocui.Gui, v *gocui.View) error {
	_, maxY := g.Size()
	halfY := maxY / 2
	if c.tabAdjust > -halfY+1 {
		c.tabAdjust--
	}
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

func (c *Console) switchHelp(g *gocui.Gui, v *gocui.View) error {
	c.selectedPage = help
	return nil
}

func (c *Console) switchSessionStats(g *gocui.Gui, v *gocui.View) error {
	c.selectedPage = sessionStats
	return nil
}

func (c *Console) switchAddTorrent(g *gocui.Gui, v *gocui.View) error {
	c.selectedPage = addTorrent
	return nil
}
