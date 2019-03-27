package torrent

import (
	"net"
	"time"

	"github.com/cenkalti/rain/internal/tracker"
)

// Start downloading.
// After all files are downloaded, seeding continues until the torrent is stopped.
func (t *torrent) Start() {
	select {
	case t.startCommandC <- struct{}{}:
	case <-t.closeC:
	}
}

// Stop downloading and seeding.
// Stop closes all peer connections.
func (t *torrent) Stop() {
	select {
	case t.stopCommandC <- struct{}{}:
	case <-t.closeC:
	}
}

// Close this torrent and release all resources.
// Close must be called before discarding the torrent.
func (t *torrent) Close() {
	close(t.closeC)
	<-t.doneC
}

// NotifyComplete returns a channel for notifying completion.
// The channel is closed once all pieces are downloaded successfully.
func (t *torrent) NotifyComplete() <-chan struct{} {
	return t.completeC
}

type notifyErrorCommand struct {
	errCC chan chan error
}

// NotifyError returns a new channel for notifying fatal errors.
// When an error is sent to the channel, torrent is stopped automatically.
// NotifyError must be called after calling Start().
func (t *torrent) NotifyError() <-chan error {
	cmd := notifyErrorCommand{errCC: make(chan chan error)}
	select {
	case t.notifyErrorCommandC <- cmd:
		return <-cmd.errCC
	case <-t.closeC:
		return nil
	}
}

type notifyListenCommand struct {
	portCC chan chan int
}

// NotifyListen returns a new channel that is signalled after torrent has started to listen on peer port.
// NotifyListen must be called after calling Start().
func (t *torrent) NotifyListen() <-chan int {
	cmd := notifyListenCommand{portCC: make(chan chan int)}
	select {
	case t.notifyListenCommandC <- cmd:
		return <-cmd.portCC
	case <-t.closeC:
		return nil
	}
}

type statsRequest struct {
	Response chan Stats
}

// Stats returns statistics about the Torrent.
func (t *torrent) Stats() Stats {
	var stats Stats
	req := statsRequest{Response: make(chan Stats, 1)}
	select {
	case t.statsCommandC <- req:
	case <-t.closeC:
	}
	select {
	case stats = <-req.Response:
	case <-t.closeC:
	}
	return stats
}

func (t *torrent) AddPeers(peers []*net.TCPAddr) {
	select {
	case t.addPeersCommandC <- peers:
	case <-t.closeC:
	}
}

func (t *torrent) AddTrackers(trackers []tracker.Tracker) {
	select {
	case t.addTrackersCommandC <- trackers:
	case <-t.closeC:
	}
}

type TrackerStatus int

const (
	NotContactedYet TrackerStatus = iota
	Contacting
	Working
	NotWorking
)

func trackerStatusToString(s TrackerStatus) string {
	m := map[TrackerStatus]string{
		NotContactedYet: "Not contacted yet",
		Contacting:      "Contacting",
		Working:         "Working",
		NotWorking:      "Not working",
	}
	return m[s]
}

type Tracker struct {
	URL      string
	Status   TrackerStatus
	Leechers int
	Seeders  int
	Error    error
}

type trackersRequest struct {
	Response chan []Tracker
}

func (t *torrent) Trackers() []Tracker {
	var trackers []Tracker
	req := trackersRequest{Response: make(chan []Tracker, 1)}
	select {
	case t.trackersCommandC <- req:
	case <-t.closeC:
	}
	select {
	case trackers = <-req.Response:
	case <-t.closeC:
	}
	return trackers
}

type Peer struct {
	ID                 [20]byte
	Client             string
	Addr               net.Addr
	Source             PeerSource
	ConnectedAt        time.Time
	Downloading        bool
	ClientInterested   bool
	ClientChoking      bool
	PeerInterested     bool
	PeerChoking        bool
	OptimisticUnchoked bool
	Snubbed            bool
	EncryptedHandshake bool
	EncryptedStream    bool
	DownloadSpeed      uint
	UploadSpeed        uint
}

type PeerSource int

const (
	SourceTracker PeerSource = iota
	SourceDHT
	SourcePEX
	SourceIncoming
	SourceManual
)

type peersRequest struct {
	Response chan []Peer
}

func (t *torrent) Peers() []Peer {
	var peers []Peer
	req := peersRequest{Response: make(chan []Peer, 1)}
	select {
	case t.peersCommandC <- req:
	case <-t.closeC:
	}
	select {
	case peers = <-req.Response:
	case <-t.closeC:
	}
	return peers
}

type Webseed struct {
	URL           string
	Error         error
	DownloadSpeed uint
}

type webseedsRequest struct {
	Response chan []Webseed
}

func (t *torrent) Webseeds() []Webseed {
	var webseeds []Webseed
	req := webseedsRequest{Response: make(chan []Webseed, 1)}
	select {
	case t.webseedsCommandC <- req:
	case <-t.closeC:
	}
	select {
	case webseeds = <-req.Response:
	case <-t.closeC:
	}
	return webseeds
}
