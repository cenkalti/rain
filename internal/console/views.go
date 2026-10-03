package console

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/rain/v2/internal/jsonutil"
	"github.com/jroimartin/gocui"
)

func (c *Console) layout(g *gocui.Gui) error {
	err := c.drawTitle(g)
	if err != nil {
		return err
	}
	if c.selectedPage == torrents {
		c.startUpdatingTorrents(g)
	} else {
		c.stopUpdatingTorrents()
	}
	if c.selectedPage == sessionStats {
		c.startUpdatingSessionStats(g)
	} else {
		c.stopUpdatingSessionStats()
		_ = g.DeleteView("session-stats")
	}
	if c.selectedPage != help {
		_ = g.DeleteView("help")
	}
	if c.selectedPage != addTorrent {
		_ = g.DeleteView("add-torrent")
		g.Cursor = false
	}
	switch c.selectedPage {
	case torrents:
		err = c.drawTorrents(g)
		if err != nil {
			return err
		}
		err = c.drawDetails(g)
		if err != nil {
			return err
		}
		_, err = g.SetCurrentView("torrents")
	case sessionStats:
		err = c.drawSessionStats(g)
		if err != nil {
			return err
		}
		_, err = g.SetCurrentView("session-stats")
	case help:
		err = c.drawHelp(g)
		if err != nil {
			return err
		}
		_, err = g.SetCurrentView("help")
	case addTorrent:
		err = c.drawAddTorrent(g)
		if err != nil {
			return err
		}
		g.Cursor = true
		_, err = g.SetCurrentView("add-torrent")
	}
	return err
}

func (c *Console) drawTitle(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	v, err := g.SetView("title", -1, 0, maxX, maxY)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Title = "Rain by put.io [" + c.client.Addr() + "] (Press '?' for help)"
	}
	return nil
}

func (c *Console) drawHelp(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	v, err := g.SetView("help", 5, 2, maxX-6, maxY-3)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Frame = true
		v.Title = "Help"
	} else {
		v.Clear()
	}
	fmt.Fprintln(v, "         q  Quit")
	fmt.Fprintln(v, "    j|down  move down")
	fmt.Fprintln(v, "      k|up  move up")
	fmt.Fprintln(v, "     alt+j  move tab separator down")
	fmt.Fprintln(v, "     alt+k  move tab separator up")
	fmt.Fprintln(v, "    g|home  Go to top")
	fmt.Fprintln(v, "     G|end  Go to bottom")
	fmt.Fprintln(v, "     alt+a  show session stats page")

	fmt.Fprintln(v, "")

	fmt.Fprintln(v, "     alt+g  switch to General info tab")
	fmt.Fprintln(v, "     alt+s  switch to Stats tab")
	fmt.Fprintln(v, "     alt+t  switch to Trackers tab")
	fmt.Fprintln(v, "     alt+p  switch to Peers tab")
	fmt.Fprintln(v, "     alt+w  switch to Webseeds tab")

	fmt.Fprintln(v, "")

	fmt.Fprintln(v, "    ctrl+s  Start torrent")
	fmt.Fprintln(v, "ctrl+alt+s  Stop torrent")
	fmt.Fprintln(v, "    ctrl+R  Remove torrent")
	fmt.Fprintln(v, "ctrl+alt+a  Announce torrent")
	fmt.Fprintln(v, "    ctrl+v  Verify torrent")
	fmt.Fprintln(v, "    ctrl+a  Add new torrent")

	return nil
}

func (c *Console) drawAddTorrent(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	v, err := g.SetView("add-torrent", 5, 2, maxX-6, maxY-3)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Frame = true
		v.Title = "Add Torrent (Press ctrl-q to close window)"
		v.Editable = true
		v.Wrap = true
	}
	return nil
}

func (c *Console) drawSessionStats(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	v, err := g.SetView("session-stats", 5, 2, maxX-6, maxY-3)
	if err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Frame = true
		v.Title = "Session Stats"
		fmt.Fprintln(v, "loading...")
	} else {
		v.Clear()
		if c.errSessionStats != nil {
			fmt.Fprintln(v, "error:", c.errSessionStats)
			return nil
		}
		FormatSessionStats(&c.sessionStats, v)
	}
	return nil
}

