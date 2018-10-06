package torrent

func (t *Torrent) Start() {
	select {
	case t.startCommandC <- struct{}{}:
	case <-t.closedC:
	}
}

func (t *Torrent) Stop() {
	select {
	case t.stopCommandC <- struct{}{}:
	case <-t.closedC:
	}
}

// Close this torrent and release all resources.
func (t *Torrent) Close() {
	select {
	case t.closeC <- struct{}{}:
	case <-t.closedC:
	}
}

// NotifyComplete returns a channel that is closed once all pieces are downloaded successfully.
func (t *Torrent) NotifyComplete() <-chan struct{} {
	return t.completeC
}

type notifyErrorCommand struct {
	errCC chan chan error
}

// NotifyError returns a new channel for waiting download errors.
//
// When error is sent to the channel, torrent is stopped automatically.
func (t *Torrent) NotifyError() <-chan error {
	cmd := notifyErrorCommand{errCC: make(chan chan error)}
	select {
	case t.notifyErrorCommandC <- cmd:
		return <-cmd.errCC
	case <-t.closedC:
		return nil
	}
}

type statsRequest struct {
	Response chan Stats
}

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
