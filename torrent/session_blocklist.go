package torrent

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1" // nolint: gosec
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/backoff/v3"
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
	now := time.Now()
	var nextReload time.Duration
	switch {
	case blocklistTimestamp.IsZero():
		s.log.Infof("Blocklist is empty. Loading blocklist...")
		s.retryReloadBlocklist()
		nextReload = s.config.BlocklistUpdateInterval
	case deadline.Before(now):
		s.log.Infof("Last blocklist reload was %s ago. Reloading blocklist...", now.Sub(s.blocklistTimestamp).Truncate(time.Second).String())
		s.retryReloadBlocklist()
		nextReload = s.config.BlocklistUpdateInterval
	default:
		s.log.Infof("Loading blocklist from session db...")
		err = s.loadBlocklistFromDB()
		if err != nil {
			s.log.Errorln("Couldn't load blocklist from sesson db:", err)
			s.log.Infof("Loading blocklist from remote URL...")
			s.retryReloadBlocklist()
			nextReload = s.config.BlocklistUpdateInterval
		} else {
			nextReload = deadline.Sub(now)
		}
	}
	go s.blocklistReloader(nextReload)
	return nil
}

func (s *Session) getBlocklistTimestamp() (time.Time, error) {
	sum := sha1.Sum([]byte(s.config.BlocklistURL)) // nolint: gosec
	var t time.Time
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(sessionBucket)
		val := b.Get(blocklistURLHashKey)
		if val == nil {
			return nil
		}
		if !bytes.Equal(val, sum[:]) {
			return nil
		}
		val = b.Get(blocklistTimestampKey)
		if val == nil {
			return nil
		}
		var err2 error
		t, err2 = time.Parse(time.RFC3339, string(val))
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
	req, err := http.NewRequest(http.MethodGet, s.config.BlocklistURL, nil)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-s.closeC:
			cancel()
		case <-ctx.Done():
		}
	}()
	req = req.WithContext(ctx)

	client := http.Client{
		Timeout: s.config.BlocklistUpdateTimeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	s.log.Infoln("Blocklist response status code:", resp.StatusCode)
	s.log.Infoln("Blocklist response content length:", resp.ContentLength)
	s.log.Infoln("Blocklist response content type:", resp.Header.Get("content-type"))

	if resp.StatusCode != 200 {
		return errors.New("invalid blocklist status code")
	}
	if resp.ContentLength == -1 {
		return errors.New("unknown content length")
	}
	if resp.ContentLength > s.config.BlocklistMaxResponseSize {
		return errors.New("response too big")
	}

	buf := make([]byte, resp.ContentLength)
	_, err = io.ReadFull(resp.Body, buf)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.Header.Get("content-type") == "application/x-gzip" {
		gr, gerr := gzip.NewReader(bytes.NewReader(buf))
		if gerr != nil {
			return gerr
		}
		defer gr.Close()
		buf, err = ioutil.ReadAll(gr)
		if err != nil {
			return err
		}
	}

	err = s.loadBlocklistReader(bytes.NewReader(buf))
	if err != nil {
		return err
	}

	now := time.Now()

	s.mBlocklist.Lock()
	s.blocklistTimestamp = now
	s.mBlocklist.Unlock()

	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(sessionBucket)
		err2 := b.Put(blocklistKey, buf)
		if err2 != nil {
			return err2
		}
		sum := sha1.Sum([]byte(s.config.BlocklistURL)) // nolint: gosec
		err2 = b.Put(blocklistURLHashKey, sum[:])
		if err2 != nil {
			return err2
		}
		return b.Put(blocklistTimestampKey, []byte(now.Format(time.RFC3339)))
	})
}

func (s *Session) loadBlocklistFromDB() error {
	return s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(sessionBucket)
		val := b.Get(blocklistKey)
		if len(val) == 0 {
			return errors.New("no blocklist data in db")
		}
		return s.loadBlocklistReader(bytes.NewReader(val))
	})
}

func (s *Session) loadBlocklistReader(r io.Reader) error {
	n, err := s.blocklist.Reload(r)
	if err != nil {
		return err
	}
	s.log.Infof("Loaded %d rules from blocklist.", n)
	return nil
}

func (s *Session) blocklistReloader(d time.Duration) {
	for {
		select {
		case <-time.After(d):
		case <-s.closeC:
			return
		}

		s.log.Info("Reloading blocklist...")
		s.retryReloadBlocklist()
		d = s.config.BlocklistUpdateInterval
	}
}
