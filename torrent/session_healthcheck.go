package torrent

import (
	"io/ioutil"
	"runtime"
	"time"
)

// checkTorrent pings the torrent run loop periodically and crashes the program if a torrent does not respond in
// specified timeout. This is not a good behavior for a production program but it helps to find deadlocks easily,
// at least while developing.
func (s *Session) checkTorrent(t *torrent) {
	const interval = 10 * time.Second
	const timeout = 60 * time.Second
	for {
		select {
		case <-time.After(interval):
			timeout := time.NewTimer(timeout)
			select {
			case t.notifyErrorCommandC <- notifyErrorCommand{errCC: make(chan chan error, 1)}:
				timeout.Stop()
			case <-t.closeC:
				return
			case <-timeout.C:
				crash(t.id, "Torrent (id="+t.id+") does not respond.")
			}
		case <-t.closeC:
			return
		case <-s.closeC:
			return
		}
	}
}

func crash(torrentID string, msg string) {
	f, err := ioutil.TempFile("", "rain-crash-dump-"+torrentID+"-*")
	if err == nil {
		msg += " Saving goroutine stacks to: " + f.Name()
		b := make([]byte, 100<<20)
		n := runtime.Stack(b, true)
		b = b[:n]
		_, _ = f.Write(b)
		_ = f.Close()
	}
	panic(msg)
}
