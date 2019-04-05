package torrent

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/cenkalti/rain/internal/magnet"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/resumer"
	"github.com/cenkalti/rain/internal/resumer/boltdbresumer"
	"github.com/cenkalti/rain/internal/storage/filestorage"
	"github.com/cenkalti/rain/internal/webseedsource"
	"github.com/gofrs/uuid"
	"github.com/nictuku/dht"
)

func (s *Session) AddTorrent(r io.Reader) (*Torrent, error) {
	t, err := s.addTorrentStopped(r)
	if err != nil {
		return nil, err
	}
	return t, t.Start()
}

func (s *Session) addTorrentStopped(r io.Reader) (*Torrent, error) {
	r = io.LimitReader(r, int64(s.config.MaxTorrentSize))
	mi, err := metainfo.New(r)
	if err != nil {
		return nil, err
	}
	id, port, sto, err := s.add()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			s.releasePort(port)
		}
	}()
	t, err := newTorrent2(
		s,
		id,
		time.Now(),
		mi.Info.Hash[:],
		sto,
		mi.Info.Name,
		port,
		s.parseTrackers(mi.AnnounceList, mi.Info.IsPrivate()),
		mi.Info,
		nil, // bitfield
		resumer.Stats{},
	)
	if err != nil {
		return nil, err
	}
	t.webseedClient = &s.webseedClient
	t.webseedSources = webseedsource.NewList(mi.URLList)
	go s.checkTorrent(t)
	defer func() {
		if err != nil {
			t.Close()
		}
	}()
	rspec := &boltdbresumer.Spec{
		InfoHash: mi.Info.Hash[:],
		Dest:     sto.Dest(),
		Port:     port,
		Name:     mi.Info.Name,
		Trackers: mi.AnnounceList,
		URLList:  mi.URLList,
		Info:     mi.Info.Bytes,
		AddedAt:  t.addedAt,
	}
	err = s.resumer.Write(id, rspec)
	if err != nil {
		return nil, err
	}
	t2 := s.insertTorrent(t)
	return t2, nil
}

func (s *Session) AddURI(uri string) (*Torrent, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http", "https":
		return s.addURL(uri)
	case "magnet":
		return s.addMagnet(uri)
	default:
		return nil, errors.New("unsupported uri scheme: " + u.Scheme)
	}
}

func (s *Session) addURL(u string) (*Torrent, error) {
	client := http.Client{
		Timeout: s.config.TorrentAddHTTPTimeout,
	}
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.ContentLength > int64(s.config.MaxTorrentSize) {
		return nil, fmt.Errorf("torrent too large: %d", resp.ContentLength)
	}
	r := io.LimitReader(resp.Body, int64(s.config.MaxTorrentSize))
	return s.AddTorrent(r)
}

func (s *Session) addMagnet(link string) (*Torrent, error) {
	ma, err := magnet.New(link)
	if err != nil {
		return nil, err
	}
	id, port, sto, err := s.add()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			s.releasePort(port)
		}
	}()
	t, err := newTorrent2(
		s,
		id,
		time.Now(),
		ma.InfoHash[:],
		sto,
		ma.Name,
		port,
		s.parseTrackers(ma.Trackers, false),
		nil, // info
		nil, // bitfield
		resumer.Stats{},
	)
	if err != nil {
		return nil, err
	}
	go s.checkTorrent(t)
	defer func() {
		if err != nil {
			t.Close()
		}
	}()
	rspec := &boltdbresumer.Spec{
		InfoHash:    ma.InfoHash[:],
		Dest:        sto.Dest(),
		Port:        port,
		Name:        ma.Name,
		Trackers:    ma.Trackers,
		MagnetPeers: ma.Peers,
		AddedAt:     t.addedAt,
	}
	err = s.resumer.Write(id, rspec)
	if err != nil {
		return nil, err
	}
	t2 := s.insertTorrent(t)
	return t2, t2.Start()
}

func (s *Session) add() (id string, port int, sto *filestorage.FileStorage, err error) {
	port, err = s.getPort()
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			s.releasePort(port)
		}
	}()
	u1, err := uuid.NewV1()
	if err != nil {
		return
	}
	id = base64.RawURLEncoding.EncodeToString(u1[:])
	dest := filepath.Join(s.config.DataDir, id)
	sto, err = filestorage.New(dest)
	if err != nil {
		return
	}
	return
}

func (s *Session) insertTorrent(t *torrent) *Torrent {
	t2 := &Torrent{
		torrent: t,
	}
	s.mTorrents.Lock()
	defer s.mTorrents.Unlock()
	s.torrents[t.id] = t2
	ih := dht.InfoHash(t.InfoHash())
	s.torrentsByInfoHash[ih] = append(s.torrentsByInfoHash[ih], t2)
	return t2
}
