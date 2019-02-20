package torrent

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/backoff"
)

func (s *Session) startBlocklistReloader() error {
	if s.config.BlocklistURL == "" {
		return nil
	}
	blocklistTimestamp, err := s.getBlocklistTimestamp()
	if err != nil {
		return err
	}

	s.mBlocklist.Lock()
	s.blocklistTimestamp = blocklistTimestamp
	s.mBlocklist.Unlock()

	deadline := blocklistTimestamp.Add(s.config.BlocklistUpdateInterval)
	now := time.Now().UTC()
	delta := now.Sub(deadline)
	if blocklistTimestamp.IsZero() {
		s.log.Infof("Blocklist is empty. Loading blacklist...")
		s.retryReloadBlocklist()
	} else if delta > 0 {
		s.log.Infof("Last blocklist reload was %s ago. Reloading blacklist...", delta.String())
		s.retryReloadBlocklist()
	}
	go s.blocklistReloader(delta)
	return nil
}

func (s *Session) getBlocklistTimestamp() (time.Time, error) {
	var t time.Time
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(sessionBucket)
		val := b.Get(blocklistTimestampKey)
		if val == nil {
			return nil
		}
		var err2 error
		t, err2 = time.ParseInLocation(time.RFC3339, string(val), time.UTC)
		return err2
	})
	return t, err
}

func (s *Session) retryReloadBlocklist() {
	bo := backoff.NewExponentialBackOff()
	bo.MaxElapsedTime = 0

	ticker := backoff.NewTicker(bo)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			err := s.reloadBlocklist()
			if err != nil {
				s.log.Errorln("cannot load blocklist:", err.Error())
				continue
			}
			return
		case <-s.closeC:
			return
		}
	}
}

func (s *Session) reloadBlocklist() error {
	resp, err := http.Get(s.config.BlocklistURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return errors.New("invalid blocklist status code")
	}

	buf := bytes.NewBuffer(make([]byte, 0, resp.ContentLength))
	r := io.TeeReader(resp.Body, buf)

	n, err := s.blocklist.Reload(r)
	if err != nil {
		return err
	}
	s.log.Infof("Loaded %d rules from blocklist.", n)

	now := time.Now()

	s.mBlocklist.Lock()
	s.blocklistTimestamp = now
	s.mBlocklist.Unlock()

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(sessionBucket)
		err2 := b.Put(blocklistKey, buf.Bytes())
		if err2 != nil {
			return err2
		}
		return b.Put(blocklistTimestampKey, []byte(now.UTC().Format(time.RFC3339)))
	})
}

func (s *Session) blocklistReloader(d time.Duration) {
	for {
		select {
		case <-time.After(d):
		case <-s.closeC:
			return
		}

		s.retryReloadBlocklist()
		d = s.config.BlocklistUpdateInterval
	}
}
