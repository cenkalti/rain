package torrent

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
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

// AddTorrentOptions contains options for adding a new torrent.
type AddTorrentOptions struct {
	// ID uniquely identifies the torrent in Session.
	// If empty, a random ID is generated.
	ID string
	// Do not start torrent automatically after adding.
	Stopped bool
	// Stop torrent after all pieces are downloaded.
	StopAfterDownload bool
}

// AddTorrent adds a new torrent to the session by reading .torrent metainfo from reader.
// Nil value can be passed as opt for default options.
func (s *Session) AddTorrent(r io.Reader, opt *AddTorrentOptions) (*Torrent, error) {
	if opt == nil {
		opt = &AddTorrentOptions{}
	}
	t, err := s.addTorrentStopped(r, opt)
	if err != nil {
		return nil, err
	}
	if !opt.Stopped {
		err = t.Start()
	}
	return t, err
}

func (s *Session) parseMetaInfo(r io.Reader) (*metainfo.MetaInfo, error) {
	mi, err := metainfo.New(r)
	if err != nil {
		return nil, err
	}
	if mi.Info.NumPieces > s.config.MaxPieces {
		return nil, errTooManyPieces
	}
	return mi, nil
}

func (s *Session) addTorrentStopped(r io.Reader, opt *AddTorrentOptions) (*Torrent, error) {
	r = io.LimitReader(r, int64(s.config.MaxTorrentSize))
	mi, err := s.parseMetaInfo(r)
	if err != nil {
		return nil, newInputError(err)
	}
	id, port, sto, err := s.add(opt)
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
		s.parseTrackers(mi.AnnounceList, mi.Info.Private),
		nil, // fixedPeers
		&mi.Info,
		nil, // bitfield
		resumer.Stats{},
		webseedsource.NewList(mi.URLList),
		opt.StopAfterDownload,
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
		InfoHash:          mi.Info.Hash[:],
		Port:              port,
		Name:              mi.Info.Name,
		Trackers:          mi.AnnounceList,
		URLList:           mi.URLList,
		Info:              mi.Info.Bytes,
		AddedAt:           t.addedAt,
		StopAfterDownload: opt.StopAfterDownload,
	}
	err = s.resumer.Write(id, rspec)
	if err != nil {
		return nil, err
	}
	t2 := s.insertTorrent(t)
	return t2, nil
}

// AddURI adds a new torrent to the session from a URI.
// URI may be a magnet link or a HTTP URL.
// In case of a HTTP address, a torrent is tried to be downloaded from that URL.
// Nil value can be passed as opt for default options.
func (s *Session) AddURI(uri string, opt *AddTorrentOptions) (*Torrent, error) {
	uri = filterOutControlChars(uri)
	if opt == nil {
		opt = &AddTorrentOptions{}
	}
	u, err := url.Parse(uri)
	if err != nil {
		return nil, newInputError(err)
	}
	switch u.Scheme {
	case "http", "https":
		return s.addURL(uri, opt)
	case "magnet":
		return s.addMagnet(uri, opt)
	default:
		return nil, errors.New("unsupported uri scheme: " + u.Scheme)
	}
}

func filterOutControlChars(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < ' ' || b == 0x7f {
			continue
		}
		sb.WriteByte(b)
	}
	return sb.String()
}

func (s *Session) addURL(u string, opt *AddTorrentOptions) (*Torrent, error) {
	client := http.Client{
		Timeout: s.config.TorrentAddHTTPTimeout,
	}
	resp, err := client.Get(u)
	if err != nil {
		return nil, newInputError(err)
	}
	defer resp.Body.Close()

	if resp.ContentLength > int64(s.config.MaxTorrentSize) {
		return nil, newInputError(fmt.Errorf("torrent too large: %d", resp.ContentLength))
	}
	r := io.LimitReader(resp.Body, int64(s.config.MaxTorrentSize))
	return s.AddTorrent(r, opt)
}

func (s *Session) addMagnet(link string, opt *AddTorrentOptions) (*Torrent, error) {
	ma, err := magnet.New(link)
	if err != nil {
		return nil, newInputError(err)
	}
	id, port, sto, err := s.add(opt)
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
		ma.Peers,
		nil, // info
		nil, // bitfield
		resumer.Stats{},
		nil, // webseedSources
		opt.StopAfterDownload,
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
		InfoHash:          ma.InfoHash[:],
		Port:              port,
		Name:              ma.Name,
		Trackers:          ma.Trackers,
		FixedPeers:        ma.Peers,
		AddedAt:           t.addedAt,
		StopAfterDownload: opt.StopAfterDownload,
	}
	err = s.resumer.Write(id, rspec)
	if err != nil {
		return nil, err
	}
	t2 := s.insertTorrent(t)
	if !opt.Stopped {
		err = t2.Start()
	}
	return t2, err
}

func (s *Session) add(opt *AddTorrentOptions) (id string, port int, sto *filestorage.FileStorage, err error) {
	port, err = s.getPort()
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			s.releasePort(port)
		}
	}()
	var givenID string
	if opt.ID != "" {
		givenID = opt.ID
	}
	if givenID != "" {
		s.mTorrents.RLock()
		defer s.mTorrents.RUnlock()
		if _, ok := s.torrents[givenID]; ok {
			err = errors.New("duplicate torrent id")
			return
		}
		id = givenID
	} else {
		u1, err2 := uuid.NewV1()
		if err2 != nil {
			err = err2
			return
		}
		id = base64.RawURLEncoding.EncodeToString(u1[:])
	}
	var dest string
	if s.config.DataDirIncludesTorrentID {
		dest = filepath.Join(s.config.DataDir, id)
	} else {
		dest = s.config.DataDir
	}
	sto, err = filestorage.New(dest)
	if err != nil {
		return
	}
	return
}

func (s *Session) insertTorrent(t *torrent) *Torrent {
	t.log.Info("added torrent")
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
