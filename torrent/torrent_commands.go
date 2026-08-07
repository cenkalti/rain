package torrent

import (
	"errors"
	"net"
	"time"

	"github.com/cenkalti/rain/v2/internal/magnet"
	"github.com/cenkalti/rain/v2/internal/metainfo"
	"github.com/cenkalti/rain/v2/internal/tracker"
)

// sendCommand sends cmd to the torrent's run loop.
// It gives up and returns false if the torrent is closed first.
func sendCommand[T any](ch chan<- T, cmd T, closeC <-chan struct{}) bool {
	select {
	case ch <- cmd:
		return true
	case <-closeC:
		return false
	}
}

// recvResponse receives a command response from the torrent's run loop.
// It gives up and returns the zero value if the torrent is closed first.
func recvResponse[T any](ch <-chan T, closeC <-chan struct{}) T {
	select {
	case v := <-ch:
		return v
	case <-closeC:
		var zero T
		return zero
	}
}

// Start downloading.
// After all files are downloaded, seeding continues until the torrent is stopped.
func (t *torrent) Start() {
	sendCommand(t.startCommandC, struct{}{}, t.closeC)
}

// Stop downloading and seeding.
// Stop closes all peer connections.
func (t *torrent) Stop() {
	sendCommand(t.stopCommandC, struct{}{}, t.closeC)
}

// Announce torrent to trackers and DHT manually.
func (t *torrent) Announce() {
	sendCommand(t.announceCommandC, struct{}{}, t.closeC)
}

// Verify pieces by checking files.
func (t *torrent) Verify() {
	sendCommand(t.verifyCommandC, struct{}{}, t.closeC)
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
	if !sendCommand(t.notifyErrorCommandC, cmd, t.closeC) {
		return nil
	}
	return <-cmd.errCC
}

type notifyListenCommand struct {
	portCC chan chan int
}

// NotifyListen returns a new channel that is signalled after torrent has started to listen on peer port.
// NotifyListen must be called after calling Start().
func (t *torrent) NotifyListen() <-chan int {
	cmd := notifyListenCommand{portCC: make(chan chan int)}
	if !sendCommand(t.notifyListenCommandC, cmd, t.closeC) {
		return nil
	}
	return <-cmd.portCC
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
	req := statsRequest{Response: make(chan Stats, 1)}
	sendCommand(t.statsCommandC, req, t.closeC)
	return recvResponse(req.Response, t.closeC)
}

func (t *torrent) AddPeers(peers []*net.TCPAddr) {
	sendCommand(t.addPeersCommandC, peers, t.closeC)
}

func (t *torrent) AddTrackers(trackers []tracker.Tracker) {
	sendCommand(t.addTrackersCommandC, trackers, t.closeC)
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
	req := trackersRequest{Response: make(chan []Tracker, 1)}
	sendCommand(t.trackersCommandC, req, t.closeC)
	return recvResponse(req.Response, t.closeC)
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
	req := peersRequest{Response: make(chan []Peer, 1)}
	sendCommand(t.peersCommandC, req, t.closeC)
	return recvResponse(req.Response, t.closeC)
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
	req := webseedsRequest{Response: make(chan []Webseed, 1)}
	sendCommand(t.webseedsCommandC, req, t.closeC)
	return recvResponse(req.Response, t.closeC)
}
