package console

import (
	"fmt"
	"io"
	"os"
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
	// pages
	torrents int = iota
	sessionStats
	addTorrent
	help
)

const (
	// tabs
	general int = iota
	stats
	trackers
	peers
	webseeds
)

// Console is for drawing a text user interface for a remote Session.
type Console struct {
	client    *rainrpc.Client
	columns   []string
	needStats bool

	// protects global state in client
	m sync.Mutex

	// error from listing torrents rpc call
	errTorrents error
	// error from getting stats/trackers/peers/etc...
	errDetails error
	// error from getting session stats
	errSessionStats error

	// id of currently selected torrent
	selectedID string
	// selected detail tab
	selectedTab int
	// selected global page
	selectedPage int
	// distance Y from 0,0
	tabAdjust int

	// fields to hold responsed from rpc requests
	torrents     []Torrent
	stats        rpctypes.Stats
	sessionStats rpctypes.SessionStats
	trackers     []rpctypes.Tracker
	peers        []rpctypes.Peer
	webseeds     []rpctypes.Webseed

	// whether details tab is currently updating state
	updatingDetails bool

	// channels for triggering refresh after view update / key events
	updateTorrentsC chan struct{}
	updateDetailsC  chan struct{}

	// state for updater goroutine for updating torrents list and details tab
	stopUpdatingTorrentsC chan struct{}
	updatingTorrents      bool

	// state for updater goroutine for updating session-stats page
	stopUpdatingSessionStatsC chan struct{}
	updatingSessionStats      bool
}

// Torrent in the ssession.
type Torrent struct {
	rpctypes.Torrent
	Stats *rpctypes.Stats
}

// New returns a new Console object that uses a RPC client to get information from a torrent.Session.
func New(clt *rainrpc.Client, columns []string) *Console {
	return &Console{
		client:          clt,
		columns:         columns,
		needStats:       columnsNeedStats(columns),
		updateTorrentsC: make(chan struct{}, 1),
		updateDetailsC:  make(chan struct{}, 1),
	}
}

func columnsNeedStats(columns []string) bool {
	l := []string{"ID", "Name", "InfoHash", "Port"}
	for _, c := range columns {
		for _, d := range l {
			if c != d {
				return true
			}
		}
	}
	return false
}

// Run the UI loop.
func (c *Console) Run() error {
	g, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		return err
	}
	defer g.Close()
	g.SetManagerFunc(c.layout)
	c.keybindings(g)
	err = g.MainLoop()
	if err == gocui.ErrQuit {
		err = nil
	}
	return err
}

