package console

import (
	"sync"

	"github.com/cenkalti/rain/v2/internal/rpctypes"
	"github.com/cenkalti/rain/v2/rainrpc"
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