func getHeader(columns []string) string {
	var header strings.Builder
	for i, column := range columns {
		if i != 0 {
			header.WriteString(" ")
		}

		switch column {
		case "#":
			fmt.Fprintf(&header, "%3s", column)
		case "ID":
			fmt.Fprintf(&header, "%-22s", column)
		case "Name":
			header.WriteString(column)
		case "InfoHash":
			fmt.Fprintf(&header, "%-40s", column)
		case "Port":
			fmt.Fprintf(&header, "%5s", column)
		case "Status":
			fmt.Fprintf(&header, "%-11s", column)
		case "Speed":
			fmt.Fprintf(&header, "%8s", column)
		case "ETA":
			fmt.Fprintf(&header, "%8s", column)
		case "Progress":
			fmt.Fprintf(&header, "%8s", column)
		case "Ratio":
			fmt.Fprintf(&header, "%5s", column)
		case "Size":
			fmt.Fprintf(&header, "%8s", column)
		default:
			panic(fmt.Sprintf("unsupported column %s", column))
		}
	}
	return header.String()
}

func getRow(columns []string, t Torrent, index int) string {
	var row strings.Builder
	for i, column := range columns {
		if i != 0 {
			row.WriteString(" ")
		}
		stats := t.Stats
		switch column {
		case "#":
			fmt.Fprintf(&row, "%3d", index+1)
		case "ID":
			row.WriteString(t.ID)
		case "Name":
			row.WriteString(t.Name)
		case "InfoHash":
			row.WriteString(t.InfoHash)
		case "Port":
			fmt.Fprintf(&row, "%5d", t.Port)
		case "Status":
			if stats == nil {
				fmt.Fprintf(&row, "%-11s", "")
			} else {
				status := stats.Status
				if status == "Downloading Metadata" {
					status = "Downloading"
				}
				fmt.Fprintf(&row, "%-11s", status)
			}
		case "Speed":
			switch {
			case stats == nil:
				fmt.Fprintf(&row, "%8s", "")
			case stats.Status == "Seeding":
				fmt.Fprintf(&row, "%6d K", stats.Speed.Upload/1024)
			default:
				fmt.Fprintf(&row, "%6d K", stats.Speed.Download/1024)
			}
		case "ETA":
			if stats == nil {
				fmt.Fprintf(&row, "%8s", "")
			} else {
				fmt.Fprintf(&row, "%8s", getETA(stats))
			}
		case "Progress":
			if stats == nil {
				fmt.Fprintf(&row, "%8s", "")
			} else {
				fmt.Fprintf(&row, "%8d", getProgress(stats))
			}
		case "Ratio":
			if stats == nil {
				fmt.Fprintf(&row, "%5s", "")
			} else {
				fmt.Fprintf(&row, "%5.2f", getRatio(stats))
			}
		case "Size":
			if stats == nil {
				fmt.Fprintf(&row, "%8s", "")
			} else {
				fmt.Fprintf(&row, "%6d M", stats.Bytes.Total/(1<<20))
			}
		default:
			panic(fmt.Sprintf("unsupported column %s", column))
		}
	}
	row.WriteString("\n")
	return row.String()
}