func (c *Console) keybindings(g *gocui.Gui) {
	// Global keys
	_ = g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, c.forceQuit)

	// Quit keys
	_ = g.SetKeybinding("torrents", 'q', gocui.ModNone, c.forceQuit)
	_ = g.SetKeybinding("help", 'q', gocui.ModNone, c.quit)
	_ = g.SetKeybinding("session-stats", 'q', gocui.ModNone, c.quit)
	_ = g.SetKeybinding("add-torrent", gocui.KeyCtrlQ, gocui.ModNone, c.quit)

	// Navigation
	_ = g.SetKeybinding("torrents", 'j', gocui.ModNone, c.cursorDown)
	_ = g.SetKeybinding("torrents", gocui.KeyArrowDown, gocui.ModNone, c.cursorDown)
	_ = g.SetKeybinding("torrents", 'k', gocui.ModNone, c.cursorUp)
	_ = g.SetKeybinding("torrents", gocui.KeyArrowUp, gocui.ModNone, c.cursorUp)
	_ = g.SetKeybinding("torrents", 'j', gocui.ModAlt, c.tabAdjustDown)
	_ = g.SetKeybinding("torrents", 'k', gocui.ModAlt, c.tabAdjustUp)
	_ = g.SetKeybinding("torrents", 'g', gocui.ModNone, c.goTop)
	_ = g.SetKeybinding("torrents", gocui.KeyHome, gocui.ModNone, c.goTop)
	_ = g.SetKeybinding("torrents", 'G', gocui.ModNone, c.goBottom)
	_ = g.SetKeybinding("torrents", gocui.KeyEnd, gocui.ModNone, c.goBottom)
	_ = g.SetKeybinding("torrents", 'a', gocui.ModAlt, c.switchSessionStats)
	_ = g.SetKeybinding("torrents", '?', gocui.ModNone, c.switchHelp)

	// Tabs
	_ = g.SetKeybinding("torrents", 'g', gocui.ModAlt, c.switchGeneral)
	_ = g.SetKeybinding("torrents", 's', gocui.ModAlt, c.switchStats)
	_ = g.SetKeybinding("torrents", 't', gocui.ModAlt, c.switchTrackers)
	_ = g.SetKeybinding("torrents", 'p', gocui.ModAlt, c.switchPeers)
	_ = g.SetKeybinding("torrents", 'w', gocui.ModAlt, c.switchWebseeds)

	// Torrent control
	_ = g.SetKeybinding("torrents", gocui.KeyCtrlS, gocui.ModNone, c.startTorrent)
	_ = g.SetKeybinding("torrents", gocui.KeyCtrlS, gocui.ModAlt, c.stopTorrent)
	_ = g.SetKeybinding("torrents", gocui.KeyCtrlR, gocui.ModNone, c.removeTorrent)
	_ = g.SetKeybinding("torrents", gocui.KeyCtrlA, gocui.ModAlt, c.announce)
	_ = g.SetKeybinding("torrents", gocui.KeyCtrlV, gocui.ModNone, c.verify)
	_ = g.SetKeybinding("torrents", gocui.KeyCtrlA, gocui.ModNone, c.switchAddTorrent)
	_ = g.SetKeybinding("add-torrent", gocui.KeyEnter, gocui.ModNone, c.addTorrentHandleEnter)
}

func (c *Console) startUpdatingTorrents(g *gocui.Gui) {
	if c.updatingTorrents {
		return
	}
	c.updatingTorrents = true
	c.stopUpdatingTorrentsC = make(chan struct{})
	go c.updateTorrentsAndDetailsLoop(g, c.stopUpdatingTorrentsC)
}

func (c *Console) stopUpdatingTorrents() {
	if !c.updatingTorrents {
		return
	}
	c.updatingTorrents = false
	close(c.stopUpdatingTorrentsC)
}

func (c *Console) startUpdatingSessionStats(g *gocui.Gui) {
	if c.updatingSessionStats {
		return
	}
	c.updatingSessionStats = true
	c.stopUpdatingSessionStatsC = make(chan struct{})
	go c.updateSessionStatsLoop(g, c.stopUpdatingSessionStatsC)
}

func (c *Console) stopUpdatingSessionStats() {
	if !c.updatingSessionStats {
		return
	}
	c.updatingSessionStats = false
	close(c.stopUpdatingSessionStatsC)
}

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
	header := ""
	for i, column := range columns {
		if i != 0 {
			header += " "
		}

		switch column {
		case "#":
			header += fmt.Sprintf("%3s", column)
		case "ID":
			header += fmt.Sprintf("%-22s", column)
		case "Name":
			header += column
		case "InfoHash":
			header += fmt.Sprintf("%-40s", column)
		case "Port":
			header += fmt.Sprintf("%5s", column)
		case "Status":
			header += fmt.Sprintf("%-11s", column)
		case "Speed":
			header += fmt.Sprintf("%8s", column)
		case "ETA":
			header += fmt.Sprintf("%8s", column)
		case "Progress":
			header += fmt.Sprintf("%8s", column)
		case "Ratio":
			header += fmt.Sprintf("%5s", column)
		case "Size":
			header += fmt.Sprintf("%8s", column)
		default:
			panic(fmt.Sprintf("unsupported column %s", column))
		}
	}
	return header
}

