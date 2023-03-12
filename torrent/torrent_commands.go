package torrent

import (
	"errors"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/magnet"
	"github.com/cenkalti/rain/internal/metainfo"
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

// Announce torrent to trackers and DHT manually.
func (t *torrent) Announce() {
	select {
	case t.announceCommandC <- struct{}{}:
	case <-t.closeC:
	}
}

// Verify pieces by checking files.
func (t *torrent) Verify() {
	select {
	case t.verifyCommandC <- struct{}{}:
	case <-t.closeC:
	}
}

// Close this torrent and release all resources.
// Close must be called before discarding the torrent.
func (t *torrent) Close() {
	close(t.closeC)
	<-t.doneC
}

func (t *torrent) NotifyClose() <-chan struct{} {
	return t.closeC
}

func (t *torrent) NotifyComplete() <-chan struct{} {
	return t.completeC
}

func (t *torrent) NotifyMetadata() <-chan struct{} {
	return t.completeMetadataC
}

type notifyErrorCommand struct {
	errCC chan chan error
}

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

func (t *torrent) Magnet() (string, error) {
	if t.info != nil && t.info.Private {
		return "", errors.New("torrent is private")
	}
	m := magnet.Magnet{
		InfoHash: t.infoHash,
		Name:     t.Name(),
		Trackers: t.getTieredTrackers(),
		Peers:    t.fixedPeers,
	}
	return m.String(), nil
}

func (t *torrent) Torrent() ([]byte, error) {
	if t.info == nil {
		return nil, errors.New("torrent metadata not ready")
	}
	webseeds := make([]string, len(t.webseedSources))
	for i, ws := range t.webseedSources {
		webseeds[i] = ws.URL
	}
	return metainfo.NewBytes(t.info.Bytes, t.getTieredTrackers(), webseeds, "")
}

func (t *torrent) getTieredTrackers() [][]string {
	var trackers [][]string
	for _, tr := range t.trackers {
		if tier, ok := tr.(*tracker.Tier); ok {
			urls := make([]string, len(tier.Trackers))
			for i, tt := range tier.Trackers {
				urls[i] = tt.URL()
			}
			trackers = append(trackers, urls)
		} else {
			trackers = append(trackers, []string{tr.URL()})
		}
	}
	return trackers
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

// TrackerStatus is status of the Tracker.
type TrackerStatus int

const (
	// NotContactedYet indicates that no announce request has been made to the tracker.
	NotContactedYet TrackerStatus = iota
	// Contacting the tracker. Sending request or waiting response from the tracker.
	Contacting
	// Working indicates that the tracker has responded as expected.
	Working
	// NotWorking indicates that the tracker didn't respond or returned an error.
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

// Tracker is a server that tracks the peers of torrents.
type Tracker struct {
	URL          string
	Status       TrackerStatus
	Leechers     int
	Seeders      int
	Error        *AnnounceError
	Warning      string
	LastAnnounce time.Time
	NextAnnounce time.Time
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

// Peer is a remote peer that is connected and completed protocol handshake.
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
	DownloadSpeed      int
	UploadSpeed        int
}

// PeerSource indicates that how the peer is found.
type PeerSource int

const (
	// SourceTracker indicates that the peer is found from one of the trackers.
	SourceTracker PeerSource = iota
	// SourceDHT indicates that the peer is found from Decentralised Hash Table.
	SourceDHT
	// SourcePEX indicates that the peer is found from another peer.
	SourcePEX
	// SourceIncoming indicates that the peer found us.
	SourceIncoming
	// SourceManual indicates that the peer is added manually via AddPeer method.
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

// Webseed is a HTTP source defined in Torrent.
// Client can download from these sources along with peers from the swarm.
type Webseed struct {
	URL           string
	Error         error
	DownloadSpeed int
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
