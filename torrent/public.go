package torrent

import (
	"net"
)

// Start downloading.
// After all files are downloaded, seeding continues until the torrent is stopped.
func (t *Torrent) Start() {
	select {
	case t.startCommandC <- struct{}{}:
	case <-t.doneC:
	}
}

// Stop downloading and seeding.
// Stop closes all peer connections.
func (t *Torrent) Stop() {
	select {
	case t.stopCommandC <- struct{}{}:
	case <-t.doneC:
	}
}

// Close this torrent and release all resources.
// Close must be called before discarding the torrent.
func (t *Torrent) Close() {
	select {
	case t.closeC <- struct{}{}:
		<-t.doneC
	case <-t.doneC:
	}
}

// NotifyComplete returns a channel for notifying completion.
// The channel is closed once all pieces are downloaded successfully.
func (t *Torrent) NotifyComplete() <-chan struct{} {
	return t.completeC
}

type notifyErrorCommand struct {
	errCC chan chan error
}

// NotifyError returns a new channel for notifying fatal errors.
// When an error is sent to the channel, torrent is stopped automatically.
// NotifyError must be called after calling Start().
func (t *Torrent) NotifyError() <-chan error {
	cmd := notifyErrorCommand{errCC: make(chan chan error)}
	select {
	case t.notifyErrorCommandC <- cmd:
		return <-cmd.errCC
	case <-t.doneC:
		return nil
	}
}

type statsRequest struct {
	Response chan Stats
}

// Stats returns statistics about the Torrent.
func (t *Torrent) Stats() Stats {
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

func (t *Torrent) AddPeers(peers []*net.TCPAddr) {
	select {
	case t.addPeersC <- peers:
	case <-t.doneC:
	}
}