func getRow(columns []string, t Torrent, index int) string {
	row := ""
	for i, column := range columns {
		if i != 0 {
			row += " "
		}
		stats := t.Stats
		switch column {
		case "#":
			row += fmt.Sprintf("%3d", index+1)
		case "ID":
			row += t.ID
		case "Name":
			row += t.Name
		case "InfoHash":
			row += t.InfoHash
		case "Port":
			row += fmt.Sprintf("%5d", t.Port)
		case "Status":
			if stats == nil {
				row += fmt.Sprintf("%-11s", "")
			} else {
				status := stats.Status
				if status == "Downloading Metadata" {
					status = "Downloading"
				}
				row += fmt.Sprintf("%-11s", status)
			}
		case "Speed":
			switch {
			case stats == nil:
				row += fmt.Sprintf("%8s", "")
			case stats.Status == "Seeding":
				row += fmt.Sprintf("%6d K", stats.Speed.Upload/1024)
			default:
				row += fmt.Sprintf("%6d K", stats.Speed.Download/1024)
			}
		case "ETA":
			if stats == nil {
				row += fmt.Sprintf("%8s", "")
			} else {
				row += fmt.Sprintf("%8s", getETA(stats))
			}
		case "Progress":
			if stats == nil {
				row += fmt.Sprintf("%8s", "")
			} else {
				row += fmt.Sprintf("%8d", getProgress(stats))
			}
		case "Ratio":
			if stats == nil {
				row += fmt.Sprintf("%5s", "")
			} else {
				row += fmt.Sprintf("%5.2f", getRatio(stats))
			}
		case "Size":
			if stats == nil {
				row += fmt.Sprintf("%8s", "")
			} else {
				row += fmt.Sprintf("%6d M", stats.Bytes.Total/(1<<20))
			}
		default:
			panic(fmt.Sprintf("unsupported column %s", column))
		}
	}
	return row + "\n"
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
					nextAnnounce = t.NextAnnounce.Time.Format(time.RFC3339)
				}
				fmt.Fprintf(v, "    Last announce: %s, Next announce: %s\n", t.LastAnnounce.Time.Format(time.RFC3339), nextAnnounce)
			}
		case peers:
			format := "%2s %21s %7s %8s %6s %s\n"
			fmt.Fprintf(v, format, "#", "Addr", "Flags", "Download", "Upload", "Client")
			for i, p := range c.peers {
				num := fmt.Sprintf("%d", i+1)
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
				num := fmt.Sprintf("%d", i+1)
				var dl string
				if p.DownloadSpeed > 0 {
					dl = fmt.Sprintf("%d", p.DownloadSpeed/1024)
				}
				var errstr string
				if p.Error != "" {
					errstr = p.Error
				}
				fmt.Fprintf(v, format, num, p.URL, dl, errstr)
			}
		}
	}
	return nil
}

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

	sort.Slice(rpcTorrents, func(i, j int) bool {
		a, b := rpcTorrents[i], rpcTorrents[j]
		if a.AddedAt.Equal(b.AddedAt.Time) {
			return a.ID < b.ID
		}
		return a.AddedAt.Time.Before(b.AddedAt.Time)
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

func (c *Console) updateSessionStats(g *gocui.Gui) {
	stats, err := c.client.GetSessionStats()
	c.m.Lock()
	defer c.m.Unlock()
	c.sessionStats = *stats
	c.errSessionStats = err

	g.Update(c.drawSessionStats)
}

func (c *Console) quit(g *gocui.Gui, v *gocui.View) error {
	c.selectedPage = torrents
	return nil
}

func (c *Console) forceQuit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func isURI(arg string) bool {
	return strings.HasPrefix(arg, "magnet:") || strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://")
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
	sb.Grow(6)
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

func getProgress(stats *rpctypes.Stats) int {
	var progress int
	if stats.Pieces.Total > 0 {
		switch stats.Status {
		case "Verifying":
			progress = int(stats.Pieces.Checked * 100 / stats.Pieces.Total)
		case "Allocating":
			progress = int(stats.Bytes.Allocated * 100 / stats.Bytes.Total)
		default:
			progress = int(stats.Pieces.Have * 100 / stats.Pieces.Total)
		}
	}
	return progress
}

func getRatio(stats *rpctypes.Stats) float64 {
	var ratio float64
	if stats.Bytes.Downloaded > 0 {
		ratio = float64(stats.Bytes.Uploaded) / float64(stats.Bytes.Downloaded)
	}
	return ratio
}

func getSize(stats *rpctypes.Stats) string {
	var size string
	switch {
	case stats.Bytes.Total < 1<<10:
		size = fmt.Sprintf("%d bytes", stats.Bytes.Total)
	case stats.Bytes.Total < 1<<20:
		size = fmt.Sprintf("%d KiB", stats.Bytes.Total/(1<<10))
	default:
		size = fmt.Sprintf("%d MiB", stats.Bytes.Total/(1<<20))
	}
	return size
}

func getDownloadSpeed(stats *rpctypes.Stats) string {
	return fmt.Sprintf("%d KiB/s", stats.Speed.Download/1024)
}

func getUploadSpeed(stats *rpctypes.Stats) string {
	return fmt.Sprintf("%d KiB/s", stats.Speed.Upload/1024)
}

func getETA(stats *rpctypes.Stats) string {
	var eta string
	if stats.ETA != -1 {
		eta = (time.Duration(stats.ETA) * time.Second).String()
	}
	return eta
}

// FormatStats returns the human readable representation of torrent stats object.
func FormatStats(stats *rpctypes.Stats, v io.Writer) {
	fmt.Fprintf(v, "Name: %s\n", stats.Name)
	fmt.Fprintf(v, "Private: %v\n", stats.Private)
	status := stats.Status
	if status == "Stopped" && stats.Error != "" {
		status = status + ": " + stats.Error
	}
	fmt.Fprintf(v, "Status: %s\n", status)
	fmt.Fprintf(v, "Progress: %d%%\n", getProgress(stats))
	fmt.Fprintf(v, "Ratio: %.2f\n", getRatio(stats))
	fmt.Fprintf(v, "Size: %s\n", getSize(stats))
	fmt.Fprintf(v, "Peers: %d in / %d out\n", stats.Peers.Incoming, stats.Peers.Outgoing)
	fmt.Fprintf(v, "Download speed: %11s\n", getDownloadSpeed(stats))
	fmt.Fprintf(v, "Upload speed:   %11s\n", getUploadSpeed(stats))
	fmt.Fprintf(v, "ETA: %s\n", getETA(stats))
}

// FormatSessionStats returns the human readable representation of session stats object.
func FormatSessionStats(s *rpctypes.SessionStats, v io.Writer) {
	fmt.Fprintf(v, "Torrents: %d, Peers: %d, Uptime: %s\n", s.Torrents, s.Peers, time.Duration(s.Uptime)*time.Second)
	fmt.Fprintf(v, "BlocklistRules: %d, Updated: %s ago\n", s.BlockListRules, time.Duration(s.BlockListRecency)*time.Second)
	fmt.Fprintf(v, "Reads: %d/s, %dKB/s, Active: %d, Pending: %d\n", s.ReadsPerSecond, s.SpeedRead/1024, s.ReadsActive, s.ReadsPending)
	fmt.Fprintf(v, "Writes: %d/s, %dKB/s, Active: %d, Pending: %d\n", s.WritesPerSecond, s.SpeedWrite/1024, s.WritesActive, s.WritesPending)
	fmt.Fprintf(v, "ReadCache Objects: %d, Size: %dMB, Utilization: %d%%\n", s.ReadCacheObjects, s.ReadCacheSize/(1<<20), s.ReadCacheUtilization)
	fmt.Fprintf(v, "WriteCache Objects: %d, Size: %dMB, PendingKeys: %d\n", s.WriteCacheObjects, s.WriteCacheSize/(1<<20), s.WriteCachePendingKeys)
	fmt.Fprintf(v, "DownloadSpeed: %dKB/s, UploadSpeed: %dKB/s\n", s.SpeedDownload/1024, s.SpeedUpload/1024)
	fmt.Fprintf(v, "BytesDownloaded: %dMB, BytesUploaded: %dMB\n", s.BytesDownloaded/1024/1024, s.BytesUploaded/1024/1024)
	fmt.Fprintf(v, "BytesRead: %dMB, BytesWritten: %dMB\n", s.BytesRead/1024/1024, s.BytesWritten/1024/1024)
}
