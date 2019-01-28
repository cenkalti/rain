package session

import (
	"encoding/base64"
	"errors"
	"strings"

	"github.com/cenkalti/rain/rainrpc"
)

type rpcHandler struct {
	session *Session
}

func (h *rpcHandler) ListTorrents(args *rainrpc.ListTorrentsRequest, reply *rainrpc.ListTorrentsResponse) error {
	torrents := h.session.ListTorrents()
	reply.Torrents = make([]rainrpc.Torrent, 0, len(torrents))
	for _, t := range torrents {
		reply.Torrents = append(reply.Torrents, newTorrent(t))
	}
	return nil
}

func (h *rpcHandler) AddTorrent(args *rainrpc.AddTorrentRequest, reply *rainrpc.AddTorrentResponse) error {
	r := base64.NewDecoder(base64.StdEncoding, strings.NewReader(args.Torrent))
	t, err := h.session.AddTorrent(r)
	if err != nil {
		return err
	}
	reply.Torrent = newTorrent(t)
	return nil
}

func (h *rpcHandler) AddURI(args *rainrpc.AddURIRequest, reply *rainrpc.AddURIResponse) error {
	t, err := h.session.AddURI(args.URI)
	if err != nil {
		return err
	}
	reply.Torrent = newTorrent(t)
	return nil
}

func newTorrent(t *Torrent) rainrpc.Torrent {
	return rainrpc.Torrent{
		ID:        t.ID(),
		Name:      t.Name(),
		InfoHash:  t.InfoHash().String(),
		Port:      t.Port(),
		CreatedAt: rainrpc.Time{Time: t.CreatedAt()},
	}
}

func (h *rpcHandler) RemoveTorrent(args *rainrpc.RemoveTorrentRequest, reply *rainrpc.RemoveTorrentResponse) error {
	h.session.RemoveTorrent(args.ID)
	return nil
}

func (h *rpcHandler) GetTorrentStats(args *rainrpc.GetTorrentStatsRequest, reply *rainrpc.GetTorrentStatsResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	s := t.Stats()
	reply.Stats = rainrpc.Stats{
		Status: torrentStatusToString(s.Status),
		Pieces: struct {
			Have      uint32
			Missing   uint32
			Available uint32
			Total     uint32
		}{
			Have:      s.Pieces.Have,
			Missing:   s.Pieces.Missing,
			Available: s.Pieces.Available,
			Total:     s.Pieces.Total,
		},
		Bytes: struct {
			Total      int64
			Allocated  int64
			Complete   int64
			Incomplete int64
			Downloaded int64
			Uploaded   int64
			Wasted     int64
		}{
			Total:      s.Bytes.Total,
			Allocated:  s.Bytes.Allocated,
			Complete:   s.Bytes.Complete,
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
	}
	if s.Error != nil {
		errStr := s.Error.Error()
		reply.Stats.Error = &errStr
	}
	return nil
}

func (h *rpcHandler) GetTorrentTrackers(args *rainrpc.GetTorrentTrackersRequest, reply *rainrpc.GetTorrentTrackersResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	trackers := t.Trackers()
	reply.Trackers = make([]rainrpc.Tracker, len(trackers))
	for i, t := range trackers {
		reply.Trackers[i] = rainrpc.Tracker{
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

func (h *rpcHandler) GetTorrentPeers(args *rainrpc.GetTorrentPeersRequest, reply *rainrpc.GetTorrentPeersResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	peers := t.Peers()
	reply.Peers = make([]rainrpc.Peer, len(peers))
	for i, p := range peers {
		reply.Peers[i] = rainrpc.Peer{
			Addr: p.Addr.String(),
		}
	}
	return nil
}

func (h *rpcHandler) StartTorrent(args *rainrpc.StartTorrentRequest, reply *rainrpc.StartTorrentResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	t.Start()
	return nil
}

func (h *rpcHandler) StopTorrent(args *rainrpc.StopTorrentRequest, reply *rainrpc.StopTorrentResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	t.Stop()
	return nil
}