func (c *Console) drawTorrents(g *gocui.Gui) error {
	c.m.Lock()
	defer c.m.Unlock()

	maxX, maxY := g.Size()
	halfY := maxY / 2
	split := halfY + c.tabAdjust
	if split <= 0 {
		return nil
	}
	if v, err := g.SetView("torrents-header", -1, 0, maxX, split); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Frame = false

		fmt.Fprint(v, getHeader(c.columns))
	}
	if split <= 1 {
		return nil
	}
	if v, err := g.SetView("torrents", -1, 1, maxX, split); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Frame = false
		v.Highlight = true
		v.SelBgColor = gocui.ColorGreen
		v.SelFgColor = gocui.ColorBlack
		v.Title = "Rain"
		fmt.Fprintln(v, "loading torrents...")
	} else {
		v.Clear()
		if c.errTorrents != nil {
			fmt.Fprintln(v, "error:", c.errTorrents)
			return nil
		}

		selectedIDrow := -1
		for i, t := range c.torrents {
			fmt.Fprint(v, getRow(c.columns, t, i))

			if t.ID == c.selectedID {
				selectedIDrow = i
			}
		}

		_, cy := v.Cursor()
		_, oy := v.Origin()
		selectedRow := cy + oy
		if selectedRow < len(c.torrents) {
			if c.torrents[selectedRow].ID != c.selectedID && selectedIDrow != -1 {
				_ = v.SetCursor(0, selectedIDrow)
			} else {
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
	split := halfY + c.tabAdjust
	if v, err := g.SetView("details", -1, split, maxX, maxY); err != nil {
		if err != gocui.ErrUnknownView {
			return err
		}
		v.Wrap = true
		fmt.Fprintln(v, "loading details...")
	} else {
		v.Clear()
		switch c.selectedTab {
		case general:
			v.Title = "General Info"
		case stats:
			v.Title = "Stats"
		case trackers:
			v.Title = "Trackers"
		case peers:
			v.Title = "Peers"
		case webseeds:
			v.Title = "WebSeeds"
		}
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
			FormatStats(&c.stats, v)
		case stats:
			b, err := jsonutil.MarshalCompactPretty(c.stats)
			if err != nil {
				fmt.Fprintln(v, "error:", err)
			} else {
				fmt.Fprintln(v, string(b))
			}
		case trackers:
			for i, t := range c.trackers {
				fmt.Fprintf(v, "#%d %s\n", i+1, t.URL)
				switch t.Status {
				case "Not working":
					errStr := t.Error
					if t.ErrorUnknown {
						errStr = errStr + " (" + t.ErrorInternal + ")"
					}
					fmt.Fprintf(v, "    Status: %s, Error: %s\n", t.Status, errStr)
				default:
					if t.Warning != "" {
						fmt.Fprintf(v, "    Status: %s, Seeders: %d, Leechers: %d Warning: %s\n", t.Status, t.Seeders, t.Leechers, t.Warning)
					} else {
						fmt.Fprintf(v, "    Status: %s, Seeders: %d, Leechers: %d\n", t.Status, t.Seeders, t.Leechers)
					}
				}
				var nextAnnounce string
				if t.NextAnnounce.IsZero() {
					nextAnnounce = "Unknown"
				} else {
					nextAnnounce = t.NextAnnounce.Format(time.RFC3339)
				}
				fmt.Fprintf(v, "    Last announce: %s, Next announce: %s\n", t.LastAnnounce.Format(time.RFC3339), nextAnnounce)
			}
		case peers:
			format := "%2s %21s %7s %8s %6s %s\n"
			fmt.Fprintf(v, format, "#", "Addr", "Flags", "Download", "Upload", "Client")
			for i, p := range c.peers {
				num := strconv.Itoa(i + 1)
				var dl string
				if p.DownloadSpeed > 0 {
					dl = strconv.Itoa(p.DownloadSpeed / 1024)
				}
				var ul string
				if p.UploadSpeed > 0 {
					ul = strconv.Itoa(p.UploadSpeed / 1024)
				}
				fmt.Fprintf(v, format, num, p.Addr, flags(p), dl, ul, p.Client)
			}
		case webseeds:
			format := "%2s %40s %8s %s\n"
			fmt.Fprintf(v, format, "#", "URL", "Speed", "Error")
			for i, p := range c.webseeds {
				num := strconv.Itoa(i + 1)
				var dl string
				if p.DownloadSpeed > 0 {
					dl = strconv.Itoa(p.DownloadSpeed / 1024)
				}
				fmt.Fprintf(v, format, num, p.URL, dl, p.Error)
			}
		}
	}
	return nil
}
