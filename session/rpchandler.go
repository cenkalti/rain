package session

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/cenkalti/rain/internal/rpctypes"
)

var errTorrentNotFound = errors.New("torrent not found")

type rpcHandler struct {
	session *Session
}

func (h *rpcHandler) Version(args struct{}, reply *string) error {
	*reply = Version
	return nil
}

func (h *rpcHandler) ListTorrents(args *rpctypes.ListTorrentsRequest, reply *rpctypes.ListTorrentsResponse) error {
	torrents := h.session.ListTorrents()
	reply.Torrents = make([]rpctypes.Torrent, 0, len(torrents))
	for _, t := range torrents {
		reply.Torrents = append(reply.Torrents, newTorrent(t))
	}
	return nil
}

func (h *rpcHandler) AddTorrent(args *rpctypes.AddTorrentRequest, reply *rpctypes.AddTorrentResponse) error {
	r := base64.NewDecoder(base64.StdEncoding, strings.NewReader(args.Torrent))
	t, err := h.session.AddTorrent(r)
	if err != nil {
		return err
	}
	reply.Torrent = newTorrent(t)
	return nil
}

func (h *rpcHandler) AddURI(args *rpctypes.AddURIRequest, reply *rpctypes.AddURIResponse) error {
	t, err := h.session.AddURI(args.URI)
	if err != nil {
		return err
	}
	reply.Torrent = newTorrent(t)
	return nil
}

func newTorrent(t *Torrent) rpctypes.Torrent {
	return rpctypes.Torrent{
		ID:        t.ID(),
		Name:      t.Name(),
		InfoHash:  t.InfoHash().String(),
		Port:      t.Port(),
		CreatedAt: rpctypes.Time{Time: t.CreatedAt()},
	}
}

func (h *rpcHandler) RemoveTorrent(args *rpctypes.RemoveTorrentRequest, reply *rpctypes.RemoveTorrentResponse) error {
	h.session.RemoveTorrent(args.ID)
	return nil
}

func (h *rpcHandler) GetTorrentStats(args *rpctypes.GetTorrentStatsRequest, reply *rpctypes.GetTorrentStatsResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	s := t.Stats()
	reply.Stats = rpctypes.Stats{
		Status: torrentStatusToString(s.Status),
		Pieces: struct {
			Checked   uint32
			Have      uint32
			Missing   uint32
			Available uint32
			Total     uint32
		}{
			Checked:   s.Pieces.Checked,
			Have:      s.Pieces.Have,
			Missing:   s.Pieces.Missing,
			Available: s.Pieces.Available,
			Total:     s.Pieces.Total,
		},
		Bytes: struct {
			Total      int64
			Allocated  int64
			Completed  int64
			Incomplete int64
			Downloaded int64
			Uploaded   int64
			Wasted     int64
		}{
			Total:      s.Bytes.Total,
			Allocated:  s.Bytes.Allocated,
			Completed:  s.Bytes.Completed,
			Incomplete: s.Bytes.Incomplete,
			Downloaded: s.Bytes.Downloaded,
			Uploaded:   s.Bytes.Uploaded,
			Wasted:     s.Bytes.Wasted,
		},
		Peers: struct {
			Total    int
			Incoming int
			Outgoing int
		}{
			Total:    s.Peers.Total,
			Incoming: s.Peers.Incoming,
			Outgoing: s.Peers.Outgoing,
		},
		Handshakes: struct {
			Total    int
			Incoming int
			Outgoing int
		}{
			Total:    s.Handshakes.Total,
			Incoming: s.Handshakes.Incoming,
			Outgoing: s.Handshakes.Outgoing,
		},
		Addresses: struct {
			Total   int
			Tracker int
			DHT     int
			PEX     int
		}{
			Total:   s.Addresses.Total,
			Tracker: s.Addresses.Tracker,
			DHT:     s.Addresses.DHT,
			PEX:     s.Addresses.PEX,
		},
		Downloads: struct {
			Total   int
			Running int
			Snubbed int
			Choked  int
		}{
			Total:   s.Downloads.Total,
			Running: s.Downloads.Running,
			Snubbed: s.Downloads.Snubbed,
			Choked:  s.Downloads.Choked,
		},
		MetadataDownloads: struct {
			Total   int
			Snubbed int
			Running int
		}{
			Total:   s.MetadataDownloads.Total,
			Snubbed: s.MetadataDownloads.Snubbed,
			Running: s.MetadataDownloads.Running,
		},
		Name:        s.Name,
		Private:     s.Private,
		PieceLength: s.PieceLength,
		SeededFor:   uint(s.SeededFor / time.Second),
		Speed: struct {
			Download uint
			Upload   uint
		}{
			Download: s.Speed.Download,
			Upload:   s.Speed.Upload,
		},
	}
	if s.Error != nil {
		errStr := s.Error.Error()
		reply.Stats.Error = &errStr
	}
	if s.ETA != nil {
		eta := uint(*s.ETA / time.Second)
		reply.Stats.ETA = &eta
	}
	return nil
}

func (h *rpcHandler) GetTorrentTrackers(args *rpctypes.GetTorrentTrackersRequest, reply *rpctypes.GetTorrentTrackersResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	trackers := t.Trackers()
	reply.Trackers = make([]rpctypes.Tracker, len(trackers))
	for i, t := range trackers {
		reply.Trackers[i] = rpctypes.Tracker{
			URL:      t.URL,
			Status:   trackerStatusToString(t.Status),
			Leechers: t.Leechers,
			Seeders:  t.Seeders,
		}
		if t.Error != nil {
			errStr := t.Error.Error()
			reply.Trackers[i].Error = &errStr
		}
	}
	return nil
}

func (h *rpcHandler) GetTorrentPeers(args *rpctypes.GetTorrentPeersRequest, reply *rpctypes.GetTorrentPeersResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	peers := t.Peers()
	reply.Peers = make([]rpctypes.Peer, len(peers))
	for i, p := range peers {
		var source string
		switch p.Source {
		case SourceTracker:
			source = "TRACKER"
		case SourceDHT:
			source = "DHT"
		case SourcePEX:
			source = "PEX"
		case SourceIncoming:
			source = "INCOMING"
		default:
			panic("unhandled peer source")
		}
		reply.Peers[i] = rpctypes.Peer{
			Addr:               p.Addr.String(),
			Source:             source,
			ClientInterested:   p.ClientInterested,
			ClientChoking:      p.ClientChoking,
			PeerInterested:     p.PeerInterested,
			PeerChoking:        p.PeerChoking,
			OptimisticUnchoked: p.OptimisticUnchoked,
			Snubbed:            p.Snubbed,
			EncryptedHandshake: p.EncryptedHandshake,
			EncryptedStream:    p.EncryptedStream,
		}
	}
	return nil
}

func (h *rpcHandler) StartTorrent(args *rpctypes.StartTorrentRequest, reply *rpctypes.StartTorrentResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	t.Start()
	return nil
}

func (h *rpcHandler) StopTorrent(args *rpctypes.StopTorrentRequest, reply *rpctypes.StopTorrentResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	t.Stop()
	return nil
}
